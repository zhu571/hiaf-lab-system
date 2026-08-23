"""命令行动作的命令实现（login / 日报 / 项目 / 问题 / 测试数据 / 批次 / 告警 / 日志 / 经验 / 周报 / 系统更新）。

全部经 REST API 调用服务端（不直连数据库），被 cli.py 与 mcp_server.py 共用；
参数校验尽量薄（服务端为准），写操作由 api_client 自动附加 Idempotency-Key/CSRF。
"""

import json
import os
from urllib.parse import urlsplit

import httpx

from cli.api_client import LabctlError

ISSUE_SEVERITIES = ("low", "medium", "high", "critical")
TEST_DATA_TYPES = ("cryo", "pressure", "voltage", "rf_voltage", "efficiency")
RUN_TRANSITIONS = ("start", "abort", "pause", "complete", "resume")
PROJECT_TRANSITIONS = ("activate", "complete", "archive", "reactivate", "deactivate", "reopen")
LOG_STATUSES = ("draft", "confirmed", "locked", "voided")


def _clean(data):
    return {k: v for k, v in data.items() if v is not None and v != ""}


def _require(value, name):
    if not value:
        raise LabctlError(f"参数缺失：{name}", code="bad_request")
    return value


def run_login(api, username, password):
    return api.login(username, password)


def run_logout(api):
    if api.refresh_token:
        try:
            # 服务端 Logout 只读 refresh_token Cookie（忽略请求体），
            # 必须先回填 cookie 才能真正撤销服务端 refresh token。
            if api.client is not None:
                host = urlsplit(api.base_url).hostname
                if host:
                    api.client.cookies.set(
                        "refresh_token", api.refresh_token, domain=host, path="/api")
            return api.request("POST", "/api/v1/auth/logout")
        except LabctlError:
            return {"success": True, "warning": "服务端注销失败（可能已过期），本地凭证已清除"}
    return {"success": True}


def run_whoami(api):
    return api.request("GET", "/api/v1/auth/me")


def run_daily_report_today(api):
    return api.request("POST", "/api/v1/daily-reports/today")


def run_daily_report_history(api, status="", keyword="", date="", page=1, per_page=20):
    return api.request("GET", "/api/v1/daily-reports",
                       params=_clean({"status": status, "keyword": keyword,
                                      "date": date, "page": page, "per_page": per_page}))


def run_daily_report_entry(api, report_id, raw_text=None):
    _require(report_id, "report_id")
    if raw_text is not None:
        return api.request("PATCH", f"/api/v1/daily-reports/{report_id}", json={"raw_text": raw_text})
    return api.request("GET", f"/api/v1/daily-reports/{report_id}")


def run_projects_list(api, status=""):
    return api.request("GET", "/api/v1/projects", params=_clean({"status": status}))


def run_projects_get(api, project_id):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}")


def run_projects_create(api, code, name, short_name=None, description=None, visibility=None,
                        start_date=None, target_end_date=None, default_category=None, tags=None):
    _require(code, "code")
    _require(name, "name")
    body = _clean({
        "code": code, "name": name, "short_name": short_name, "description": description,
        "visibility": visibility, "start_date": start_date, "target_end_date": target_end_date,
        "default_category": default_category, "tags": tags,
    })
    return api.request("POST", "/api/v1/projects", json=body)


def run_projects_transition(api, project_id, action):
    _require(project_id, "project_id")
    _require(action, "action")
    if action not in PROJECT_TRANSITIONS:
        raise LabctlError(
            f"无效的流转动作 {action}（可选：{'/'.join(PROJECT_TRANSITIONS)}）", code="bad_request")
    return api.request("POST", f"/api/v1/projects/{project_id}/transition",
                       json={"action": action})


def run_issues_list(api, project_id, status="", severity="", search="", assignee="",
                    page=1, per_page=20):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}/issues",
                       params=_clean({"status": status, "severity": severity, "search": search,
                                      "assignee": assignee, "page": page, "per_page": per_page}))


def run_issues_create(api, project_id, title, description=None, severity="medium", assignee_id=None):
    _require(project_id, "project_id")
    _require(title, "title")
    body = _clean({"project_id": project_id, "title": title, "description": description,
                   "severity": severity, "assignee_id": assignee_id})
    return api.request("POST", f"/api/v1/projects/{project_id}/issues", json=body)


def run_issues_transition(api, issue_id, target_status, reason=None):
    _require(issue_id, "issue_id")
    _require(target_status, "target_status")
    return api.request("POST", f"/api/v1/issues/{issue_id}/transition",
                       json=_clean({"target_status": target_status, "reason": reason}))


def run_test_data_list(api, project_id, run_id=None, data_type=None, quality=None,
                       page=1, per_page=20):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}/test-data",
                       params=_clean({"run_id": run_id, "data_type": data_type,
                                      "quality": quality, "page": page, "per_page": per_page}))


def run_test_data_entry(api, project_id, data_type, measurement, value, unit=None,
                        quality="normal", measured_at=None, run_id=None, notes=None):
    _require(project_id, "project_id")
    _require(data_type, "data_type")
    _require(measurement, "measurement")
    if value is None:
        raise LabctlError("参数缺失：value", code="bad_request")
    body = _clean({
        "data_type": data_type, "measurement": measurement, "value": value, "unit": unit,
        "quality": quality, "measured_at": measured_at, "run_id": run_id, "notes": notes,
    })
    return api.request("POST", f"/api/v1/projects/{project_id}/test-data", json=body)


def run_runs_list(api, project_id, campaign=None, status=None, run_type=None, page=1, per_page=20):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}/experiment-runs",
                       params=_clean({"campaign": campaign, "status": status,
                                      "run_type": run_type, "page": page, "per_page": per_page}))


def run_runs_get(api, run_id):
    _require(run_id, "run_id")
    return api.request("GET", f"/api/v1/experiment-runs/{run_id}")


def run_runs_status(api, run_id, action):
    _require(run_id, "run_id")
    _require(action, "action")
    if action not in RUN_TRANSITIONS:
        raise LabctlError(
            f"无效的流转动作 {action}（可选：{'/'.join(RUN_TRANSITIONS)}）", code="bad_request")
    return api.request("PATCH", f"/api/v1/experiment-runs/{run_id}", json={"transition": action})


def run_alerts_list(api, status="active", limit=50, offset=0):
    return api.request("GET", "/api/v1/alerts",
                       params=_clean({"status": status, "limit": limit, "offset": offset}))


def run_alerts_resolve(api, alert_id):
    _require(alert_id, "alert_id")
    return api.request("POST", "/api/v1/alerts/resolve", json={"id": alert_id})


def run_logs_list(api, project_id, category=None, date_from=None, date_to=None, status=None,
                  page=1, per_page=20):
    _require(project_id, "project_id")
    if status and status not in LOG_STATUSES:
        raise LabctlError(
            f"无效的状态 {status}（可选：{'/'.join(LOG_STATUSES)}）", code="bad_request")
    return api.request("GET", f"/api/v1/projects/{project_id}/logs",
                       params=_clean({"category": category, "date_from": date_from,
                                      "date_to": date_to, "status": status,
                                      "page": page, "per_page": per_page}))


def run_logs_get(api, log_id):
    _require(log_id, "log_id")
    return api.request("GET", f"/api/v1/logs/{log_id}")


def run_weekly_generate(api, week_start="", notify=True):
    return api.request("POST", "/api/v1/weekly/summary",
                       json=_clean({"week_start": week_start, "notify": notify}))


def run_weekly_recent(api, limit=5):
    return api.request("GET", "/api/v1/experiences",
                       params={"tags": "weekly_summary", "status": "published",
                               "per_page": limit})


def run_experiences_extract(api, days=None):
    """触发 AI 经验候选提取（maintainer+）：最近 days 天（默认 7）resolved/closed 的
    issue 提炼经验候选，落库为 candidate 草稿供审核。days=None 走服务端默认。"""
    if days is not None and (days < 1 or days > 30):
        raise LabctlError("无效的回溯天数 days（可选 1-30）", code="bad_request")
    return api.request("POST", "/api/v1/experiences/extract-candidates",
                       json=_clean({"days": days}))


def run_experiences_list(api, status="", project_id="", page=1, per_page=20):
    return api.request("GET", "/api/v1/experiences",
                       params=_clean({"status": status, "project_id": project_id,
                                      "page": page, "per_page": per_page}))


def run_experiences_publish(api, experience_id):
    _require(experience_id, "experience_id")
    return api.request("POST", f"/api/v1/experiences/{experience_id}/publish")


def run_update_status(api):
    """查询系统版本与远端差异（admin 只读）。"""
    return api.request("GET", "/api/v1/admin/system/version")


def run_update_trigger(api):
    """触发系统更新（admin + 审计 + 幂等），返回 {session_id, current}。

    api.request 已解 WriteSuccess 信封取 data；这里再兜一层，兼容
    返回整体信封 {data: {...}} 的调用形态（如自定义 client 未解包）。
    """
    result = api.request("POST", "/api/v1/admin/system/update")
    if isinstance(result, dict) and "session_id" not in result \
            and isinstance(result.get("data"), dict):
        result = result["data"]
    return result


def run_update_stream(api, session_id, timeout_s=2400):
    """订阅更新 SSE 日志流，yield (event, payload) 直到流关闭。

    服务端事件 type：line / step / done / error（system/model.go SSEEvent）；
    timeout_s 是 httpx 读超时（无新数据的上限），默认 2400s =
    服务端 30min 看门狗略留余量。
    """
    _require(session_id, "session_id")
    token = os.getenv("LABCTL_SERVICE_TOKEN", "") or api.access_token
    if not token:
        raise LabctlError("未登录：请先执行 labctl login 或设置 LABCTL_SERVICE_TOKEN",
                          code="not_logged_in")
    headers = {"Authorization": f"Bearer {token}", "Accept": "text/event-stream"}
    url = f"{api.base_url}/api/v1/admin/system/update/stream/{session_id}"
    try:
        with api.client.stream("GET", url, headers=headers, timeout=timeout_s) as response:
            if response.status_code != 200:
                body = response.read().decode("utf-8", "replace")[:200]
                raise LabctlError(
                    f"更新日志流订阅失败（HTTP {response.status_code}）：{body}",
                    code="stream_failed", status=response.status_code)
            yield from _iter_sse_events(response)
    except httpx.TimeoutException as exc:
        raise LabctlError(
            f"更新日志流超时（{timeout_s}s 内无新数据），session={session_id}；"
            "更新可能仍在执行，可稍后重新订阅查看结果", code="stream_timeout") from exc
    except httpx.TransportError as exc:
        raise LabctlError(
            f"更新日志流连接中断（{exc.__class__.__name__}），session={session_id}",
            code="stream_disconnected") from exc


def _iter_sse_events(response):
    """逐行解析 SSE：`event:`/`data:` 行收集，空行分发一个事件。

    服务端只发 `id:`/`data:` 行（无 `event:`），事件名回退取 data JSON 的
    `type` 字段；payload 尝试 JSON 解析，失败给原文。`id:` 行与
    `: keepalive` 注释行忽略（注释行仅用于保活，不构成事件）。
    """
    event_name, data_lines = "", []
    for line in response.iter_lines():
        if line == "":
            if data_lines or event_name:
                raw = "\n".join(data_lines)
                try:
                    payload = json.loads(raw)
                except ValueError:
                    payload = raw
                if event_name:
                    event = event_name
                else:
                    event = payload.get("type", "message") if isinstance(payload, dict) else "message"
                yield event, payload
            event_name, data_lines = "", []
        elif line.startswith("event:"):
            event_name = _sse_field_value(line, "event")
        elif line.startswith("data:"):
            data_lines.append(_sse_field_value(line, "data"))


def _sse_field_value(line, field):
    """取 `field: value` 的值：按 SSE 规范剥掉冒号后的一个前导空格。"""
    value = line[len(field) + 1:]
    return value[1:] if value.startswith(" ") else value
