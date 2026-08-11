"""8 子命令 20 个动作的命令实现。

全部经 REST API 调用服务端（不直连数据库），被 cli.py 与 mcp_server.py 共用；
参数校验尽量薄（服务端为准），写操作由 api_client 自动附加 Idempotency-Key/CSRF。
"""

from cli.api_client import LabctlError

ISSUE_SEVERITIES = ("low", "medium", "high", "critical")
TEST_DATA_TYPES = ("cryo", "pressure", "voltage", "rf_voltage", "efficiency")
RUN_TRANSITIONS = ("start", "abort", "pause", "complete", "resume")


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
            return api.request("POST", "/api/v1/auth/logout",
                               json={"refresh_token": api.refresh_token})
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
    return api.request("GET", f"/api/v1/projects/{project_id}/logs",
                       params=_clean({"category": category, "date_from": date_from,
                                      "date_to": date_to, "status": status,
                                      "page": page, "per_page": per_page}))


def run_logs_get(api, log_id):
    _require(log_id, "log_id")
    return api.request("GET", f"/api/v1/logs/{log_id}")
