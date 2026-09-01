"""命令行动作的命令实现（被 cli.py 与 mcp_server.py 共用，覆盖全部业务模块）。

全部经 REST API 调用服务端（不直连数据库）；参数校验尽量薄（枚举/成对约束等
本地拦截，其余以服务端为准），写操作由 api_client 自动附加 Idempotency-Key/CSRF。
枚举常量集中在本文件顶部，与后端 model.go 一一对应（以 api-contract.md 为准）。
"""

import csv
from datetime import date, datetime, timedelta
import io
import json
import mimetypes
import os
import re
import uuid
from urllib.parse import urlsplit

import httpx

from cli.api_client import LabctlError

ISSUE_SEVERITIES = ("low", "medium", "high", "critical")
ISSUE_STATUSES = ("open", "in_progress", "resolved", "closed")
TEST_DATA_TYPES = ("cryo", "pressure", "voltage", "rf_voltage", "efficiency")
TEST_DATA_QUALITIES = ("normal", "outlier", "suspect", "invalid")
TEST_DATA_SOURCES = ("manual", "instrument", "import", "backfill")  # 不含 agent：CLI 不以 agent 身份录入
RUN_TRANSITIONS = ("start", "abort", "pause", "complete", "resume")
RUN_TYPES = ("cooldown", "warmup", "steady_state", "test")
GAS_TYPES = ("He", "Ar", "Xe")
RUN_DEVICES = ("rf_carpet", "rfq", "qpig")
PROJECT_TRANSITIONS = ("activate", "complete", "archive", "reactivate", "deactivate", "reopen")
PROJECT_VISIBILITIES = ("restricted", "workspace")
COMMENT_POLICIES = ("everyone", "members", "disabled")
PROJECT_MEMBER_ROLES = ("owner", "maintainer", "member", "viewer")
LOG_STATUSES = ("draft", "confirmed", "locked", "voided")
LOG_CATEGORIES = ("general", "assembly", "test", "cryo", "rf", "vacuum", "beam", "data_analysis")
TODO_PRIORITIES = ("high", "medium", "low")
TODO_SCOPES = ("all", "mine", "shared")
TODO_STATUSES = ("open", "done", "cancelled", "all")
EXPERIENCE_STATUSES = ("candidate", "published", "archived")
EXPERIENCE_RELATIONS = ("primary", "applicable", "derived_from")
ASSEMBLY_TRANSITIONS = ("start", "pause", "resume", "complete", "skip", "cancel")
STEP_TRANSITIONS = ("start", "pause", "resume", "complete", "skip", "cancel")
STEP_TEMPLATE_KINDS = ("assembly", "experiment")
RF_DEVICES = ("rf_carpet", "rfq", "qpig")
RF_STATUSES = ("pass", "adjust", "fail")
USER_ROLES = ("admin", "maintainer", "member", "viewer")
LANGUAGES = ("zh", "en")
CANDIDATE_STATUSES = ("pending_review", "approved", "rejected", "executed", "execution_failed")
ATTACHMENT_ENTITY_TYPES = ("assembly_step", "daily_report", "issue", "log", "test_data",
                           "experiment_run", "rf_matching_record")
MAX_UPLOAD_BYTES = 100 * 1024 * 1024  # 服务端 maxUploadSize 同阈值（100 MiB）
MAX_LOGS_ALL = 1000
_DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
_RFC3339_RE = re.compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$")


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


def run_daily_report_entry(api, report_id, raw_text=None, summary=None):
    _require(report_id, "report_id")
    if raw_text is None and summary is None:
        return api.request("GET", f"/api/v1/daily-reports/{report_id}")
    return api.request("PATCH", f"/api/v1/daily-reports/{report_id}",
                       json=_clean({"raw_text": raw_text, "summary": summary}))


def run_daily_report_submit(api, report_id, force=False):
    """提交日报：返回 {report, warnings, blocked}。

    blocked=true 表示有警告且未 force（服务端不落库）——调用方（CLI）据此以
    退出码 1 提示加 --force；warnings 为空列表也原样返回。
    """
    _require(report_id, "report_id")
    return api.request("POST", f"/api/v1/daily-reports/{report_id}/submit",
                       json={"force": force})


def run_daily_report_ai_parse(api, report_id):
    """触发日报 AI 解析（仅作者 + draft；服务端限流 10 次/min，调 py-agent）。

    返回三态 status=ok/clarify/rejected + 候选日志草稿；502 upstream_error
    表示 py-agent-interpret 未就绪（AI 降级，其余业务不受影响）。
    """
    _require(report_id, "report_id")
    return api.request("POST", f"/api/v1/daily-reports/{report_id}/ai-parse")


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


def run_projects_transition(api, project_id, action, ignore_warnings=False, reason=None):
    _require(project_id, "project_id")
    _require(action, "action")
    if action not in PROJECT_TRANSITIONS:
        raise LabctlError(
            f"无效的流转动作 {action}（可选：{'/'.join(PROJECT_TRANSITIONS)}）", code="bad_request")
    # ignore_warnings 仅在 True 时下发：存在警告且未确认时服务端返回 400
    # ErrTransitionWarning，须带 --ignore-warnings 越过（如 complete 时有 open issue）。
    body = {"action": action}
    if ignore_warnings:
        body["ignore_warnings"] = True
    if reason:
        body["reason"] = reason
    return api.request("POST", f"/api/v1/projects/{project_id}/transition", json=body)


def run_projects_update(api, project_id, name=None, short_name=None, description=None,
                        visibility=None, comment_policy=None, start_date=None,
                        target_end_date=None, default_category=None, tags=None):
    _require(project_id, "project_id")
    if not any([name, short_name, description, visibility, comment_policy,
                start_date, target_end_date, default_category, tags]):
        raise LabctlError("至少提供一个修改项", code="bad_request")
    if visibility is not None and visibility not in PROJECT_VISIBILITIES:
        raise LabctlError(
            f"无效的可见性 {visibility}（可选：{'/'.join(PROJECT_VISIBILITIES)}）", code="bad_request")
    if comment_policy is not None and comment_policy not in COMMENT_POLICIES:
        raise LabctlError(
            f"无效的评论策略 {comment_policy}（可选：{'/'.join(COMMENT_POLICIES)}）", code="bad_request")
    body = _clean({
        "name": name, "short_name": short_name, "description": description,
        "visibility": visibility, "comment_policy": comment_policy,
        "start_date": start_date, "target_end_date": target_end_date,
        "default_category": default_category, "tags": tags,
    })
    return api.request("PATCH", f"/api/v1/projects/{project_id}", json=body)


def run_projects_members_list(api, project_id):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}/members")


def run_projects_members_add(api, project_id, user_id, role):
    _require(project_id, "project_id")
    _require(user_id, "user_id")
    _require(role, "role")
    if role not in PROJECT_MEMBER_ROLES:
        raise LabctlError(
            f"无效的角色 {role}（可选：{'/'.join(PROJECT_MEMBER_ROLES)}）", code="bad_request")
    return api.request("POST", f"/api/v1/projects/{project_id}/members",
                       json={"user_id": user_id, "role": role})


def run_projects_members_set_role(api, project_id, user_id, role):
    _require(project_id, "project_id")
    _require(user_id, "user_id")
    _require(role, "role")
    if role not in PROJECT_MEMBER_ROLES:
        raise LabctlError(
            f"无效的角色 {role}（可选：{'/'.join(PROJECT_MEMBER_ROLES)}）", code="bad_request")
    return api.request("PATCH", f"/api/v1/projects/{project_id}/members/{user_id}",
                       json={"role": role})


def run_projects_members_remove(api, project_id, user_id):
    _require(project_id, "project_id")
    _require(user_id, "user_id")
    return api.request("DELETE", f"/api/v1/projects/{project_id}/members/{user_id}")


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


def run_issues_get(api, issue_id):
    _require(issue_id, "issue_id")
    return api.request("GET", f"/api/v1/issues/{issue_id}")


def run_issues_update(api, issue_id, title=None, description=None, severity=None,
                      assignee_id=None):
    _require(issue_id, "issue_id")
    if not any([title, description, severity is not None, assignee_id]):
        raise LabctlError("至少提供一个修改项（title/description/severity/assignee_id）",
                          code="bad_request")
    if severity is not None and severity not in ISSUE_SEVERITIES:
        raise LabctlError(
            f"无效的严重级 {severity}（可选：{'/'.join(ISSUE_SEVERITIES)}）", code="bad_request")
    body = _clean({"title": title, "description": description,
                   "severity": severity, "assignee_id": assignee_id})
    return api.request("PATCH", f"/api/v1/issues/{issue_id}", json=body)


def run_issues_comment(api, issue_id, content):
    _require(issue_id, "issue_id")
    _require(content, "content")
    return api.request("POST", f"/api/v1/issues/{issue_id}/comments", json={"content": content})


def run_test_data_list(api, project_id, run_id=None, data_type=None, quality=None,
                       page=1, per_page=20):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}/test-data",
                       params=_clean({"run_id": run_id, "data_type": data_type,
                                      "quality": quality, "page": page, "per_page": per_page}))


def run_test_data_entry(api, project_id, data_type, measurement, value, unit=None,
                        quality="normal", source=None, measured_at=None, run_id=None, notes=None):
    _require(project_id, "project_id")
    _require(data_type, "data_type")
    _require(measurement, "measurement")
    if value is None:
        raise LabctlError("参数缺失：value", code="bad_request")
    body = _clean({
        "data_type": data_type, "measurement": measurement, "value": value, "unit": unit,
        "quality": quality, "source": source, "measured_at": measured_at,
        "run_id": run_id, "notes": notes,
    })
    return api.request("POST", f"/api/v1/projects/{project_id}/test-data", json=body)


BATCH_CSV_FIELDS = ("data_type", "measurement", "value", "unit", "quality",
                    "measured_at", "run_id", "notes")
BATCH_REQUIRED_FIELDS = ("data_type", "measurement", "value")


def parse_test_data_batch(text):
    """批量录入源文本 → 行列表：JSON 数组或表头 CSV（列名=BATCH_CSV_FIELDS）。

    CSV 表头列名不区分大小写、列序随意；空单元格省略；value 列转 float。
    行数 1-100（服务端同上限，本地提前拦截）。
    """
    try:
        rows = _parse_batch_json_or_csv(text)
    except ValueError as exc:
        raise LabctlError(f"批量数据解析失败：{exc}", code="bad_request")
    if not isinstance(rows, list) or not rows:
        raise LabctlError("批量数据须为非空数组（1-100 行）", code="bad_request")
    if len(rows) > 100:
        raise LabctlError(f"批量一次最多 100 行（当前 {len(rows)} 行）", code="bad_request")
    return rows


def _parse_batch_json_or_csv(text):
    if text.lstrip().startswith("["):
        return json.loads(text)
    reader = csv.DictReader(io.StringIO(text))
    header = [(f or "").strip().lower() for f in (reader.fieldnames or [])]
    missing = [f for f in BATCH_REQUIRED_FIELDS if f not in header]
    unknown = [f for f in header if f not in BATCH_CSV_FIELDS]
    if missing or unknown:
        raise ValueError(
            f"CSV 表头须为 {','.join(BATCH_CSV_FIELDS)}（缺 {missing}，未知列 {unknown}）；"
            "复杂数据请用 JSON 数组")
    rows = []
    for row in reader:
        item = {}
        for key, value in row.items():
            key = (key or "").strip().lower()
            if key and value not in (None, ""):
                if key == "value":
                    item[key] = float(value)
                else:
                    item[key] = value.strip()
        rows.append(item)
    return rows


def run_test_data_batch(api, project_id, rows):
    """批量录入（裸 JSON 数组，任一行失败整批 422，errors[] 行级错误在 details 透传）。"""
    _require(project_id, "project_id")
    if not isinstance(rows, list) or not rows:
        raise LabctlError("批量数据须为非空数组（1-100 行）", code="bad_request")
    if len(rows) > 100:
        raise LabctlError(f"批量一次最多 100 行（当前 {len(rows)} 行）", code="bad_request")
    return api.request("POST", f"/api/v1/projects/{project_id}/test-data/batch", json=rows)


def run_test_data_get(api, entry_id):
    _require(entry_id, "entry_id")
    return api.request("GET", f"/api/v1/test-data/{entry_id}")


def run_test_data_invalidate(api, entry_id):
    """软失效（DELETE → quality=invalid）：记录者本人或项目 owner（admin 直通）。"""
    _require(entry_id, "entry_id")
    return api.request("DELETE", f"/api/v1/test-data/{entry_id}")


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


def run_runs_create(api, project_id, name, campaign=None, run_type=None, gas_type=None,
                    target_temp=None, min_temp=None, pressure_min=None, pressure_max=None,
                    pressure_unit=None, has_beam=None, devices=None, description=None):
    _require(project_id, "project_id")
    _require(name, "name")
    if run_type is not None and run_type not in RUN_TYPES:
        raise LabctlError(
            f"无效的批次类型 {run_type}（可选：{'/'.join(RUN_TYPES)}）", code="bad_request")
    if gas_type is not None and gas_type not in GAS_TYPES:
        raise LabctlError(
            f"无效的气体类型 {gas_type}（可选：{'/'.join(GAS_TYPES)}）", code="bad_request")
    for device in devices or []:
        if device not in RUN_DEVICES:
            raise LabctlError(
                f"无效的设备 {device}（可选：{'/'.join(RUN_DEVICES)}）", code="bad_request")
    body = _clean({
        "name": name, "campaign": campaign, "run_type": run_type, "gas_type": gas_type,
        "target_temp": target_temp, "min_temp": min_temp,
        "pressure_min": pressure_min, "pressure_max": pressure_max,
        "pressure_unit": pressure_unit, "has_beam": has_beam,
        "devices": devices, "description": description,
    })
    return api.request("POST", f"/api/v1/projects/{project_id}/experiment-runs", json=body)


def run_runs_delete(api, run_id):
    """软删批次（maintainer+ 或创建者本人）。"""
    _require(run_id, "run_id")
    return api.request("DELETE", f"/api/v1/experiment-runs/{run_id}")


def run_alerts_list(api, status="active", limit=50, offset=0):
    return api.request("GET", "/api/v1/alerts",
                       params=_clean({"status": status, "limit": limit, "offset": offset}))


def run_alerts_resolve(api, alert_id):
    _require(alert_id, "alert_id")
    return api.request("POST", "/api/v1/alerts/resolve", json={"id": alert_id})


def run_alerts_get(api, alert_id):
    _require(alert_id, "alert_id")
    return api.request("GET", f"/api/v1/alerts/{alert_id}")


# ---------------------------------------------------------------- todos
def run_todos_list(api, date="", scope="", status="", limit=100):
    """待办列表（仅本人/自己参与的共享项；date 默认今天、status 默认 open、limit 默认 100）。"""
    if scope and scope not in TODO_SCOPES:
        raise LabctlError(f"无效的 scope {scope}（可选：{'/'.join(TODO_SCOPES)}）", code="bad_request")
    if status and status not in TODO_STATUSES:
        raise LabctlError(f"无效的 status {status}（可选：{'/'.join(TODO_STATUSES)}）", code="bad_request")
    return api.request("GET", "/api/v1/todos",
                       params=_clean({"date": date, "scope": scope,
                                      "status": status, "limit": limit}))


def run_todos_add(api, title, priority=None, project_id=None):
    """新建待办；带 project_id 为共享待办（须为该项目成员）。"""
    _require(title, "title")
    if priority is not None and priority not in TODO_PRIORITIES:
        raise LabctlError(
            f"无效的优先级 {priority}（可选：{'/'.join(TODO_PRIORITIES)}）", code="bad_request")
    return api.request("POST", "/api/v1/todos",
                       json=_clean({"title": title, "priority": priority,
                                    "project_id": project_id}))


def run_todos_edit(api, todo_id, updated_at="", title=None, priority=None,
                   project_id=None, clear_project=False):
    """编辑待办（仅 owner）。updated_at 必填（乐观锁版本，409=版本冲突）：

    未显式传时尝试从列表自动取当前值；列表取不到（如非今日待办）则要求
    显式 --updated-at（RFC3339，取自 todos list 输出）。
    clear_project=True 发 project_id=""（取消共享）。
    """
    _require(todo_id, "todo_id")
    if not any([title, priority is not None, project_id, clear_project]):
        raise LabctlError("至少提供一个修改项（title/priority/project-id）", code="bad_request")
    if priority is not None and priority not in TODO_PRIORITIES:
        raise LabctlError(
            f"无效的优先级 {priority}（可选：{'/'.join(TODO_PRIORITIES)}）", code="bad_request")
    body = _clean({"title": title, "priority": priority, "project_id": project_id})
    if clear_project:
        body["project_id"] = ""
    body["updated_at"] = updated_at or _lookup_todo_updated_at(api, todo_id)
    return api.request("PATCH", f"/api/v1/todos/{todo_id}", json=body)


def _lookup_todo_updated_at(api, todo_id):
    result = api.request("GET", "/api/v1/todos", params={"status": "all", "limit": 200})
    items = result.get("items", []) if isinstance(result, dict) else (result or [])
    for item in items:
        if isinstance(item, dict) and item.get("id") == todo_id:
            updated_at = item.get("updated_at", "")
            if updated_at:
                return updated_at
    raise LabctlError(
        f"未显式给 --updated-at，且待办列表中未找到 {todo_id} 的当前版本。"
        "请从 labctl todos list 输出取该条 updated_at（RFC3339）后用 --updated-at 传入",
        code="bad_request")


def run_todos_done(api, todo_id):
    """完成待办（owner 或共享项目 active 非 viewer 成员）。"""
    _require(todo_id, "todo_id")
    return api.request("PATCH", f"/api/v1/todos/{todo_id}/done")


def run_todos_defer(api, todo_id):
    """推迟待办到明天（仅 owner）。"""
    _require(todo_id, "todo_id")
    return api.request("PATCH", f"/api/v1/todos/{todo_id}/defer")


def run_todos_rm(api, todo_id):
    _require(todo_id, "todo_id")
    return api.request("DELETE", f"/api/v1/todos/{todo_id}")


# ---------------------------------------------------------------- audit
def run_audit_events(api, action="", user_id="", actor_type="", from_="", to_="",
                     page=1, per_page=20):
    """审计事件列表（admin/maintainer）。from/to 为 RFC3339，user_id 须 36 位 UUID。"""
    return api.request("GET", "/api/v1/audit/events",
                       params=_clean({"action": action, "user_id": user_id,
                                      "actor_type": actor_type, "from": from_, "to": to_,
                                      "page": page, "per_page": per_page}))


def run_audit_verify(api, from_id=0, to_id=0):
    """校验审计 hash 链（admin/maintainer）。from_id/to_id 为审计事件 id，缺省从头校验。"""
    params = {}
    if from_id:
        params["from_id"] = from_id
    if to_id:
        params["to_id"] = to_id
    return api.request("GET", "/api/v1/audit/verify", params=params)


def run_audit_get(api, request_id):
    """按 request_id 查单条审计轨迹（admin/maintainer）。"""
    _require(request_id, "request_id")
    return api.request("GET", f"/api/v1/audit/{request_id}")


# ---------------------------------------------------------------- P2: sensors
def run_sensors_latest(api, tags=""):
    """最新读数（tags 逗号分隔，须 ∈ 服务端配置 measurements；固定查最近 1h 的 last()）。"""
    return api.request("GET", "/api/v1/sensors/latest", params=_clean({"tags": tags}))


def run_sensors_history(api, tag, from_="-1h", to="", interval=""):
    """历史序列。注意 from/to/interval 是 InfluxDB Flux 字面量（如 -1h / now() / 5m
    或 RFC3339），不是普通时间参数——服务端原样拼入 Flux range/aggregateWindow。"""
    _require(tag, "tag")
    return api.request("GET", "/api/v1/sensors/history",
                       params=_clean({"tag": tag, "from": from_, "to": to,
                                      "interval": interval}))


# ---------------------------------------------------------------- P2: ask
def run_ask_chat(api, question):
    """AI 问答（JWT 通道；限流 10 次/min；502 upstream_error=AI 降级中）。"""
    _require(question, "question")
    return api.request("POST", "/api/v1/ask/chat", json={"question": question})


def run_ask_history(api, page=1, per_page=20):
    """本人问答历史（仅本人可见）。"""
    return api.request("GET", "/api/v1/ask/history",
                       params=_clean({"page": page, "per_page": per_page}))


# ---------------------------------------------------------------- P2: 杂项只读
def run_experiences_get(api, experience_id):
    _require(experience_id, "experience_id")
    return api.request("GET", f"/api/v1/experiences/{experience_id}")


def run_daily_report_by_date(api, date="", latest=False):
    """按日期查本人日报（date 缺省今天；latest=true 只取当天最新一份）。"""
    params = _clean({"date": date})
    if latest:
        params["latest"] = "true"  # 服务端仅当值恰为字符串 "true" 时生效
    return api.request("GET", "/api/v1/daily-reports/by-date", params=params)


def run_test_data_update(api, entry_id, measurement=None, value=None, unit=None,
                         quality=None, measured_at=None, notes=None):
    """编辑测试数据（member+；白名单字段，data_type/run_id/source 不可变）。"""
    _require(entry_id, "entry_id")
    if not any([measurement, value is not None, unit is not None,
                quality is not None, measured_at, notes is not None]):
        raise LabctlError("至少提供一个修改项（measurement/value/unit/quality/measured-at/notes）",
                          code="bad_request")
    if quality is not None and quality not in TEST_DATA_QUALITIES:
        raise LabctlError(
            f"无效的质量标记 {quality}（可选：{'/'.join(TEST_DATA_QUALITIES)}）", code="bad_request")
    return api.request("PATCH", f"/api/v1/test-data/{entry_id}",
                       json=_clean({"measurement": measurement, "value": value, "unit": unit,
                                    "quality": quality, "measured_at": measured_at,
                                    "notes": notes}))


# ---------------------------------------------------------------- P2: 装配/模板/RF
def run_assembly_list(api, project_id, status=None, page=1, per_page=20):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}/assembly",
                       params=_clean({"status": status, "page": page, "per_page": per_page}))


def run_assembly_transition(api, step_id, action, override_reason=None):
    """流转装配步骤（member+）。依赖步骤 cancelled 时 start 须给 override_reason。"""
    _require(step_id, "step_id")
    _require(action, "action")
    if action not in ASSEMBLY_TRANSITIONS:
        raise LabctlError(
            f"无效的流转动作 {action}（可选：{'/'.join(ASSEMBLY_TRANSITIONS)}）", code="bad_request")
    return api.request("PATCH", f"/api/v1/assembly/{step_id}",
                       json=_clean({"transition": action, "override_reason": override_reason}))


def run_step_templates_list(api, kind="", q="", page=1, per_page=20):
    if kind and kind not in STEP_TEMPLATE_KINDS:
        raise LabctlError(
            f"无效的模板类型 {kind}（可选：{'/'.join(STEP_TEMPLATE_KINDS)}）", code="bad_request")
    return api.request("GET", "/api/v1/step-templates",
                       params=_clean({"kind": kind, "q": q, "page": page, "per_page": per_page}))


def run_step_templates_generate(api, kind, prompt, context=None):
    """AI 生成步骤模板（任一项目 member+；限流 10 次/min，转 py-agent /v1/step-plan）。"""
    if kind not in STEP_TEMPLATE_KINDS:
        raise LabctlError(
            f"无效的模板类型 {kind}（可选：{'/'.join(STEP_TEMPLATE_KINDS)}）", code="bad_request")
    _require(prompt, "prompt")
    return api.request("POST", "/api/v1/step-templates/generate",
                       json=_clean({"kind": kind, "prompt": prompt, "context": context}))


def run_rf_matching_list(api, project_id, device=None, status=None, page=1, per_page=20):
    _require(project_id, "project_id")
    return api.request("GET", f"/api/v1/projects/{project_id}/rf-matching",
                       params=_clean({"device": device, "status": status,
                                      "page": page, "per_page": per_page}))


def run_rf_matching_create(api, project_id, device, frequency_mhz, status, s11=None,
                           input_freq=None, input_voltage=None, input_power=None,
                           input_desc=None, output_freq=None, output_voltage=None,
                           output_power=None, output_desc=None, transformer_turns=None,
                           capacitance_text=None, transformer_material=None,
                           shunt_inductance=None, series_capacitor=None,
                           notes=None, measured_at=None):
    """录入 RF 匹配记录（member+）：device+frequency_mhz(>0)+status 必填，测量字段可选。"""
    _require(project_id, "project_id")
    if device not in RF_DEVICES:
        raise LabctlError(f"无效的设备 {device}（可选：{'/'.join(RF_DEVICES)}）", code="bad_request")
    if status not in RF_STATUSES:
        raise LabctlError(f"无效的状态 {status}（可选：{'/'.join(RF_STATUSES)}）", code="bad_request")
    if frequency_mhz is None or frequency_mhz <= 0:
        raise LabctlError("frequency_mhz 必须大于 0", code="bad_request")
    body = _clean({
        "device": device, "frequency_mhz": frequency_mhz, "status": status,
        "s11": s11, "input_freq": input_freq, "input_voltage": input_voltage,
        "input_power": input_power, "input_desc": input_desc,
        "output_freq": output_freq, "output_voltage": output_voltage,
        "output_power": output_power, "output_desc": output_desc,
        "transformer_turns": transformer_turns, "capacitance_text": capacitance_text,
        "transformer_material": transformer_material, "shunt_inductance": shunt_inductance,
        "series_capacitor": series_capacitor, "notes": notes, "measured_at": measured_at,
    })
    return api.request("POST", f"/api/v1/projects/{project_id}/rf-matching", json=body)


# ---------------------------------------------------------------- P2: admin 管理
def run_admin_users_list(api):
    return api.request("GET", "/api/v1/admin/users")


def run_admin_users_create(api, username, display_name=None, role=None, password=None):
    """创建用户（admin）；不给 password 服务端生成 temporary_password。"""
    _require(username, "username")
    if role is not None and role not in USER_ROLES:
        raise LabctlError(f"无效的角色 {role}（可选：{'/'.join(USER_ROLES)}）", code="bad_request")
    return api.request("POST", "/api/v1/admin/users",
                       json=_clean({"username": username, "display_name": display_name,
                                    "role": role, "password": password}))


def run_admin_users_set(api, user_id, display_name=None, role=None, disabled=None):
    """更新用户（admin）：display_name/role/disabled 全可选指针。"""
    _require(user_id, "user_id")
    if not any([display_name, role is not None, disabled is not None]):
        raise LabctlError("至少提供一个修改项（display-name/role/disable|enable）", code="bad_request")
    if role is not None and role not in USER_ROLES:
        raise LabctlError(f"无效的角色 {role}（可选：{'/'.join(USER_ROLES)}）", code="bad_request")
    return api.request("PATCH", f"/api/v1/admin/users/{user_id}",
                       json=_clean({"display_name": display_name, "role": role,
                                    "disabled": disabled}))


def run_admin_users_reset_password(api, user_id, new_password=None):
    """重置密码（admin）；不给 new_password 服务端生成 temporary_password。"""
    _require(user_id, "user_id")
    return api.request("POST", f"/api/v1/admin/users/{user_id}/reset-password",
                       json=_clean({"new_password": new_password}))


def run_admin_invites_list(api):
    return api.request("GET", "/api/v1/admin/invitation-codes")


def run_admin_invites_create(api, expires_at=None):
    """创建邀请码（admin；expires_at 可选）。"""
    return api.request("POST", "/api/v1/admin/invitation-codes",
                       json=_clean({"expires_at": expires_at}))


def run_admin_invites_revoke(api, invite_id):
    _require(invite_id, "invite_id")
    return api.request("POST",
                       f"/api/v1/admin/invitation-codes/{invite_id}/revoke")


def run_automation_rules_list(api):
    return api.request("GET", "/api/v1/admin/automation/rules")


def run_automation_rules_create(api, name, trigger_event="daily_report.submitted"):
    """创建自动化规则（admin；一期仅 daily_report.submitted 触发 + enqueue_agent_task 动作）。"""
    _require(name, "name")
    return api.request("POST", "/api/v1/admin/automation/rules",
                       json={"name": name, "trigger_event": trigger_event,
                             "action": {"type": "enqueue_agent_task"}})


def run_automation_rules_enable(api, rule_id, enabled):
    """启停规则（admin；一期 PATCH 仅支持切 enabled）。"""
    _require(rule_id, "rule_id")
    return api.request("PATCH", f"/api/v1/admin/automation/rules/{rule_id}",
                       json={"enabled": bool(enabled)})


def run_automation_rules_rm(api, rule_id):
    _require(rule_id, "rule_id")
    return api.request("DELETE", f"/api/v1/admin/automation/rules/{rule_id}")


# ---------------------------------------------------------------- P2: agent candidates
def run_agent_candidates_list(api, status="", page=1, per_page=20):
    """Agent 候选动作队列（admin/maintainer；审核队列 Web 已有，CLI 作补充）。"""
    if status and status not in CANDIDATE_STATUSES:
        raise LabctlError(
            f"无效的候选状态 {status}（可选：{'/'.join(CANDIDATE_STATUSES)}）", code="bad_request")
    return api.request("GET", "/api/v1/agent/candidates",
                       params=_clean({"status": status, "page": page, "per_page": per_page}))


def run_agent_candidates_trace(api, candidate_id):
    """候选动作完整 AI 时间线（迁移 030）。"""
    _require(candidate_id, "candidate_id")
    return api.request("GET", f"/api/v1/agent/candidates/{candidate_id}/trace")


def run_agent_candidates_approve(api, candidate_id):
    _require(candidate_id, "candidate_id")
    return api.request("POST", f"/api/v1/agent/candidates/{candidate_id}/approve")


def run_agent_candidates_reject(api, candidate_id, reason=""):
    _require(candidate_id, "candidate_id")
    return api.request("POST", f"/api/v1/agent/candidates/{candidate_id}/reject",
                       json={"reason": reason})


# ---------------------------------------------------------------- P2: instruments 只读
def run_instruments_list(api):
    return api.request("GET", "/api/v1/instruments")


def run_instruments_status(api, instrument_id):
    _require(instrument_id, "instrument_id")
    return api.request("GET", f"/api/v1/instruments/{instrument_id}/status")


def run_instruments_whitelist(api):
    return api.request("GET", "/api/v1/instruments/whitelist")


def run_instruments_parse_result(api, instrument_id, command, response):
    """只读解析仪器原始响应（viewer+；不发任何命令）。"""
    _require(instrument_id, "instrument_id")
    _require(command, "command")
    _require(response, "response")
    return api.request("POST", f"/api/v1/instruments/{instrument_id}/parse-result",
                       json={"command": command, "response": response})


# ---------------------------------------------------------------- P2: auth 自助
def run_login_password(api, old_password, new_password):
    """修改本人密码（强密码校验由服务端做；改完建议重新 login）。"""
    _require(old_password, "old_password")
    _require(new_password, "new_password")
    return api.request("POST", "/api/v1/auth/change-password",
                       json={"old_password": old_password, "new_password": new_password})


def run_login_set_language(api, language):
    """切换界面语言（PATCH profile，后端 users.language 持久化）。"""
    if language not in LANGUAGES:
        raise LabctlError(f"无效的语言 {language}（可选：{'/'.join(LANGUAGES)}）", code="bad_request")
    return api.request("PATCH", "/api/v1/auth/profile", json={"language": language})


# ---------------------------------------------------------------- P2: run steps / report-links
def run_runs_steps_list(api, run_id, page=1, per_page=50):
    _require(run_id, "run_id")
    return api.request("GET", f"/api/v1/experiment-runs/{run_id}/steps",
                       params=_clean({"page": page, "per_page": per_page}))


def run_runs_steps_add(api, run_id, name, description=None, depends_on=None, step_order=0):
    """添加实验步骤（member+；step_order=0 追加到末尾，depends_on 为前置步骤 ID）。"""
    _require(run_id, "run_id")
    _require(name, "name")
    return api.request("POST", f"/api/v1/experiment-runs/{run_id}/steps",
                       json=_clean({"name": name, "description": description,
                                    "depends_on": depends_on, "step_order": step_order}))


def run_run_steps_status(api, step_id, action):
    """流转实验步骤（member+；状态机由服务端校验）。"""
    _require(step_id, "step_id")
    _require(action, "action")
    if action not in STEP_TRANSITIONS:
        raise LabctlError(
            f"无效的流转动作 {action}（可选：{'/'.join(STEP_TRANSITIONS)}）", code="bad_request")
    return api.request("PATCH", f"/api/v1/run-steps/{step_id}", json={"transition": action})


def run_runs_steps_reorder(api, run_id, steps):
    """重排实验步骤（maintainer+）。steps: [{id, step_order}]（JSON 字符串或列表）。"""
    _require(run_id, "run_id")
    if isinstance(steps, str):
        try:
            steps = json.loads(steps)
        except ValueError as exc:
            raise LabctlError(f"steps 不是合法 JSON：{exc}", code="bad_request")
    if not isinstance(steps, list) or not steps:
        raise LabctlError("steps 须为非空的 [{id, step_order}] 数组", code="bad_request")
    payload = []
    for step in steps:
        if not isinstance(step, dict) or not step.get("id") \
                or not isinstance(step.get("step_order"), int):
            raise LabctlError("每个步骤须含 id 与整数 step_order", code="bad_request")
        payload.append({"id": step["id"], "step_order": step["step_order"]})
    return api.request("POST", "/api/v1/run-steps/reorder",
                       json={"run_id": run_id, "steps": payload})


def run_runs_report_link(api, run_id, report_id):
    """关联日报到批次（maintainer+ 或创建者）。"""
    _require(run_id, "run_id")
    _require(report_id, "report_id")
    return api.request("POST",
                       f"/api/v1/experiment-runs/{run_id}/daily-reports/{report_id}")


def run_runs_report_unlink(api, run_id, report_id):
    _require(run_id, "run_id")
    _require(report_id, "report_id")
    return api.request("DELETE",
                       f"/api/v1/experiment-runs/{run_id}/daily-reports/{report_id}")


def _log_time(value, end=False):
    if not value:
        return value
    if _DATE_RE.fullmatch(value):
        try:
            date.fromisoformat(value)
        except ValueError as exc:
            raise LabctlError(f"无效日期：{value}", code="bad_request") from exc
        return f"{value}T{'23:59:59' if end else '00:00:00'}+08:00"
    if not _RFC3339_RE.fullmatch(value):
        raise LabctlError(f"无效时间：{value}（须为 YYYY-MM-DD 或 RFC3339）", code="bad_request")
    try:
        datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise LabctlError(f"无效时间：{value}", code="bad_request") from exc
    return value


def _resolve_project(api, value):
    try:
        uuid.UUID(value)
        return value
    except ValueError:
        pass
    projects = run_projects_list(api)
    exact = [p for p in projects if str(p.get("code", "")).casefold() == value.casefold()]
    matches = exact or [p for p in projects if value.casefold() in
                        f"{p.get('name', '')} {p.get('short_name', '')}".casefold()]
    if len(matches) == 1:
        return matches[0]["id"]
    candidates = ", ".join(f"{p.get('code')} ({p.get('name')})" for p in matches)
    if candidates:
        raise LabctlError(f"项目定位不唯一：{value}；候选：{candidates}", code="bad_request")
    raise LabctlError(f"未找到项目：{value}", code="bad_request")


def run_logs_list(api, project_id, category=None, date_from=None, date_to=None, status=None,
                  page=1, per_page=20, date_value=None, all_pages=False,
                  resolve_project=False):
    _require(project_id, "project_id")
    if page < 1 or not 1 <= per_page <= 100:
        raise LabctlError("page 须 >= 1，per_page 须为 1-100", code="bad_request")
    if status and status not in LOG_STATUSES:
        raise LabctlError(
            f"无效的状态 {status}（可选：{'/'.join(LOG_STATUSES)}）", code="bad_request")
    if date_value and (date_from or date_to):
        raise LabctlError("--date 不能与 --date-from/--date-to 同用", code="bad_request")
    if date_value:
        date_from = _log_time(date_value)
        try:
            date_to = f"{date.fromisoformat(date_value) + timedelta(days=1)}T00:00:00+08:00"
        except ValueError as exc:
            raise LabctlError(f"无效日期：{date_value}", code="bad_request") from exc
    else:
        date_from, date_to = _log_time(date_from), _log_time(date_to, end=True)
    if resolve_project:
        project_id = _resolve_project(api, project_id)

    def fetch(current_page):
        return api.request("GET", f"/api/v1/projects/{project_id}/logs",
                           params=_clean({"category": category, "date_from": date_from,
                                          "date_to": date_to, "status": status,
                                          "page": current_page, "per_page": per_page}))

    if not all_pages:
        return fetch(page)
    items = []
    current_page = 1
    while True:
        result = fetch(current_page)
        total = result.get("total", 0)
        if total > MAX_LOGS_ALL or len(items) + len(result.get("items", [])) > MAX_LOGS_ALL:
            raise LabctlError(f"匹配日志超过 {MAX_LOGS_ALL} 条，请增加筛选条件", code="bad_request")
        items.extend(result.get("items", []))
        if current_page * per_page >= total:
            return {"items": items, "total": total}
        current_page += 1


def run_logs_get(api, log_id):
    _require(log_id, "log_id")
    return api.request("GET", f"/api/v1/logs/{log_id}")


def run_logs_create(api, project_id, content, category="general", occurred_at=None,
                    daily_report_id=None, raw_snippet=None):
    """录入项目日志（member+），source 固定 manual（agent/wechat 通道不属于 CLI）。

    典型闭环：logs create →（可选）logs update --confirm → daily-report submit。
    带 daily_report_id 时须为该日报作者；raw_snippet 须是日报原文片段（服务端模糊校验）。
    """
    _require(project_id, "project_id")
    _require(content, "content")
    if category not in LOG_CATEGORIES:
        raise LabctlError(
            f"无效的日志分类 {category}（可选：{'/'.join(LOG_CATEGORIES)}）", code="bad_request")
    if raw_snippet and not daily_report_id:
        raise LabctlError(
            "raw_snippet 必须与 daily_report_id 同用（片段须匹配该日报原文）", code="bad_request")
    body = _clean({
        "daily_report_id": daily_report_id, "category": category, "content": content,
        "raw_snippet": raw_snippet, "occurred_at": occurred_at,
    })
    return api.request("POST", f"/api/v1/projects/{project_id}/logs", json=body)


def run_logs_update(api, log_id, content=None, category=None, occurred_at=None, confirm=False):
    """更新日志（member+ 改自己的 / maintainer+ 改任意；仅 draft 态，项目须 active）。

    confirm=True 置 content_status=confirmed（后端唯一允许的显式状态值）。
    """
    _require(log_id, "log_id")
    if not any([content, category is not None, occurred_at, confirm]):
        raise LabctlError("至少提供一个修改项（content/category/occurred-at/confirm）",
                          code="bad_request")
    if category is not None and category not in LOG_CATEGORIES:
        raise LabctlError(
            f"无效的日志分类 {category}（可选：{'/'.join(LOG_CATEGORIES)}）", code="bad_request")
    body = _clean({"category": category, "content": content, "occurred_at": occurred_at})
    if confirm:
        body["content_status"] = "confirmed"
    return api.request("PATCH", f"/api/v1/logs/{log_id}", json=body)


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


def parse_linked_projects(values):
    """['prj_1', 'prj_2:primary'] → [{project_id, relation?}]（relation 缺省服务端补 applicable）。"""
    links = []
    for value in values or []:
        project_id, sep, relation = value.partition(":")
        project_id = project_id.strip()
        if not project_id:
            raise LabctlError(f"无效的关联项目 {value}（格式 ID[:relation]）", code="bad_request")
        if sep and relation not in EXPERIENCE_RELATIONS:
            raise LabctlError(
                f"无效的 relation {relation}（可选：{'/'.join(EXPERIENCE_RELATIONS)}）",
                code="bad_request")
        link = {"project_id": project_id}
        if sep:
            link["relation"] = relation
        links.append(link)
    return links or None


def run_experiences_create(api, title, content, project_id=None, tags=None,
                           linked_projects=None):
    """创建经验候选（candidate 草稿）：带 project_id 需该项目 member+；
    不带=全局经验仅 admin。发布走 experiences publish。"""
    _require(title, "title")
    _require(content, "content")
    body = _clean({
        "project_id": project_id, "title": title, "content": content,
        "tags": tags, "linked_projects": linked_projects,
    })
    return api.request("POST", "/api/v1/experiences", json=body)


def run_experiences_update(api, experience_id, title=None, content=None, tags=None,
                           linked_projects=None):
    """编辑经验（仅 candidate 态；作者/admin/项目 maintainer+）。"""
    _require(experience_id, "experience_id")
    if not any([title, content, tags, linked_projects]):
        raise LabctlError("至少提供一个修改项（title/content/tags/linked-project）",
                          code="bad_request")
    return api.request("PATCH", f"/api/v1/experiences/{experience_id}",
                       json=_clean({"title": title, "content": content, "tags": tags,
                                    "linked_projects": linked_projects}))


def run_experiences_archive(api, experience_id):
    """归档经验（须 published；项目 owner，全局仅 admin）。"""
    _require(experience_id, "experience_id")
    return api.request("POST", f"/api/v1/experiences/{experience_id}/archive")


# ---------------------------------------------------------------- 附件
def _check_entity_pair(entity_type, entity_id):
    if (entity_type is None) != (entity_id is None):
        raise LabctlError("entity_type 与 entity_id 必须成对出现（或不都传）", code="bad_request")
    if entity_type is not None and entity_type not in ATTACHMENT_ENTITY_TYPES:
        raise LabctlError(
            f"无效的实体类型 {entity_type}（可选：{'/'.join(ATTACHMENT_ENTITY_TYPES)}）",
            code="bad_request")
    return _clean({"entity_type": entity_type, "entity_id": entity_id})


def run_attachments_upload(api, path, entity_type=None, entity_id=None, description=None):
    """上传附件（≤100 MiB，本地预检同阈值）；不带实体=任意登录用户，带实体需 write 权限。

    文件先整体读入内存再构造 multipart（保证 429/5xx 重试可重放）；
    服务端按 sha256 去重：同文件秒传返回已有附件+新建 link。
    """
    _require(path, "path")
    fields = _check_entity_pair(entity_type, entity_id)
    try:
        with open(path, "rb") as fh:
            payload = fh.read()
    except OSError as exc:
        raise LabctlError(f"无法读取文件 {path}：{exc}", code="bad_request") from exc
    if len(payload) > MAX_UPLOAD_BYTES:
        raise LabctlError(
            f"附件超过 100 MiB 上限（当前 {len(payload)} 字节），服务端同样会拒绝",
            code="attachment_too_large")
    filename = os.path.basename(path)
    mimetype = mimetypes.guess_type(filename)[0] or "application/octet-stream"
    if description:
        fields["description"] = description
    return api.request("POST", "/api/v1/attachments", data=fields,
                       files={"file": (filename, payload, mimetype)}, timeout=300)


def run_attachments_list(api, entity_type=None, entity_id=None, page=1, per_page=20):
    """附件列表：都不传=本人未绑定附件（admin 全量）；成对传=查某实体的附件。"""
    fields = _check_entity_pair(entity_type, entity_id)
    fields.update({"page": page, "per_page": per_page})
    return api.request("GET", "/api/v1/attachments", params=fields)


def run_attachments_download(api, attachment_id, output=None):
    """下载附件到本地（--output 缺省用附件 original_name 存当前目录）。

    先取附件元数据（顺带拿到服务端 sha256），再流式下载并本地校验 sha256。
    """
    _require(attachment_id, "attachment_id")
    attachment = api.request("GET", f"/api/v1/attachments/{attachment_id}")
    if not isinstance(attachment, dict):
        raise LabctlError("附件元数据响应异常", code="api_error")
    original_name = attachment.get("original_name") or "attachment"
    dest = output or original_name
    result = dict(api.download(f"/api/v1/attachments/{attachment_id}/content",
                               dest, timeout=300))
    result.update({
        "attachment_id": attachment.get("id", attachment_id),
        "original_name": original_name,
        "mime_type": attachment.get("mime_type", ""),
        "server_sha256": attachment.get("sha256", ""),
    })
    if result["server_sha256"]:
        result["sha256_match"] = result["server_sha256"] == result["sha256"]
    return result


def run_attachments_link(api, attachment_id, entity_type, entity_id, description=None):
    """把已有附件补挂到实体（附件可读 + 目标实体 write）。"""
    _require(attachment_id, "attachment_id")
    if entity_type not in ATTACHMENT_ENTITY_TYPES:
        raise LabctlError(
            f"无效的实体类型 {entity_type}（可选：{'/'.join(ATTACHMENT_ENTITY_TYPES)}）",
            code="bad_request")
    _require(entity_id, "entity_id")
    return api.request("POST", f"/api/v1/attachments/{attachment_id}/links",
                       json=_clean({"entity_type": entity_type, "entity_id": entity_id,
                                    "description": description}))


def run_attachments_unlink(api, attachment_id, link_id):
    _require(attachment_id, "attachment_id")
    _require(link_id, "link_id")
    return api.request("DELETE", f"/api/v1/attachments/{attachment_id}/links/{link_id}")


def run_attachments_rm(api, attachment_id):
    """软删附件（上传者本人或 admin）。"""
    _require(attachment_id, "attachment_id")
    return api.request("DELETE", f"/api/v1/attachments/{attachment_id}")


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
