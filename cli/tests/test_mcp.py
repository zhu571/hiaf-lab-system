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
    "labctl_projects_list", "labctl_projects_get", "labctl_projects_create",
    "labctl_issues_list", "labctl_issues_create", "labctl_issues_transition",
    "labctl_test_data_list", "labctl_test_data_entry",
    "labctl_runs_list", "labctl_runs_get", "labctl_runs_status",
    "labctl_alerts_list", "labctl_alerts_resolve",
    "labctl_logs_list", "labctl_logs_get",
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
