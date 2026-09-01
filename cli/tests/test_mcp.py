import asyncio
import json
import sys
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from cli import commands  # noqa: E402
from cli.api_client import LabctlError  # noqa: E402
from cli.mcp_server import _session, mcp  # noqa: E402

EXPECTED_TOOLS = {
    "labctl_login", "labctl_logout", "labctl_whoami",
    "labctl_daily_report_today", "labctl_daily_report_history", "labctl_daily_report_entry",
    "labctl_daily_report_submit", "labctl_daily_report_ai_parse",
    "labctl_daily_report_by_date",
    "labctl_projects_list", "labctl_projects_get", "labctl_projects_create",
    "labctl_projects_update", "labctl_projects_transition",
    "labctl_projects_members_list", "labctl_projects_members_add",
    "labctl_projects_members_set_role", "labctl_projects_members_remove",
    "labctl_issues_list", "labctl_issues_create", "labctl_issues_transition",
    "labctl_issues_get", "labctl_issues_update", "labctl_issues_comment",
    "labctl_test_data_list", "labctl_test_data_entry",
    "labctl_test_data_batch", "labctl_test_data_get", "labctl_test_data_update",
    "labctl_test_data_invalidate",
    "labctl_runs_list", "labctl_runs_get", "labctl_runs_status",
    "labctl_runs_create", "labctl_runs_delete",
    "labctl_runs_steps_list", "labctl_runs_steps_add",
    "labctl_run_steps_status", "labctl_runs_steps_reorder",
    "labctl_runs_report_link", "labctl_runs_report_unlink",
    "labctl_alerts_list", "labctl_alerts_resolve", "labctl_alerts_get",
    "labctl_logs_list", "labctl_logs_get", "labctl_logs_create", "labctl_logs_update",
    "labctl_attachments_upload", "labctl_attachments_list", "labctl_attachments_download",
    "labctl_attachments_link", "labctl_attachments_unlink", "labctl_attachments_rm",
    "labctl_todos_list", "labctl_todos_add", "labctl_todos_edit",
    "labctl_todos_done", "labctl_todos_defer", "labctl_todos_rm",
    "labctl_audit_events", "labctl_audit_verify", "labctl_audit_get",
    "labctl_experiences_extract_candidates", "labctl_experiences_list",
    "labctl_experiences_get", "labctl_experiences_publish", "labctl_experiences_create",
    "labctl_experiences_update", "labctl_experiences_archive",
    "labctl_weekly_generate", "labctl_weekly_recent",
    "labctl_sensors_latest", "labctl_sensors_history",
    "labctl_ask_chat", "labctl_ask_history",
    "labctl_assembly_list", "labctl_assembly_transition",
    "labctl_step_templates_list", "labctl_step_templates_generate",
    "labctl_rf_matching_list", "labctl_rf_matching_create",
    "labctl_admin_users_list", "labctl_admin_users_create", "labctl_admin_users_set",
    "labctl_admin_users_reset_password",
    "labctl_admin_invites_list", "labctl_admin_invites_create",
    "labctl_admin_invites_revoke",
    "labctl_automation_rules_list", "labctl_automation_rules_create",
    "labctl_automation_rules_enable", "labctl_automation_rules_rm",
    "labctl_agent_candidates_list", "labctl_agent_candidates_trace",
    "labctl_agent_candidates_approve", "labctl_agent_candidates_reject",
    "labctl_instruments_list", "labctl_instruments_status",
    "labctl_instruments_whitelist", "labctl_instruments_parse_result",
    "labctl_update_status", "labctl_update_trigger",
}


class FakeAPI:
    """命令层同构的假客户端：不发起真实 HTTP。"""

    def __init__(self, result=None, error=None):
        self.result = result if result is not None else {"ok": True}
        self.error = error
        self.access_token = "at_fake"
        self.refresh_token = ""
        self.csrf_token = ""

    def request(self, method, path, params=None, json=None, headers=None):
        if self.error:
            raise self.error
        return self.result


class TestMCP(unittest.TestCase):
    def setUp(self):
        _session["api"] = None

    def tearDown(self):
        _session["api"] = None

    def test_tool_list_loads(self):
        tools = asyncio.run(mcp.list_tools())
        names = {t.name for t in tools}
        self.assertEqual(names, EXPECTED_TOOLS)
        self.assertTrue(all(name.startswith("labctl_") for name in names))
        for tool in tools:
            self.assertTrue(tool.description, f"{tool.name} 缺描述")
        descriptions = {tool.name: tool.description for tool in tools}
        self.assertIn("status=draft", descriptions["labctl_logs_list"])
        self.assertIn("只返回 confirmed", descriptions["labctl_logs_get"])

    def test_tool_call_alerts_list(self):
        _session["api"] = FakeAPI(result={"items": [{"id": "a_1", "level": "warning"}],
                                          "total": 1, "limit": 50, "offset": 0})
        content, _ = asyncio.run(mcp.call_tool("labctl_alerts_list", {"status": "active"}))
        payload = json.loads(content[0].text)
        self.assertEqual(payload["items"][0]["id"], "a_1")

    def test_tool_call_unauthenticated_returns_error_json(self):
        content, _ = asyncio.run(mcp.call_tool("labctl_alerts_list", {}))
        payload = json.loads(content[0].text)
        self.assertEqual(payload["error"]["code"], "not_logged_in")

    def test_tool_call_update(self):
        _session["api"] = FakeAPI(result={"current": "fc9e4f7", "latest": "b0ef9e7",
                                          "behind": 3, "can_update": True})
        content, _ = asyncio.run(mcp.call_tool("labctl_update_status", {}))
        payload = json.loads(content[0].text)
        self.assertEqual(payload["behind"], 3)

        _session["api"] = FakeAPI(result={"session_id": "upd_1", "current": "fc9e4f7"})
        content, _ = asyncio.run(mcp.call_tool("labctl_update_trigger", {}))
        payload = json.loads(content[0].text)
        self.assertEqual(payload["session_id"], "upd_1")

    def test_tool_call_error_passthrough(self):
        _session["api"] = FakeAPI(error=LabctlError("当前用户无权访问该项目",
                                                    code="permission_denied",
                                                    status=403, request_id="req_403"))
        content, _ = asyncio.run(mcp.call_tool("labctl_projects_list", {}))
        payload = json.loads(content[0].text)
        self.assertEqual(payload["error"]["code"], "permission_denied")
        self.assertEqual(payload["error"]["request_id"], "req_403")

    def test_login_establishes_session(self):
        import cli.mcp_server as mod

        original = mod.LabctlAPI

        class _StubAPI(original):
            def __init__(self, base_url="http://lab.test"):
                self.base_url = base_url
                self.username = ""
                self.access_token = ""
                self.refresh_token = ""
                self.csrf_token = ""
                self.client = None

            def login(self, username, password):
                self.username = username
                return {"access_token": "at", "refresh_token": "rt", "csrf_token": "cs",
                        "user": {"username": username, "roles": ["member"]}}

        mod.LabctlAPI = _StubAPI
        try:
            content, _ = asyncio.run(mcp.call_tool(
                "labctl_login", {"username": "zhangsan", "password": "secret"}))
            payload = json.loads(content[0].text)
            self.assertTrue(payload["success"])
            self.assertEqual(payload["user"]["username"], "zhangsan")
            self.assertIsNotNone(_session["api"])
            self.assertEqual(_session["api"].username, "zhangsan")
        finally:
            mod.LabctlAPI = original
            _session["api"] = None

    def test_login_failure_returns_error(self):
        import cli.mcp_server as mod

        original = mod.LabctlAPI

        class _StubAPI(original):
            def __init__(self, base_url="http://lab.test"):
                pass

            def login(self, username, password):
                raise LabctlError("用户名或密码错误", code="invalid_credentials",
                                  status=401, request_id="req_login")

        mod.LabctlAPI = _StubAPI
        try:
            content, _ = asyncio.run(mcp.call_tool(
                "labctl_login", {"username": "zhangsan", "password": "wrong"}))
            payload = json.loads(content[0].text)
            self.assertEqual(payload["error"]["code"], "invalid_credentials")
            self.assertEqual(payload["error"]["request_id"], "req_login")
            self.assertIsNone(_session["api"])
        finally:
            mod.LabctlAPI = original

    def test_logout_clears_session(self):
        _session["api"] = FakeAPI()
        content, _ = asyncio.run(mcp.call_tool("labctl_logout", {}))
        payload = json.loads(content[0].text)
        self.assertTrue(payload["success"])
        self.assertIsNone(_session["api"])

    def test_mcp_tools_reuse_commands(self):
        with mock.patch.object(commands, "run_alerts_list") as spy:
            spy.return_value = {"items": [], "total": 0}
            _session["api"] = FakeAPI()
            asyncio.run(mcp.call_tool("labctl_alerts_list", {}))
            spy.assert_called_once()

        with mock.patch.object(commands, "run_projects_create") as spy:
            spy.return_value = {"id": "prj_1"}
            _session["api"] = FakeAPI()
            asyncio.run(mcp.call_tool("labctl_projects_create",
                                      {"code": "a", "name": "项目A"}))
            spy.assert_called_once()


if __name__ == "__main__":
    unittest.main()
