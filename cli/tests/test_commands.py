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

    def test_daily_report_entry_patch_summary(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_entry(api, "rep_1", summary="一句话摘要"),
            "PATCH", "/api/v1/daily-reports/rep_1", body={"summary": "一句话摘要"})

    def test_daily_report_entry_patch_both(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_entry(api, "rep_1", raw_text="正文", summary="摘要"),
            "PATCH", "/api/v1/daily-reports/rep_1", body={"raw_text": "正文", "summary": "摘要"})

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

    def test_projects_transition_ignore_warnings(self):
        self._assert_happy(
            lambda api: commands.run_projects_transition(
                api, "prj_1", "complete", ignore_warnings=True, reason="遗留 issue 已确认不阻塞"),
            "POST", "/api/v1/projects/prj_1/transition",
            body={"action": "complete", "ignore_warnings": True, "reason": "遗留 issue 已确认不阻塞"})

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

    def test_test_data_entry_source_and_outlier(self):
        self._assert_happy(
            lambda api: commands.run_test_data_entry(api, "prj_1", "pressure", "cell_pressure",
                                                     9.9, quality="outlier", source="import"),
            "POST", "/api/v1/projects/prj_1/test-data",
            body={"data_type": "pressure", "measurement": "cell_pressure", "value": 9.9,
                  "quality": "outlier", "source": "import"})

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


class TestLogsCreate(unittest.TestCase):
    def test_happy(self):
        captured = {}

        def handler(request):
            captured.update(method=request.method, path=request.url.path)
            captured["body"] = json_roundtrip(request.read().decode())
            return ok({"id": "log_1", "category": "rf", "content_status": "draft"})

        api = _login_then(handler)
        result = commands.run_logs_create(api, "prj_1", "RF 匹配完成", category="rf")
        self.assertEqual((captured["method"], captured["path"]),
                         ("POST", "/api/v1/projects/prj_1/logs"))
        self.assertEqual(captured["body"], {"category": "rf", "content": "RF 匹配完成"})
        self.assertEqual(result["id"], "log_1")

    def test_with_report_linkage(self):
        captured = {}

        def handler(request):
            captured["body"] = json_roundtrip(request.read().decode())
            return ok({"id": "log_1"})

        api = _login_then(handler)
        commands.run_logs_create(api, "prj_1", "正文", daily_report_id="rep_1",
                                 raw_snippet="正文片段", occurred_at="2026-09-01T10:00:00+08:00")
        self.assertEqual(captured["body"], {
            "category": "general", "content": "正文", "daily_report_id": "rep_1",
            "raw_snippet": "正文片段", "occurred_at": "2026-09-01T10:00:00+08:00"})

    def test_no_source_field_sent(self):
        captured = {}

        def handler(request):
            captured["body"] = json_roundtrip(request.read().decode())
            return ok({"id": "log_1"})

        api = _login_then(handler)
        commands.run_logs_create(api, "prj_1", "x")
        self.assertNotIn("source", captured["body"])  # CLI 一律 manual，走服务端默认

    def test_invalid_category(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_logs_create(api, "prj_1", "x", category="bogus")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_snippet_requires_report(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_logs_create(api, "prj_1", "x", raw_snippet="片段")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_content_required(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_logs_create(api, "prj_1", "")
        self.assertEqual(cm.exception.code, "bad_request")


class TestAttachments(unittest.TestCase):
    def _upload_api(self, captured):
        def handler(request):
            captured.update(method=request.method, path=request.url.path,
                            content_type=request.headers.get("content-type", ""),
                            idempotency=request.headers.get("Idempotency-Key", ""))
            captured["body"] = request.read()
            return ok({"attachment": {"id": "att_1", "original_name": "a.png"},
                       "links": []})

        return _login_then(handler)

    def test_upload_multipart(self):
        import tempfile
        captured = {}
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as fh:
            fh.write(b"\x89PNG fake")
            path = fh.name
        api = self._upload_api(captured)
        result = commands.run_attachments_upload(api, path, entity_type="log",
                                                 entity_id="log_1", description="示意图")
        self.assertEqual((captured["method"], captured["path"]), ("POST", "/api/v1/attachments"))
        self.assertIn("multipart/form-data", captured["content_type"])
        self.assertTrue(captured["idempotency"])
        body = captured["body"]
        self.assertIn(b'name="entity_type"', body)
        self.assertIn(b"log_1", body)
        self.assertIn(b'filename="', body)
        self.assertIn(b"\x89PNG fake", body)
        self.assertEqual(result["attachment"]["id"], "att_1")

    def test_upload_entity_pair_validated(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_attachments_upload(api, "/etc/hosts", entity_type="log")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_upload_entity_type_validated(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_attachments_upload(api, "/etc/hosts", entity_type="bogus",
                                            entity_id="x")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_upload_size_precheck(self):
        import tempfile
        from unittest import mock
        captured = {}
        with tempfile.NamedTemporaryFile(delete=False) as fh:
            fh.write(b"12345678")
            path = fh.name
        api = self._upload_api(captured)
        with mock.patch.object(commands, "MAX_UPLOAD_BYTES", 4):
            with self.assertRaises(LabctlError) as cm:
                commands.run_attachments_upload(api, path)
        self.assertEqual(cm.exception.code, "attachment_too_large")
        self.assertNotIn("path", captured)  # 未发包，本地直接拒绝

    def test_upload_missing_file(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_attachments_upload(api, "/no/such/file.bin")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_list_own(self):
        self._assert = None
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            captured["params"] = dict(request.url.params)
            return ok({"items": [{"id": "att_1"}], "total": 1})

        api = _login_then(handler)
        commands.run_attachments_list(api)
        self.assertEqual(captured["path"], "/api/v1/attachments")
        self.assertEqual(captured["params"], {"page": "1", "per_page": "20"})

    def test_list_by_entity(self):
        captured = {}

        def handler(request):
            captured["params"] = dict(request.url.params)
            return ok({"items": [], "total": 0})

        api = _login_then(handler)
        commands.run_attachments_list(api, entity_type="log", entity_id="log_1", page=2)
        self.assertEqual(captured["params"], {"entity_type": "log", "entity_id": "log_1",
                                              "page": "2", "per_page": "20"})

    def test_list_entity_pair_validated(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_attachments_list(api, entity_type="log")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_download(self):
        import hashlib
        import tempfile
        payload = b"attachment-bytes"
        sha = hashlib.sha256(payload).hexdigest()
        calls = []

        def handler(request):
            calls.append(request.url.path)
            if request.url.path == "/api/v1/attachments/att_1":
                return ok({"id": "att_1", "original_name": "曲线.png", "mime_type": "image/png",
                           "sha256": sha})
            return httpx.Response(200, content=payload)

        api = _login_then(handler)
        with tempfile.TemporaryDirectory() as tmp:
            dest = tmp + "/out.bin"
            result = commands.run_attachments_download(api, "att_1", output=dest)
        self.assertEqual(calls, ["/api/v1/attachments/att_1",
                                 "/api/v1/attachments/att_1/content"])
        self.assertEqual(result["path"], dest)
        self.assertEqual(result["size"], len(payload))
        self.assertEqual(result["sha256"], sha)
        self.assertTrue(result["sha256_match"])
        self.assertEqual(result["original_name"], "曲线.png")

    def test_download_sha256_mismatch_flagged(self):
        import tempfile

        def handler(request):
            if request.url.path == "/api/v1/attachments/att_1":
                return ok({"id": "att_1", "original_name": "x", "sha256": "deadbeef"})
            return httpx.Response(200, content=b"different")

        api = _login_then(handler)
        with tempfile.TemporaryDirectory() as tmp:
            result = commands.run_attachments_download(api, "att_1", output=tmp + "/o")
        self.assertFalse(result["sha256_match"])

    def test_download_default_uses_original_name(self):
        import os
        import tempfile

        def handler(request):
            if request.url.path == "/api/v1/attachments/att_1":
                return ok({"id": "att_1", "original_name": "default-name.txt", "sha256": ""})
            return httpx.Response(200, content=b"x")

        api = _login_then(handler)
        cwd = os.getcwd()
        with tempfile.TemporaryDirectory() as tmp:
            os.chdir(tmp)
            try:
                result = commands.run_attachments_download(api, "att_1")
            finally:
                os.chdir(cwd)
        self.assertEqual(result["path"], "default-name.txt")
        self.assertNotIn("sha256_match", result)  # 服务端无 sha256 时不比对


class CommandCase(unittest.TestCase):
    """P1/P2 新命令共用：断言 method/path/body/params 的快乐路径基类。"""

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


class TestP1DailyReportAndLogs(CommandCase):
    def test_daily_report_submit_default(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_submit(api, "rep_1"),
            "POST", "/api/v1/daily-reports/rep_1/submit", body={"force": False})

    def test_daily_report_submit_force(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_submit(api, "rep_1", force=True),
            "POST", "/api/v1/daily-reports/rep_1/submit", body={"force": True})

    def test_daily_report_ai_parse(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_ai_parse(api, "rep_1"),
            "POST", "/api/v1/daily-reports/rep_1/ai-parse")

    def test_logs_update_fields(self):
        self._assert_happy(
            lambda api: commands.run_logs_update(api, "log_1", content="新正文",
                                                 category="rf",
                                                 occurred_at="2026-09-01T09:00:00+08:00"),
            "PATCH", "/api/v1/logs/log_1",
            body={"category": "rf", "content": "新正文",
                  "occurred_at": "2026-09-01T09:00:00+08:00"})

    def test_logs_update_confirm(self):
        self._assert_happy(
            lambda api: commands.run_logs_update(api, "log_1", confirm=True),
            "PATCH", "/api/v1/logs/log_1", body={"content_status": "confirmed"})

    def test_logs_update_requires_field(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_logs_update(api, "log_1")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_logs_update_invalid_category(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_logs_update(api, "log_1", category="bogus")
        self.assertEqual(cm.exception.code, "bad_request")


class TestP1IssuesAndProjects(CommandCase):
    def test_issues_get(self):
        self._assert_happy(lambda api: commands.run_issues_get(api, "iss_1"),
                           "GET", "/api/v1/issues/iss_1")

    def test_issues_update(self):
        self._assert_happy(
            lambda api: commands.run_issues_update(api, "iss_1", title="新标题",
                                                   severity="high", assignee_id="usr_2"),
            "PATCH", "/api/v1/issues/iss_1",
            body={"title": "新标题", "severity": "high", "assignee_id": "usr_2"})

    def test_issues_update_requires_field(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_issues_update(api, "iss_1")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_issues_update_invalid_severity(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_issues_update(api, "iss_1", severity="bogus")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_issues_comment(self):
        self._assert_happy(
            lambda api: commands.run_issues_comment(api, "iss_1", "已现场核实"),
            "POST", "/api/v1/issues/iss_1/comments", body={"content": "已现场核实"})

    def test_issues_comment_content_required(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_issues_comment(api, "iss_1", "")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_projects_update(self):
        self._assert_happy(
            lambda api: commands.run_projects_update(api, "prj_1", name="新名",
                                                     visibility="workspace",
                                                     comment_policy="members"),
            "PATCH", "/api/v1/projects/prj_1",
            body={"name": "新名", "visibility": "workspace", "comment_policy": "members"})

    def test_projects_update_requires_field(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_projects_update(api, "prj_1")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_projects_update_invalid_visibility(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_projects_update(api, "prj_1", visibility="private")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_projects_members_list(self):
        self._assert_happy(lambda api: commands.run_projects_members_list(api, "prj_1"),
                           "GET", "/api/v1/projects/prj_1/members")

    def test_projects_members_add(self):
        self._assert_happy(
            lambda api: commands.run_projects_members_add(api, "prj_1", "usr_2", "viewer"),
            "POST", "/api/v1/projects/prj_1/members",
            body={"user_id": "usr_2", "role": "viewer"})

    def test_projects_members_add_invalid_role(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_projects_members_add(api, "prj_1", "usr_2", "boss")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_projects_members_set_role(self):
        self._assert_happy(
            lambda api: commands.run_projects_members_set_role(api, "prj_1", "usr_2",
                                                               "maintainer"),
            "PATCH", "/api/v1/projects/prj_1/members/usr_2", body={"role": "maintainer"})

    def test_projects_members_remove(self):
        self._assert_happy(
            lambda api: commands.run_projects_members_remove(api, "prj_1", "usr_2"),
            "DELETE", "/api/v1/projects/prj_1/members/usr_2")


class TestP1TestDataAndRuns(CommandCase):
    def test_parse_batch_json(self):
        rows = commands.parse_test_data_batch(
            '[{"data_type":"cryo","measurement":"t","value":4.2,"unit":"K"},'
            '{"data_type":"pressure","measurement":"p","value":0}]')
        self.assertEqual(rows[0]["value"], 4.2)
        self.assertEqual(rows[1]["value"], 0)

    def test_parse_batch_csv(self):
        text = ("data_type,measurement,value,unit,quality,measured_at,run_id,notes\n"
                "cryo,target_temp,4.2,K,outlier,,run_1,\n"
                "pressure,cell_pressure,912,,,2026-09-01T10:00:00Z,,午间读数\n")
        rows = commands.parse_test_data_batch(text)
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0], {"data_type": "cryo", "measurement": "target_temp",
                                   "value": 4.2, "unit": "K", "quality": "outlier",
                                   "run_id": "run_1"})
        self.assertEqual(rows[1]["value"], 912.0)
        self.assertEqual(rows[1]["notes"], "午间读数")

    def test_parse_batch_csv_header_case_insensitive(self):
        rows = commands.parse_test_data_batch(
            "Data_Type,Measurement,Value\npressure,p,1.5")
        self.assertEqual(rows, [{"data_type": "pressure", "measurement": "p", "value": 1.5}])

    def test_parse_batch_bad_csv_header(self):
        with self.assertRaises(LabctlError) as cm:
            commands.parse_test_data_batch("foo,bar\n1,2")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_parse_batch_empty_and_too_many(self):
        with self.assertRaises(LabctlError) as cm:
            commands.parse_test_data_batch("[]")
        self.assertEqual(cm.exception.code, "bad_request")
        with self.assertRaises(LabctlError) as cm:
            commands.parse_test_data_batch(
                "[" + ",".join('{"data_type":"cryo","measurement":"m","value":1}' for _ in range(101)) + "]")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_parse_batch_bad_json(self):
        with self.assertRaises(LabctlError) as cm:
            commands.parse_test_data_batch("[{broken")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_test_data_batch_raw_array_body(self):
        captured = {}

        def handler(request):
            captured["raw"] = request.read().decode()
            return ok({"created": 2})

        api = _login_then(handler)
        rows = [{"data_type": "cryo", "measurement": "t", "value": 1.0},
                {"data_type": "cryo", "measurement": "t2", "value": 2.0}]
        commands.run_test_data_batch(api, "prj_1", rows)
        self.assertEqual(json_roundtrip(captured["raw"]), rows)  # 裸数组，非 {rows: [...]}
        self.assertEqual(captured["raw"].lstrip()[0], "[")

    def test_test_data_batch_validates_rows(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_test_data_batch(api, "prj_1", [])
        self.assertEqual(cm.exception.code, "bad_request")

    def test_test_data_get(self):
        self._assert_happy(lambda api: commands.run_test_data_get(api, "td_1"),
                           "GET", "/api/v1/test-data/td_1")

    def test_test_data_invalidate(self):
        self._assert_happy(lambda api: commands.run_test_data_invalidate(api, "td_1"),
                           "DELETE", "/api/v1/test-data/td_1")

    def test_runs_create(self):
        self._assert_happy(
            lambda api: commands.run_runs_create(api, "prj_1", "第 3 轮降温",
                                                 campaign="C1", run_type="cooldown",
                                                 gas_type="He", target_temp=4.2,
                                                 pressure_min=800, pressure_max=1200,
                                                 has_beam=False,
                                                 devices=["rf_carpet", "rfq"]),
            "POST", "/api/v1/projects/prj_1/experiment-runs",
            body={"name": "第 3 轮降温", "campaign": "C1", "run_type": "cooldown",
                  "gas_type": "He", "target_temp": 4.2, "pressure_min": 800,
                  "pressure_max": 1200, "has_beam": False,
                  "devices": ["rf_carpet", "rfq"]})

    def test_runs_create_invalid_device(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_runs_create(api, "prj_1", "x", devices=["bogus"])
        self.assertEqual(cm.exception.code, "bad_request")

    def test_runs_create_invalid_run_type(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_runs_create(api, "prj_1", "x", run_type="spin")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_runs_delete(self):
        self._assert_happy(lambda api: commands.run_runs_delete(api, "run_1"),
                           "DELETE", "/api/v1/experiment-runs/run_1")


class TestP1ExperiencesAndTodos(CommandCase):
    def test_parse_linked_projects(self):
        self.assertEqual(commands.parse_linked_projects(["prj_1", "prj_2:primary"]),
                         [{"project_id": "prj_1"},
                          {"project_id": "prj_2", "relation": "primary"}])
        self.assertIsNone(commands.parse_linked_projects([]))

    def test_parse_linked_projects_invalid_relation(self):
        with self.assertRaises(LabctlError) as cm:
            commands.parse_linked_projects(["prj_1:bogus"])
        self.assertEqual(cm.exception.code, "bad_request")

    def test_experiences_create(self):
        self._assert_happy(
            lambda api: commands.run_experiences_create(
                api, "标题", "内容", project_id="prj_1", tags=["a", "b"],
                linked_projects=[{"project_id": "prj_2", "relation": "primary"}]),
            "POST", "/api/v1/experiences",
            body={"project_id": "prj_1", "title": "标题", "content": "内容",
                  "tags": ["a", "b"],
                  "linked_projects": [{"project_id": "prj_2", "relation": "primary"}]})

    def test_experiences_update(self):
        self._assert_happy(
            lambda api: commands.run_experiences_update(api, "exp_1", title="新标题",
                                                        tags=["x"]),
            "PATCH", "/api/v1/experiences/exp_1",
            body={"title": "新标题", "tags": ["x"]})

    def test_experiences_update_requires_field(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_experiences_update(api, "exp_1")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_experiences_archive(self):
        self._assert_happy(lambda api: commands.run_experiences_archive(api, "exp_1"),
                           "POST", "/api/v1/experiences/exp_1/archive")

    def test_todos_list(self):
        self._assert_happy(
            lambda api: commands.run_todos_list(api, scope="mine", status="done", limit=10),
            "GET", "/api/v1/todos",
            params={"scope": "mine", "status": "done", "limit": "10"})

    def test_todos_list_invalid_scope(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_todos_list(api, scope="bogus")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_todos_add(self):
        self._assert_happy(
            lambda api: commands.run_todos_add(api, "换气瓶", priority="high",
                                               project_id="prj_1"),
            "POST", "/api/v1/todos",
            body={"title": "换气瓶", "priority": "high", "project_id": "prj_1"})

    def test_todos_edit_explicit_updated_at(self):
        self._assert_happy(
            lambda api: commands.run_todos_edit(api, "td_1", title="改后",
                                                updated_at="2026-09-01T08:00:00Z"),
            "PATCH", "/api/v1/todos/td_1",
            body={"title": "改后", "updated_at": "2026-09-01T08:00:00Z"})

    def test_todos_edit_clear_project(self):
        self._assert_happy(
            lambda api: commands.run_todos_edit(api, "td_1", clear_project=True,
                                                updated_at="2026-09-01T08:00:00Z"),
            "PATCH", "/api/v1/todos/td_1",
            body={"project_id": "", "updated_at": "2026-09-01T08:00:00Z"})

    def test_todos_edit_looks_up_updated_at_from_list(self):
        calls = []

        def handler(request):
            calls.append((request.method, request.url.path, dict(request.url.params)))
            if request.method == "GET":
                return ok({"items": [{"id": "td_1", "updated_at": "2026-09-01T07:00:00Z"}]})
            body = json_roundtrip(request.read().decode())
            self.assertEqual(body, {"title": "新标题",
                                    "updated_at": "2026-09-01T07:00:00Z"})
            return ok({"id": "td_1"})

        api = _login_then(handler)
        commands.run_todos_edit(api, "td_1", title="新标题")
        self.assertEqual(calls[0], ("GET", "/api/v1/todos", {"status": "all", "limit": "200"}))
        self.assertEqual(calls[1], ("PATCH", "/api/v1/todos/td_1", {}))

    def test_todos_edit_lookup_not_found(self):
        def handler(request):
            return ok({"items": [{"id": "other", "updated_at": "x"}]})

        api = _login_then(handler)
        with self.assertRaises(LabctlError) as cm:
            commands.run_todos_edit(api, "td_1", title="新标题")
        self.assertEqual(cm.exception.code, "bad_request")
        self.assertIn("--updated-at", str(cm.exception))

    def test_todos_done_defer_rm(self):
        self._assert_happy(lambda api: commands.run_todos_done(api, "td_1"),
                           "PATCH", "/api/v1/todos/td_1/done")
        self._assert_happy(lambda api: commands.run_todos_defer(api, "td_1"),
                           "PATCH", "/api/v1/todos/td_1/defer")
        self._assert_happy(lambda api: commands.run_todos_rm(api, "td_1"),
                           "DELETE", "/api/v1/todos/td_1")


class TestP1AuditAlertsAttachments(CommandCase):
    def test_audit_events(self):
        self._assert_happy(
            lambda api: commands.run_audit_events(api, action="logs.create",
                                                  user_id="u" * 36, page=2),
            "GET", "/api/v1/audit/events",
            params={"action": "logs.create", "user_id": "u" * 36, "page": "2",
                    "per_page": "20"})

    def test_audit_events_time_range(self):
        self._assert_happy(
            lambda api: commands.run_audit_events(api, from_="2026-09-01T00:00:00Z",
                                                  to_="2026-09-02T00:00:00Z"),
            "GET", "/api/v1/audit/events",
            params={"from": "2026-09-01T00:00:00Z", "to": "2026-09-02T00:00:00Z",
                    "page": "1", "per_page": "20"})

    def test_audit_verify_range(self):
        self._assert_happy(
            lambda api: commands.run_audit_verify(api, from_id=10, to_id=99),
            "GET", "/api/v1/audit/verify", params={"from_id": "10", "to_id": "99"})

    def test_audit_verify_default_no_params(self):
        self._assert_happy(lambda api: commands.run_audit_verify(api),
                           "GET", "/api/v1/audit/verify", params={})

    def test_audit_get(self):
        self._assert_happy(lambda api: commands.run_audit_get(api, "req_20260901_1"),
                           "GET", "/api/v1/audit/req_20260901_1")

    def test_alerts_get(self):
        self._assert_happy(lambda api: commands.run_alerts_get(api, "a_1"),
                           "GET", "/api/v1/alerts/a_1")

    def test_attachments_link(self):
        self._assert_happy(
            lambda api: commands.run_attachments_link(api, "att_1", "log", "log_1",
                                                      description="补充挂载"),
            "POST", "/api/v1/attachments/att_1/links",
            body={"entity_type": "log", "entity_id": "log_1", "description": "补充挂载"})

    def test_attachments_link_invalid_type(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_attachments_link(api, "att_1", "bogus", "x")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_attachments_unlink(self):
        self._assert_happy(
            lambda api: commands.run_attachments_unlink(api, "att_1", "lnk_1"),
            "DELETE", "/api/v1/attachments/att_1/links/lnk_1")

    def test_attachments_rm(self):
        self._assert_happy(lambda api: commands.run_attachments_rm(api, "att_1"),
                           "DELETE", "/api/v1/attachments/att_1")


class TestP2SensorsAskMisc(CommandCase):
    def test_sensors_latest(self):
        self._assert_happy(
            lambda api: commands.run_sensors_latest(api, tags="pressure,vacuum"),
            "GET", "/api/v1/sensors/latest", params={"tags": "pressure,vacuum"})

    def test_sensors_latest_no_tags(self):
        self._assert_happy(lambda api: commands.run_sensors_latest(api),
                           "GET", "/api/v1/sensors/latest", params={})

    def test_sensors_history_flux_literals(self):
        self._assert_happy(
            lambda api: commands.run_sensors_history(api, "cell_pressure",
                                                     from_="-24h", to="now()",
                                                     interval="5m"),
            "GET", "/api/v1/sensors/history",
            params={"tag": "cell_pressure", "from": "-24h", "to": "now()",
                    "interval": "5m"})

    def test_sensors_history_defaults(self):
        self._assert_happy(
            lambda api: commands.run_sensors_history(api, "cell_pressure"),
            "GET", "/api/v1/sensors/history",
            params={"tag": "cell_pressure", "from": "-1h"})

    def test_sensors_history_requires_tag(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_sensors_history(api, "")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_ask_chat(self):
        self._assert_happy(lambda api: commands.run_ask_chat(api, "上周降温到多少 K？"),
                           "POST", "/api/v1/ask/chat", body={"question": "上周降温到多少 K？"})

    def test_ask_history(self):
        self._assert_happy(lambda api: commands.run_ask_history(api, page=2),
                           "GET", "/api/v1/ask/history", params={"page": "2", "per_page": "20"})

    def test_experiences_get(self):
        self._assert_happy(lambda api: commands.run_experiences_get(api, "exp_1"),
                           "GET", "/api/v1/experiences/exp_1")

    def test_daily_report_by_date(self):
        self._assert_happy(
            lambda api: commands.run_daily_report_by_date(api, date="2026-08-30",
                                                          latest=True),
            "GET", "/api/v1/daily-reports/by-date",
            params={"date": "2026-08-30", "latest": "true"})

    def test_daily_report_by_date_default(self):
        self._assert_happy(lambda api: commands.run_daily_report_by_date(api),
                           "GET", "/api/v1/daily-reports/by-date", params={})

    def test_test_data_update(self):
        self._assert_happy(
            lambda api: commands.run_test_data_update(api, "td_1", value=4.5,
                                                      quality="outlier", notes="复测"),
            "PATCH", "/api/v1/test-data/td_1",
            body={"value": 4.5, "quality": "outlier", "notes": "复测"})

    def test_test_data_update_value_zero(self):
        self._assert_happy(
            lambda api: commands.run_test_data_update(api, "td_1", value=0.0),
            "PATCH", "/api/v1/test-data/td_1", body={"value": 0.0})

    def test_test_data_update_requires_field(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_test_data_update(api, "td_1")
        self.assertEqual(cm.exception.code, "bad_request")


class TestP2AssemblyTemplatesRF(CommandCase):
    def test_assembly_list(self):
        self._assert_happy(
            lambda api: commands.run_assembly_list(api, "prj_1", status="planned"),
            "GET", "/api/v1/projects/prj_1/assembly",
            params={"status": "planned", "page": "1", "per_page": "20"})

    def test_assembly_transition(self):
        self._assert_happy(
            lambda api: commands.run_assembly_transition(api, "ast_1", "start",
                                                         override_reason="依赖取消，人工确认越过"),
            "PATCH", "/api/v1/assembly/ast_1",
            body={"transition": "start", "override_reason": "依赖取消，人工确认越过"})

    def test_assembly_transition_invalid(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_assembly_transition(api, "ast_1", "explode")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_step_templates_list(self):
        self._assert_happy(
            lambda api: commands.run_step_templates_list(api, kind="assembly", q="降温"),
            "GET", "/api/v1/step-templates",
            params={"kind": "assembly", "q": "降温", "page": "1", "per_page": "20"})

    def test_step_templates_list_invalid_kind(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_step_templates_list(api, kind="bogus")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_step_templates_generate(self):
        self._assert_happy(
            lambda api: commands.run_step_templates_generate(
                api, "experiment", "生成降温流程", context={"gas": "He"}),
            "POST", "/api/v1/step-templates/generate",
            body={"kind": "experiment", "prompt": "生成降温流程", "context": {"gas": "He"}})


class TestP2AdminAutomationAgents(CommandCase):
    def test_admin_users_list(self):
        self._assert_happy(lambda api: commands.run_admin_users_list(api),
                           "GET", "/api/v1/admin/users")

    def test_admin_users_create(self):
        self._assert_happy(
            lambda api: commands.run_admin_users_create(api, "lisi",
                                                        display_name="李四",
                                                        role="member"),
            "POST", "/api/v1/admin/users",
            body={"username": "lisi", "display_name": "李四", "role": "member"})

    def test_admin_users_create_invalid_role(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_admin_users_create(api, "lisi", role="bogus")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_admin_users_set_disable(self):
        self._assert_happy(
            lambda api: commands.run_admin_users_set(api, "usr_1", disabled=True),
            "PATCH", "/api/v1/admin/users/usr_1", body={"disabled": True})

    def test_admin_users_set_requires_field(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_admin_users_set(api, "usr_1")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_admin_users_reset_password(self):
        self._assert_happy(
            lambda api: commands.run_admin_users_reset_password(api, "usr_1"),
            "POST", "/api/v1/admin/users/usr_1/reset-password", body={})

    def test_admin_invites(self):
        self._assert_happy(lambda api: commands.run_admin_invites_list(api),
                           "GET", "/api/v1/admin/invitation-codes")
        self._assert_happy(
            lambda api: commands.run_admin_invites_create(api, expires_at="2026-10-01T00:00:00Z"),
            "POST", "/api/v1/admin/invitation-codes",
            body={"expires_at": "2026-10-01T00:00:00Z"})
        self._assert_happy(
            lambda api: commands.run_admin_invites_revoke(api, "inv_1"),
            "POST", "/api/v1/admin/invitation-codes/inv_1/revoke")

    def test_automation_rules(self):
        self._assert_happy(lambda api: commands.run_automation_rules_list(api),
                           "GET", "/api/v1/admin/automation/rules")
        self._assert_happy(
            lambda api: commands.run_automation_rules_create(api, "日报提交自动解析"),
            "POST", "/api/v1/admin/automation/rules",
            body={"name": "日报提交自动解析", "trigger_event": "daily_report.submitted",
                  "action": {"type": "enqueue_agent_task"}})
        self._assert_happy(
            lambda api: commands.run_automation_rules_enable(api, "rule_1", False),
            "PATCH", "/api/v1/admin/automation/rules/rule_1", body={"enabled": False})
        self._assert_happy(
            lambda api: commands.run_automation_rules_rm(api, "rule_1"),
            "DELETE", "/api/v1/admin/automation/rules/rule_1")

    def test_agent_candidates(self):
        self._assert_happy(
            lambda api: commands.run_agent_candidates_list(api, status="pending_review"),
            "GET", "/api/v1/agent/candidates",
            params={"status": "pending_review", "page": "1", "per_page": "20"})
        self._assert_happy(
            lambda api: commands.run_agent_candidates_trace(api, "cand_1"),
            "GET", "/api/v1/agent/candidates/cand_1/trace")
        self._assert_happy(
            lambda api: commands.run_agent_candidates_approve(api, "cand_1"),
            "POST", "/api/v1/agent/candidates/cand_1/approve")
        self._assert_happy(
            lambda api: commands.run_agent_candidates_reject(api, "cand_1", reason="信息不足"),
            "POST", "/api/v1/agent/candidates/cand_1/reject", body={"reason": "信息不足"})

    def test_agent_candidates_invalid_status(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_agent_candidates_list(api, status="bogus")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_rf_matching_list(self):
        self._assert_happy(
            lambda api: commands.run_rf_matching_list(api, "prj_1", device="rfq",
                                                      status="pass"),
            "GET", "/api/v1/projects/prj_1/rf-matching",
            params={"device": "rfq", "status": "pass", "page": "1", "per_page": "20"})

    def test_rf_matching_create(self):
        self._assert_happy(
            lambda api: commands.run_rf_matching_create(
                api, "prj_1", "rfq", 18.3, "pass", s11=-15.2,
                input_freq=18.3, input_power=120.5, notes="匹配良好"),
            "POST", "/api/v1/projects/prj_1/rf-matching",
            body={"device": "rfq", "frequency_mhz": 18.3, "status": "pass",
                  "s11": -15.2, "input_freq": 18.3, "input_power": 120.5,
                  "notes": "匹配良好"})

    def test_rf_matching_create_validations(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError):
            commands.run_rf_matching_create(api, "prj_1", "bogus", 18.0, "pass")
        with self.assertRaises(LabctlError):
            commands.run_rf_matching_create(api, "prj_1", "rfq", 18.0, "bogus")
        with self.assertRaises(LabctlError):
            commands.run_rf_matching_create(api, "prj_1", "rfq", 0, "pass")
        with self.assertRaises(LabctlError):
            commands.run_rf_matching_create(api, "prj_1", "rfq", -1, "pass")


class TestP2InstrumentsAuthRunsSteps(CommandCase):
    def test_instruments_readonly(self):
        self._assert_happy(lambda api: commands.run_instruments_list(api),
                           "GET", "/api/v1/instruments")
        self._assert_happy(lambda api: commands.run_instruments_status(api, "keysight_33210a"),
                           "GET", "/api/v1/instruments/keysight_33210a/status")
        self._assert_happy(lambda api: commands.run_instruments_whitelist(api),
                           "GET", "/api/v1/instruments/whitelist")

    def test_instruments_parse_result(self):
        self._assert_happy(
            lambda api: commands.run_instruments_parse_result(
                api, "keysight_33210a", "MEAS:VOLT?", "4.21E+00"),
            "POST", "/api/v1/instruments/keysight_33210a/parse-result",
            body={"command": "MEAS:VOLT?", "response": "4.21E+00"})

    def test_login_password(self):
        self._assert_happy(
            lambda api: commands.run_login_password(api, "old", "NewStrong!2026"),
            "POST", "/api/v1/auth/change-password",
            body={"old_password": "old", "new_password": "NewStrong!2026"})

    def test_login_set_language(self):
        self._assert_happy(lambda api: commands.run_login_set_language(api, "en"),
                           "PATCH", "/api/v1/auth/profile", body={"language": "en"})

    def test_login_set_language_invalid(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_login_set_language(api, "fr")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_runs_steps(self):
        self._assert_happy(
            lambda api: commands.run_runs_steps_list(api, "run_1"),
            "GET", "/api/v1/experiment-runs/run_1/steps",
            params={"page": "1", "per_page": "50"})
        self._assert_happy(
            lambda api: commands.run_runs_steps_add(api, "run_1", "抽真空",
                                                    depends_on="st_0", step_order=0),
            "POST", "/api/v1/experiment-runs/run_1/steps",
            body={"name": "抽真空", "depends_on": "st_0", "step_order": 0})
        self._assert_happy(
            lambda api: commands.run_run_steps_status(api, "st_1", "complete"),
            "PATCH", "/api/v1/run-steps/st_1", body={"transition": "complete"})

    def test_run_steps_status_invalid(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_run_steps_status(api, "st_1", "explode")
        self.assertEqual(cm.exception.code, "bad_request")

    def test_runs_steps_reorder_json_string(self):
        self._assert_happy(
            lambda api: commands.run_runs_steps_reorder(
                api, "run_1", '[{"id":"st_2","step_order":1},{"id":"st_1","step_order":2}]'),
            "POST", "/api/v1/run-steps/reorder",
            body={"run_id": "run_1",
                  "steps": [{"id": "st_2", "step_order": 1},
                            {"id": "st_1", "step_order": 2}]})

    def test_runs_steps_reorder_validations(self):
        api = _login_then(lambda request: ok({}))
        with self.assertRaises(LabctlError) as cm:
            commands.run_runs_steps_reorder(api, "run_1", "not-json")
        self.assertEqual(cm.exception.code, "bad_request")
        with self.assertRaises(LabctlError) as cm:
            commands.run_runs_steps_reorder(api, "run_1", "[]")
        self.assertEqual(cm.exception.code, "bad_request")
        with self.assertRaises(LabctlError) as cm:
            commands.run_runs_steps_reorder(api, "run_1", '[{"id":"st_1"}]')
        self.assertEqual(cm.exception.code, "bad_request")

    def test_runs_report_link_unlink(self):
        self._assert_happy(
            lambda api: commands.run_runs_report_link(api, "run_1", "rep_1"),
            "POST", "/api/v1/experiment-runs/run_1/daily-reports/rep_1")
        self._assert_happy(
            lambda api: commands.run_runs_report_unlink(api, "run_1", "rep_1"),
            "DELETE", "/api/v1/experiment-runs/run_1/daily-reports/rep_1")


def json_roundtrip(text):
    import json
    return json.loads(text)


if __name__ == "__main__":
    unittest.main()
