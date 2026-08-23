import json
import sys
import unittest
from pathlib import Path

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from cli import commands  # noqa: E402
from cli.api_client import LabctlError  # noqa: E402

from cli.tests.helpers import TOKENS, err, make_api, ok  # noqa: E402


def _login_then(handler, access="at_1"):
    api = make_api(handler, access_token=access, refresh_token="rt_1", csrf_token="csrf_1")
    return api


class TestCommands(unittest.TestCase):
    def _assert_happy(self, fn, method, path, body=None, params=None, response=ok):
        captured = {}

        def handler(request):
            captured.update(method=request.method, path=request.url.path,
                            params=request.url.params)
            if body is not None:
                captured["body"] = json_roundtrip(request.read().decode())
            return response({"ok": True})

        api = _login_then(handler)
        result = fn(api)
        self.assertEqual(captured["method"], method)
        self.assertEqual(captured["path"], path)
        if body is not None:
            self.assertEqual(captured["body"], body)
        if params is not None:
            self.assertEqual(dict(captured["params"]), params)
        self.assertEqual(result, {"ok": True})
        return captured

    def test_daily_report_today(self):
        self._assert_happy(commands.run_daily_report_today, "POST", "/api/v1/daily-reports/today")

    def test_daily_report_history(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_history(api, status="draft", page=2),
            "GET", "/api/v1/daily-reports",
            params={"status": "draft", "page": "2", "per_page": "20"})

    def test_daily_report_entry_get(self):
        self._assert_happy(lambda api: commands.run_daily_report_entry(api, "rep_1"),
                           "GET", "/api/v1/daily-reports/rep_1")

    def test_daily_report_entry_patch(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_entry(api, "rep_1", raw_text="今日记录"),
            "PATCH", "/api/v1/daily-reports/rep_1", body={"raw_text": "今日记录"})

    def test_projects_list(self):
        self._assert_happy(lambda api: commands.run_projects_list(api, status="active"),
                           "GET", "/api/v1/projects", params={"status": "active"})

    def test_projects_get(self):
        self._assert_happy(lambda api: commands.run_projects_get(api, "prj_1"),
                           "GET", "/api/v1/projects/prj_1")

    def test_projects_create(self):
        self._assert_happy(
            lambda api: commands.run_projects_create(api, code="prj_a", name="项目A",
                                                     description="desc", tags="x"),
            "POST", "/api/v1/projects",
            body={"code": "prj_a", "name": "项目A", "description": "desc", "tags": "x"})

    def test_projects_create_required(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_projects_create(api, code="", name="x")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_projects_transition(self):
        self._assert_happy(lambda api: commands.run_projects_transition(api, "prj_1", "activate"),
                           "POST", "/api/v1/projects/prj_1/transition",
                           body={"action": "activate"})

    def test_projects_transition_invalid_action(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_projects_transition(api, "prj_1", "explode")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_projects_transition_required(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_projects_transition(api, "prj_1", "")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_issues_list(self):
        self._assert_happy(
            lambda api: commands.run_issues_list(api, "prj_1", status="open", severity="high"),
            "GET", "/api/v1/projects/prj_1/issues",
            params={"status": "open", "severity": "high", "page": "1", "per_page": "20"})

    def test_issues_create(self):
        self._assert_happy(
            lambda api: commands.run_issues_create(api, "prj_1", title="反射异常",
                                                   severity="high", assignee_id="usr_2"),
            "POST", "/api/v1/projects/prj_1/issues",
            body={"project_id": "prj_1", "title": "反射异常", "severity": "high",
                  "assignee_id": "usr_2"})

    def test_issues_transition(self):
        self._assert_happy(
            lambda api: commands.run_issues_transition(api, "iss_1", "resolved", reason="已修复"),
            "POST", "/api/v1/issues/iss_1/transition",
            body={"target_status": "resolved", "reason": "已修复"})

    def test_issues_transition_required(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_issues_transition(api, "iss_1", "")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_test_data_list(self):
        self._assert_happy(
            lambda api: commands.run_test_data_list(api, "prj_1", data_type="cryo"),
            "GET", "/api/v1/projects/prj_1/test-data",
            params={"data_type": "cryo", "page": "1", "per_page": "20"})

    def test_test_data_entry(self):
        self._assert_happy(
            lambda api: commands.run_test_data_entry(api, "prj_1", "cryo", "target_temp",
                                                     4.2, unit="K", notes="稳定后读数"),
            "POST", "/api/v1/projects/prj_1/test-data",
            body={"data_type": "cryo", "measurement": "target_temp", "value": 4.2,
                  "unit": "K", "quality": "normal", "notes": "稳定后读数"})

    def test_test_data_entry_value_zero_allowed(self):
        captured = {}

        def handler(request):
            captured["body"] = json_roundtrip(request.read().decode())
            return ok({"ok": True})

        api = _login_then(handler)
        commands.run_test_data_entry(api, "prj_1", "pressure", "cell_pressure", 0.0)
        self.assertEqual(captured["body"]["value"], 0.0)

    def test_test_data_entry_required(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_test_data_entry(api, "prj_1", "cryo", "m", None)
        self.assertEqual(cm.exception.code, "bad_request")

    def test_runs_list(self):
        self._assert_happy(
            lambda api: commands.run_runs_list(api, "prj_1", status="active"),
            "GET", "/api/v1/projects/prj_1/experiment-runs",
            params={"status": "active", "page": "1", "per_page": "20"})

    def test_runs_get(self):
        self._assert_happy(lambda api: commands.run_runs_get(api, "run_1"),
                           "GET", "/api/v1/experiment-runs/run_1")

    def test_runs_status(self):
        self._assert_happy(lambda api: commands.run_runs_status(api, "run_1", "complete"),
                           "PATCH", "/api/v1/experiment-runs/run_1",
                           body={"transition": "complete"})

    def test_runs_status_invalid_action(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_runs_status(api, "run_1", "explode")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_alerts_list(self):
        self._assert_happy(lambda api: commands.run_alerts_list(api, status="active", limit=10),
                           "GET", "/api/v1/alerts",
                           params={"status": "active", "limit": "10", "offset": "0"})

    def test_alerts_resolve(self):
        self._assert_happy(lambda api: commands.run_alerts_resolve(api, "alert_1"),
                           "POST", "/api/v1/alerts/resolve", body={"id": "alert_1"})

    def test_logs_list(self):
        self._assert_happy(
            lambda api: commands.run_logs_list(api, "prj_1", category="rf", status="confirmed"),
            "GET", "/api/v1/projects/prj_1/logs",
            params={"category": "rf", "status": "confirmed", "page": "1", "per_page": "20"})

    def test_logs_list_status_draft(self):
        self._assert_happy(
            lambda api: commands.run_logs_list(api, "prj_1", status="draft"),
            "GET", "/api/v1/projects/prj_1/logs",
            params={"status": "draft", "page": "1", "per_page": "20"})

    def test_logs_list_status_omitted_keeps_default(self):
        self._assert_happy(
            lambda api: commands.run_logs_list(api, "prj_1"),
            "GET", "/api/v1/projects/prj_1/logs",
            params={"page": "1", "per_page": "20"})

    def test_logs_list_invalid_status(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_logs_list(api, "prj_1", status="bogus")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_logs_get(self):
        self._assert_happy(lambda api: commands.run_logs_get(api, "log_1"),
                           "GET", "/api/v1/logs/log_1")

    def test_weekly_generate_default(self):
        self._assert_happy(
            lambda api: commands.run_weekly_generate(api),
            "POST", "/api/v1/weekly/summary",
            body={"notify": True})

    def test_weekly_generate_explicit(self):
        self._assert_happy(
            lambda api: commands.run_weekly_generate(api, week_start="2026-08-03", notify=False),
            "POST", "/api/v1/weekly/summary",
            body={"week_start": "2026-08-03", "notify": False})

    def test_weekly_recent(self):
        self._assert_happy(
            lambda api: commands.run_weekly_recent(api, limit=3),
            "GET", "/api/v1/experiences",
            params={"tags": "weekly_summary", "status": "published", "per_page": "3"})

    def test_experiences_extract_candidates_default(self):
        self._assert_happy(
            lambda api: commands.run_experiences_extract(api),
            "POST", "/api/v1/experiences/extract-candidates",
            body={})

    def test_experiences_extract_candidates_days(self):
        self._assert_happy(
            lambda api: commands.run_experiences_extract(api, days=14),
            "POST", "/api/v1/experiences/extract-candidates",
            body={"days": 14})

    def test_experiences_extract_candidates_invalid_days(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_experiences_extract(api, days=0)
        self.assertEqual(cm.exception.code, "bad_request")
        with self.assertRaises(LabctlError) as cm:
            commands.run_experiences_extract(api, days=31)
        self.assertEqual(cm.exception.code, "bad_request")

    def test_experiences_list_candidates(self):
        self._assert_happy(
            lambda api: commands.run_experiences_list(api, status="candidate"),
            "GET", "/api/v1/experiences",
            params={"status": "candidate", "page": "1", "per_page": "20"})

    def test_experiences_publish(self):
        self._assert_happy(
            lambda api: commands.run_experiences_publish(api, "exp_1"),
            "POST", "/api/v1/experiences/exp_1/publish")

    def test_experiences_publish_requires_id(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_experiences_publish(api, "")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_login_and_whoami(self):
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            if request.url.path == "/api/v1/auth/login":
                return ok(TOKENS)
            return ok({"user": {"username": "zhangsan"}})

        api = _login_then(handler, access="")
        data = commands.run_login(api, "zhangsan", "secret")
        self.assertEqual(data["access_token"], "at_1")
        who = commands.run_whoami(api)
        self.assertEqual(who["user"]["username"], "zhangsan")

    def test_logout_sends_refresh_token(self):
        captured = {}

        def handler(request):
            captured["cookie"] = request.headers.get("cookie", "")
            captured["body"] = request.read().decode()
            return ok({"success": True})

        api = _login_then(handler)
        result = commands.run_logout(api)
        # 服务端 Logout 只读 refresh_token Cookie（忽略请求体）：
        # 必须回填 cookie 才能真正撤销服务端 refresh token。
        self.assertIn("refresh_token=rt_1", captured["cookie"])
        self.assertNotIn("refresh_token", captured["body"])
        self.assertTrue(result["success"])

    def test_logout_ignores_server_failure(self):
        api = _login_then(lambda request: err(500, "internal", "boom"))
        result = commands.run_logout(api)
        self.assertTrue(result["success"])
        self.assertIn("warning", result)


# 服务端真实 SSE 形态（system/handler.go）：`id:` + `data:` 行、空行分隔，
# 无 `event:` 行，事件类型在 data JSON 的 type 字段；注释行仅保活。
UPDATE_SSE_BODY = "\n".join([
    ": keepalive",
    "",
    'id: 1',
    'data: {"seq":1,"ts":"2026-08-23T10:00:00Z","type":"step","step":1,"step_total":7,"title":"准备工作"}',
    "",
    'id: 2',
    'data: {"seq":2,"type":"line","text":"git pull 已是最新"}',
    "",
    'id: 3',
    'data: {"seq":3,"type":"done","exit_code":0,"success":true,"old_sha":"aaaa1111","new_sha":"bbbb2222"}',
    "",
    "",
]).encode("utf-8")


class _EnvelopeAPI:
    """模拟未解 WriteSuccess 信封的 client（run_update_trigger 兜底解包用）。"""

    def request(self, method, path, params=None, json=None, headers=None):
        return {"data": {"session_id": "upd_9", "current": "abc"}, "request_id": "req_1"}


class TestUpdateCommands(unittest.TestCase):
    def test_update_status(self):
        captured = {}

        def handler(request):
            captured.update(method=request.method, path=request.url.path)
            return ok({"current": "fc9e4f7", "latest": "b0ef9e7", "behind": 3,
                       "can_update": True})

        api = make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")
        result = commands.run_update_status(api)
        self.assertEqual((captured["method"], captured["path"]),
                         ("GET", "/api/v1/admin/system/version"))
        self.assertEqual(result["behind"], 3)
        self.assertTrue(result["can_update"])

    def test_update_trigger(self):
        captured = {}

        def handler(request):
            captured["method"] = request.method
            captured["path"] = request.url.path
            captured["idempotency"] = request.headers.get("Idempotency-Key", "")
            return ok({"session_id": "upd_1", "current": "fc9e4f7"})

        api = make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")
        result = commands.run_update_trigger(api)
        self.assertEqual((captured["method"], captured["path"]),
                         ("POST", "/api/v1/admin/system/update"))
        self.assertTrue(captured["idempotency"])  # 写操作自动带幂等键
        self.assertEqual(result, {"session_id": "upd_1", "current": "fc9e4f7"})

    def test_update_trigger_unwraps_envelope(self):
        result = commands.run_update_trigger(_EnvelopeAPI())
        self.assertEqual(result, {"session_id": "upd_9", "current": "abc"})

    def test_update_trigger_409_passthrough(self):
        api = make_api(lambda request: err(409, "update_in_progress", "已有更新任务正在执行"),
                       access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")
        with self.assertRaises(LabctlError) as cm:
            commands.run_update_trigger(api)
        self.assertEqual(cm.exception.code, "update_in_progress")
        self.assertEqual(cm.exception.status, 409)
        self.assertIn("已有更新任务正在执行", str(cm.exception))

    def test_update_stream_parses_sse(self):
        captured = {}

        def handler(request):
            captured["auth"] = request.headers.get("Authorization", "")
            captured["accept"] = request.headers.get("Accept", "")
            captured["path"] = request.url.path
            return httpx.Response(200, content=UPDATE_SSE_BODY,
                                  headers={"content-type": "text/event-stream"})

        api = make_api(handler, access_token="at_1", refresh_token="rt_1")
        events = list(commands.run_update_stream(api, "upd_1", timeout_s=5))
        self.assertEqual(captured["path"], "/api/v1/admin/system/update/stream/upd_1")
        self.assertEqual(captured["auth"], "Bearer at_1")
        self.assertEqual(captured["accept"], "text/event-stream")
        self.assertEqual([event for event, _ in events], ["step", "line", "done"])
        self.assertEqual(events[0][1]["title"], "准备工作")
        self.assertEqual(events[1][1]["text"], "git pull 已是最新")
        self.assertTrue(events[2][1]["success"])
        self.assertEqual(events[2][1]["exit_code"], 0)

    def test_update_stream_event_line_wins_and_raw_payload(self):
        body = "\n".join([
            "event: status",
            'data: {"type":"line","text":"x"}',
            "",
            "data: not-json",
            "",
            "",
        ]).encode("utf-8")
        api = make_api(lambda request: httpx.Response(200, content=body,
                                                      headers={"content-type": "text/event-stream"}),
                       access_token="at_1")
        events = list(commands.run_update_stream(api, "upd_1"))
        # event: 行优先于 data JSON 的 type；非 JSON data 原文透出
        self.assertEqual(events[0], ("status", {"type": "line", "text": "x"}))
        self.assertEqual(events[1], ("message", "not-json"))

    def test_update_stream_non_200_raises(self):
        api = make_api(lambda request: err(404, "session_not_found", "session 不存在"),
                       access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            list(commands.run_update_stream(api, "upd_gone"))
        self.assertEqual(cm.exception.status, 404)
        self.assertEqual(cm.exception.code, "stream_failed")
        self.assertIn("session_not_found", str(cm.exception))  # 响应体片段入 message

    def test_update_stream_requires_session(self):
        api = make_api(lambda request: ok({}), access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            list(commands.run_update_stream(api, ""))
        self.assertEqual(cm.exception.code, "bad_request")

    def test_update_stream_requires_login(self):
        api = make_api(lambda request: ok({}), access_token="")
        with self.assertRaises(LabctlError) as cm:
            list(commands.run_update_stream(api, "upd_1"))
        self.assertEqual(cm.exception.code, "not_logged_in")

    def test_update_stream_service_token_header(self):
        os = __import__("os")
        os.environ["LABCTL_SERVICE_TOKEN"] = "svc_jwt"
        try:
            captured = {}

            def handler(request):
                captured["auth"] = request.headers.get("Authorization", "")
                return httpx.Response(200, content=UPDATE_SSE_BODY)

            api = make_api(handler, access_token="at_1")
            list(commands.run_update_stream(api, "upd_1"))
            self.assertEqual(captured["auth"], "Bearer svc_jwt")  # 服务 token 优先
        finally:
            os.environ.pop("LABCTL_SERVICE_TOKEN", None)


def json_roundtrip(text):
    import json
    return json.loads(text)


if __name__ == "__main__":
    unittest.main()
