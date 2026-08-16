import concurrent.futures
import json
import logging
import os
import re
import threading
import time

from tools.api import APIError, sanitize_error
from tools.parse import LLM_TIMEOUT_SECONDS


LOG = logging.getLogger("py-agent")


def default_renew_interval(lease_seconds):
    # R8：续约节奏 = 租约 1/3，夹在 [30s, 90s]。上限封顶保证即使配置超长租约，
    # 续约延迟与心跳间隙（healthcheck 依赖续约线程周期 touch）都不会被拉爆；
    # 下限 30s 对齐 Go 侧 claim/renew 的最小租约，避免续约风暴。
    return max(30.0, min(lease_seconds / 3.0, 90.0))


def touch_heartbeat():
    # compose healthcheck（部署面第 4 项）探 worker 自身心跳：主循环每轮与
    # 续约线程每周期各 touch 一次。正常时两次 touch 间隔 ≤ renew_interval；
    # 进程假死/崩溃后文件停更，healthcheck 判 unhealthy（watchdog 告警接手）。
    path = os.environ.get("WORKER_HEARTBEAT_FILE", "/tmp/py-agent-heartbeat")
    try:
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(str(time.time()))
    except OSError:
        LOG.debug("heartbeat write failed", exc_info=True)


def _service_token():
    # 与 Go 侧 middleware.ReadServiceToken 同源凭据：secret 文件优先，env 兜底
    # （compose 已挂载 /run/secrets/service_token，见 docker-compose.yml py-agent）。
    # 用于调告警中心内部端点（alerts/report、alerts/resolve）；与 ntfy 的
    # ntfy_publish_token 是两套凭证，用途不同。
    path = os.environ.get("SERVICE_TOKEN_FILE", "")
    if path:
        try:
            with open(path, encoding="utf-8") as fh:
                token = fh.read().strip()
                if token:
                    return token
        except OSError:
            pass
    return os.environ.get("SERVICE_TOKEN", "").strip()


def _ntfy_publish_token():
    # 与 Go 侧 notify.readPublishToken 同源凭据：secret 文件优先，env 兜底。
    # 凭据是 todo-publisher 的 Bearer token（deploy/secrets/ntfy_publish_token.txt，
    # 已授 lab-alerts write）；严禁用 service_token——那是 Go 内部服务 token，ntfy 侧无该用户。
    path = os.environ.get("NTFY_PUBLISH_TOKEN_FILE", "/run/secrets/ntfy_publish_token")
    try:
        with open(path, encoding="utf-8") as fh:
            token = fh.read().strip()
            if token:
                return token
    except OSError:
        pass
    return os.environ.get("NTFY_PUBLISH_TOKEN", "").strip()


def _report_dead_letter(task_id, detail):
    # 死信收敛到告警中心（方案 §8.1 #11）：HTTP 调 POST /api/v1/alerts/report，
    # 带 SERVICE_TOKEN（compose 已挂载 /run/secrets/service_token）。返回是否成功，
    # 失败由调用方回退 ntfy 直发兜底（双保险，死信本就是最后手段）。
    try:
        from urllib.request import Request, urlopen
        base = os.environ.get("GO_API_BASE", "http://server:8000").rstrip("/")
        body = json.dumps({
            "level": "critical",
            "source": "agent",
            "title": "Agent 死信告警",
            "detail": f"任务 {task_id}: {detail}",
        }).encode("utf-8")
        req = Request(f"{base}/api/v1/alerts/report", data=body, method="POST")
        req.add_header("Content-Type", "application/json")
        token = _service_token()
        if token:
            req.add_header("Authorization", f"Bearer {token}")
        with urlopen(req, timeout=5) as resp:
            return 200 <= resp.status < 300
    except Exception:
        LOG.exception("dead letter alert report failed")
        return False


def dead_letter_alert(task_id, detail):
    # 死信降级告警：先上报告警中心（聚合去重 + 历史可查），上报失败时回退
    # ntfy 直发兜底（带 Bearer token，C6 修复——此前无 token 发布，ntfy
    # auth-default-access: deny-all 下被 403 静默丢弃）。
    # 任何异常都在此处吞掉，不能再向上抛。
    if _report_dead_letter(task_id, detail):
        return
    try:
        from urllib.parse import quote
        from urllib.request import Request, urlopen
        req = Request("http://ntfy:80/lab-alerts", data=f"任务 {task_id}: {detail}".encode("utf-8"), method="POST")
        req.add_header("Title", quote("Agent 死信告警", safe=""))
        req.add_header("Priority", "high")
        req.add_header("Tags", "robot_face,warning")
        req.add_header("Click", "http://10.144.144.12:8000/agent-candidates")
        token = _ntfy_publish_token()
        if token:
            req.add_header("Authorization", f"Bearer {token}")
        urlopen(req, timeout=5)
    except Exception:
        LOG.exception("dead letter ntfy alert failed")


class LLMTimeoutError(RuntimeError):
    pass


def _is_invalid_lease(exc):
    # 任务已被他人重领（claim_token/租约校验失败）：属设计内行为，静默即可。
    return isinstance(exc, APIError) and "invalid_agent_lease" in str(exc)


class Worker:
    def __init__(self, api, parser, poll_interval=5, lease_seconds=300, llm_timeout=LLM_TIMEOUT_SECONDS,
                 renew_interval=None):
        self.api = api
        self.parser = parser
        self.poll_interval = poll_interval
        self.lease_seconds = lease_seconds
        self.llm_timeout = llm_timeout
        # R8：租约靠周期续约维持（见 _renew_loop），不再要求租约一次覆盖全链路耗时。
        self.renew_interval = default_renew_interval(lease_seconds) if renew_interval is None else renew_interval

    def run_once(self):
        task = self.api.claim(self.lease_seconds)
        if task is None:
            return False
        task_id = task["id"]
        claim_token = task.get("claim_token")
        renew_stop = self._start_renewal(task_id, claim_token)
        try:
            report = self.api.get_report(task["report_id"], task["acting_user_id"], task_id)
            project_ids = list(dict.fromkeys(
                item["project_id"] for item in report.get("logs", []) if item.get("project_id")
            ))
            if not project_ids:
                raise RuntimeError("submitted report has no project logs")
            keyword = search_keyword(report)
            issues = []
            for project_id in project_ids:
                for status in ("open", "in_progress"):
                    issues.extend(self.api.list_issues(
                        project_id, status, keyword, task["acting_user_id"], task_id,
                    ))
            issues = list({item["id"]: item for item in issues}.values())[:10]
            parsed = self._parse_with_timeout(report.get("raw_text", ""), issues, project_ids)
            candidates = [to_candidate(item) for item in parsed]
            confidence = sum(item["confidence"] for item in parsed) / len(parsed) if parsed else None
            self.api.complete(
                task_id, candidates, confidence, claim_token=claim_token,
                raw_text_snapshot=report.get("raw_text", ""), report_date=report.get("report_date"),
            )
            LOG.info("task completed", extra={"task_id": task_id, "candidate_count": len(candidates)})
        except Exception as exc:
            if _is_invalid_lease(exc):
                LOG.info("task ownership lost, skip fail", extra={"task_id": task_id})
                return True
            import traceback
            LOG.exception("task failed", extra={"task_id": task_id, "trace": traceback.format_exc()[:500]})
            detail = "llm timeout" if isinstance(exc, LLMTimeoutError) else sanitize_error(exc)
            LOG.warning("task failed", extra={"task_id": task_id, "error": detail})
            try:
                self.api.fail(task_id, detail, claim_token=claim_token)
            except Exception as fail_exc:
                if _is_invalid_lease(fail_exc):
                    LOG.info("fail arrived after reclaim, ignore", extra={"task_id": task_id})
                    return True
                LOG.exception("could not mark task failed", extra={"task_id": task_id})
                dead_letter_alert(task_id, detail)
        finally:
            if renew_stop is not None:
                renew_stop.set()
        return True

    def _start_renewal(self, task_id, claim_token):
        # R8：claim 后立刻起后台续约线程，覆盖前置 HTTP 链（get_report + 2N 次
        # list_issues）与 LLM 调用全程；complete/fail 返回后在 finally 停止。
        # 无 claim_token（028 之前的老 server）时不续约，行为退回旧模式。
        if not claim_token:
            return None
        stop = threading.Event()
        threading.Thread(
            target=self._renew_loop, args=(task_id, claim_token, stop),
            name=f"lease-renew-{task_id}", daemon=True,
        ).start()
        return stop

    def _renew_loop(self, task_id, claim_token, stop):
        while not stop.wait(self.renew_interval):
            try:
                self.api.renew(task_id, claim_token, self.lease_seconds)
                touch_heartbeat()
            except Exception as exc:
                if _is_invalid_lease(exc):
                    # 任务已被他人重领：停止续约，complete/fail 侧自会得到
                    # 同样的 409 并按既有路径静默处理。
                    LOG.info("task ownership lost, stop renewing", extra={"task_id": task_id})
                    return
                LOG.warning("lease renew failed, will retry", extra={"task_id": task_id, "error": sanitize_error(exc)})

    def _parse_with_timeout(self, raw_text, issues, project_ids):
        # 硬超时只包 LLM parse（前置 HTTP 调用已有自身超时）。每次调用新建
        # executor 且 shutdown(wait=False)，绝不复用共享池：ThreadPoolExecutor
        # 无法取消已启动的调用，共享池会被挂死线程堵死（C1 缺口 A）。
        pool = concurrent.futures.ThreadPoolExecutor(max_workers=1)
        future = pool.submit(self.parser.parse, raw_text, issues, project_ids)
        try:
            return future.result(timeout=self.llm_timeout)
        except concurrent.futures.TimeoutError as exc:
            future.cancel()
            raise LLMTimeoutError("llm timeout") from exc
        finally:
            pool.shutdown(wait=False)

    def run_forever(self):
        while True:
            touch_heartbeat()
            try:
                worked = self.run_once()
            except Exception:
                LOG.exception("claim failed")
                worked = False
            if not worked:
                time.sleep(self.poll_interval)


def search_keyword(report):
    for item in report.get("logs", []):
        text = re.split(r"[，。；;：:\n]", item.get("content", ""), maxsplit=1)[0].strip()
        if text:
            return text[:64]
    return ""


def to_candidate(item):
    if item["is_duplicate"]:
        return {
            "action_type": "add_comment", "project_id": item["project_id"],
            "payload": {"issue_id": item["duplicate_issue_id"], "content": item["description"]},
            "agent_confidence": item["confidence"],
        }
    return {
        "action_type": "create_issue", "project_id": item["project_id"],
        "payload": {
            "title": item["title"], "description": item["description"],
            "severity": item["severity"],
        },
        "agent_confidence": item["confidence"],
    }
