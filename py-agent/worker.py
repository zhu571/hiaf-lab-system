import concurrent.futures
import logging
import re
import time

from tools.api import APIError, sanitize_error
from tools.parse import LLM_TIMEOUT_SECONDS


LOG = logging.getLogger("py-agent")


class LLMTimeoutError(RuntimeError):
    pass


def _is_invalid_lease(exc):
    # 任务已被他人重领（claim_token/租约校验失败）：属设计内行为，静默即可。
    return isinstance(exc, APIError) and "invalid_agent_lease" in str(exc)


class Worker:
    def __init__(self, api, parser, poll_interval=5, lease_seconds=300, llm_timeout=LLM_TIMEOUT_SECONDS):
        self.api = api
        self.parser = parser
        self.poll_interval = poll_interval
        self.lease_seconds = lease_seconds
        self.llm_timeout = llm_timeout

    def run_once(self):
        task = self.api.claim(self.lease_seconds)
        if task is None:
            return False
        task_id = task["id"]
        claim_token = task.get("claim_token")
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
                try:
                    from urllib.parse import quote
                    from urllib.request import Request, urlopen
                    title = quote("Agent 死信告警", safe="")
                    body = f"任务 {task_id}: {detail}".encode("utf-8")
                    req = Request("http://ntfy:80/lab-alerts", data=body, method="POST")
                    req.add_header("Title", title)
                    req.add_header("Priority", "high")
                    req.add_header("Tags", "robot_face,warning")
                    req.add_header("Click", "http://10.144.144.12:8000/agent-candidates")
                    urlopen(req, timeout=5)
                except Exception:
                    LOG.exception("dead letter ntfy alert failed")
        return True

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
