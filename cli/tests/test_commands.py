import json
import sys
import unittest
from pathlib import Path

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

    def test_logs_get(self):
        self._assert_happy(lambda api: commands.run_logs_get(api, "log_1"),
                           "GET", "/api/v1/logs/log_1")

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
            captured["body"] = json_roundtrip(request.read().decode())
            return ok({"success": True})

        api = _login_then(handler)
        result = commands.run_logout(api)
        self.assertEqual(captured["body"], {"refresh_token": "rt_1"})
        self.assertTrue(result["success"])

    def test_logout_ignores_server_failure(self):
        api = _login_then(lambda request: err(500, "internal", "boom"))
        result = commands.run_logout(api)
        self.assertTrue(result["success"])
        self.assertIn("warning", result)


def json_roundtrip(text):
    import json
    return json.loads(text)


if __name__ == "__main__":
    unittest.main()
