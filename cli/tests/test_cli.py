import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from click.testing import CliRunner  # noqa: E402

import cli.auth as auth  # noqa: E402
import cli.cli as cli_module  # noqa: E402
from cli.api_client import LabctlAPI, LabctlError  # noqa: E402
from cli.cli import cli  # noqa: E402

from cli.tests.helpers import TOKENS, err, make_api, ok  # noqa: E402


class StubAPI(LabctlAPI):
    """login 命令直接构造 LabctlAPI（不走 build_api），用桩替身。"""

    def __init__(self, base_url="http://lab.test", login_data=None, login_error=None):
        self.base_url = base_url
        self.login_data = login_data
        self.login_error = login_error
        self.access_token = ""
        self.refresh_token = ""
        self.csrf_token = ""
        self.username = ""
        self.client = None

    def login(self, username, password):
        if self.login_error:
            raise self.login_error
        self.username = username
        self.access_token = self.login_data["access_token"]
        self.refresh_token = self.login_data["refresh_token"]
        self.csrf_token = self.login_data["csrf_token"]
        return self.login_data

    def to_token_payload(self):
        return {"base_url": self.base_url, "username": self.username,
                "access_token": self.access_token, "refresh_token": self.refresh_token,
                "csrf_token": self.csrf_token}


class BaseCliTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self._old_dir = os.environ.get("LABCTL_TOKEN_DIR")
        os.environ["LABCTL_TOKEN_DIR"] = self._tmp.name
        auth.TOKEN_DIR = __import__("pathlib").Path(self._tmp.name)
        auth.TOKEN_FILE = auth.TOKEN_DIR / "token"
        self.runner = CliRunner()

    def tearDown(self):
        if self._old_dir is None:
            os.environ.pop("LABCTL_TOKEN_DIR", None)
        else:
            os.environ["LABCTL_TOKEN_DIR"] = self._old_dir
        self._tmp.cleanup()

    def invoke(self, args, api=None):
        patcher = mock.patch.object(cli_module, "build_api", return_value=api)
        with patcher:
            return self.runner.invoke(cli, args)


class TestCliLogin(BaseCliTest):
    def test_login_saves_token_file(self):
        stub = StubAPI(login_data=TOKENS)
        with mock.patch.object(cli_module, "LabctlAPI", return_value=stub):
            result = self.runner.invoke(cli, ["login", "zhangsan"], input="secret\n")
        self.assertEqual(result.exit_code, 0, result.output)
        data = auth.load_token()
        self.assertIsNotNone(data)
        self.assertEqual(data["access_token"], "at_1")
        self.assertEqual(data["username"], "zhangsan")
        self.assertNotIn("password", json.dumps(data).lower())

    def test_login_failure_401_exit_2(self):
        stub = StubAPI(login_error=LabctlError("用户名或密码错误", code="invalid_credentials",
                                               status=401, request_id="req_login"))
        with mock.patch.object(cli_module, "LabctlAPI", return_value=stub):
            result = self.runner.invoke(cli, ["login", "zhangsan"], input="wrong\n")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("req_login", result.output)
        self.assertIsNone(auth.load_token())

    def test_logout_clears_token(self):
        auth.save_token({"access_token": "at", "refresh_token": "rt", "csrf_token": "cs",
                         "username": "zhangsan", "base_url": "http://lab.test"})
        # _do_logout 直接走 LabctlAPI.from_stored（不经 build_api），
        # 必须在这里注入 MockTransport 桩，否则会真实 DNS 解析 lab.test（~3s）
        api = make_api(lambda request: ok({"success": True}),
                       access_token="at", refresh_token="rt", csrf_token="cs")
        with mock.patch.object(cli_module, "LabctlAPI") as mock_cls:
            mock_cls.from_stored.return_value = api
            result = self.runner.invoke(cli, ["login", "--logout"])
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIsNone(auth.load_token())

    def test_whoami(self):
        api = make_api(lambda request: ok({"user": {"username": "zhangsan", "roles": ["member"]}}),
                       access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")
        result = self.invoke(["login", "--whoami"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("zhangsan", result.output)

    def test_token_stdin(self):
        stub = StubAPI(login_data=TOKENS)
        with mock.patch.object(cli_module, "LabctlAPI", return_value=stub):
            result = self.runner.invoke(cli, ["login", "--token-stdin"],
                                        input="zhangsan\nsecret\n")
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(auth.load_token()["username"], "zhangsan")


class TestCliSubcommands(BaseCliTest):
    def _api(self, handler):
        return make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")

    def test_daily_report_today_json(self):
        api = self._api(lambda request: ok({"id": "rep_1", "status": "draft"}))
        result = self.invoke(["daily-report", "today"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("rep_1", result.output)

    def test_daily_report_history_human(self):
        api = self._api(lambda request: ok({
            "items": [{"id": "rep_1", "status": "submitted", "date": "2026-08-11"}],
            "total": 1, "page": 1}))
        result = self.invoke(["--human", "daily-report", "history"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("共 1 条", result.output)

    def test_projects_list(self):
        api = self._api(lambda request: ok([{"id": "prj_1", "code": "prj_a", "name": "项目A"}]))
        result = self.invoke(["projects", "list"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("prj_a", result.output)

    def test_projects_create_403_passthrough_with_request_id(self):
        api = self._api(lambda request: err(403, "permission_denied", "当前用户无权访问该项目",
                                            "req_403"))
        result = self.invoke(["projects", "create", "--code", "x", "--name", "y"], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn("permission_denied", result.output)
        self.assertIn("req_403", result.output)

    def test_projects_create_required_missing(self):
        api = self._api(lambda request: ok({}))
        result = self.invoke(["projects", "create", "--name", "y"], api=api)
        self.assertEqual(result.exit_code, 2)  # click 参数校验失败

    def test_issues_transition_404(self):
        api = self._api(lambda request: err(404, "issue_not_found", "问题不存在", "req_404"))
        result = self.invoke(["issues", "transition", "iss_1",
                              "--target-status", "resolved"], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn("req_404", result.output)

    def test_test_data_entry_happy(self):
        api = self._api(lambda request: ok({"id": "td_1", "value": 4.2}))
        result = self.invoke(
            ["test-data", "entry", "prj_1", "--data-type", "cryo", "--measurement",
             "target_temp", "--value", "4.2"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("td_1", result.output)

    def test_runs_status_happy(self):
        api = self._api(lambda request: ok({"id": "run_1", "status": "completed"}))
        result = self.invoke(["runs", "status", "run_1", "--action", "complete"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("completed", result.output)

    def test_projects_transition_happy(self):
        api = self._api(lambda request: ok({"id": "prj_1", "status": "active"}))
        result = self.invoke(["projects", "transition", "prj_1", "--action", "activate"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("active", result.output)

    def test_projects_transition_invalid_action_rejected(self):
        api = self._api(lambda request: ok({}))
        result = self.invoke(["projects", "transition", "prj_1", "--action", "explode"], api=api)
        self.assertEqual(result.exit_code, 2)  # click 参数校验失败
        self.assertIn("explode", result.output)

    def test_alerts_list_and_resolve(self):
        api = self._api(lambda request: ok({"items": [{"id": "a_1", "level": "warning",
                                                       "title": "lab-server 探测失败"}],
                                            "total": 1, "limit": 50, "offset": 0}))
        result = self.invoke(["alerts", "list"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("a_1", result.output)

        api2 = self._api(lambda request: ok({"resolved": True}))
        result2 = self.invoke(["alerts", "resolve", "a_1"], api=api2)
        self.assertEqual(result2.exit_code, 0, result2.output)

    def test_logs_get_401_exit_2(self):
        api = make_api(lambda request: err(401, "unauthorized", "登录已过期", "req_401"),
                       access_token="at_1", refresh_token="rt_1")
        result = self.invoke(["logs", "get", "log_1"], api=api)
        self.assertEqual(result.exit_code, 2)
        self.assertIn("auth_expired", result.output)
        self.assertIn("请重新执行 labctl login", result.output)

    def test_weekly_generate_and_recent(self):
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            if request.url.path == "/api/v1/weekly/summary":
                return ok({"id": "exp_1", "title": "周报 2026-08-03 ~ 2026-08-09",
                           "summary": "本周完成匹配电路装配。", "week_start": "2026-08-03",
                           "week_end": "2026-08-09", "reused": False})
            return ok({"items": [{"id": "exp_1", "title": "周报 2026-08-03 ~ 2026-08-09",
                                  "status": "published", "tags": ["weekly_summary"]}],
                       "total": 1, "page": 1, "per_page": 5})

        api = self._api(handler)
        result = self.invoke(["weekly", "generate"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("周报 2026-08-03 ~ 2026-08-09", result.output)

        result2 = self.invoke(["weekly", "recent", "--limit", "5"], api=api)
        self.assertEqual(result2.exit_code, 0, result2.output)
        self.assertEqual(captured["path"], "/api/v1/experiences")

    def test_weekly_recent_human(self):
        api = self._api(lambda request: ok({
            "items": [{"id": "exp_1", "title": "周报 2026-08-03 ~ 2026-08-09",
                       "status": "published"}],
            "total": 1, "page": 1, "per_page": 5}))
        result = self.invoke(["--human", "weekly", "recent"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("共 1 条", result.output)

    def test_experiences_extract_candidates(self):
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            captured["body"] = request.read().decode()
            return ok({"items": [{"experience": {"id": "exp_1", "status": "candidate"},
                                  "issue_id": "iss_1", "confidence": 0.88}], "total": 1})

        api = self._api(handler)
        result = self.invoke(["experiences", "extract-candidates"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["path"], "/api/v1/experiences/extract-candidates")
        self.assertIn("exp_1", result.output)

        result2 = self.invoke(["experiences", "extract-candidates", "--days", "14"], api=api)
        self.assertEqual(result2.exit_code, 0, result2.output)
        self.assertIn('"days":14', captured["body"])

    def test_experiences_extract_candidates_invalid_days(self):
        api = self._api(lambda request: ok({}))
        result = self.invoke(["experiences", "extract-candidates", "--days", "0"], api=api)
        self.assertEqual(result.exit_code, 1, result.output)
        self.assertIn("bad_request", result.output)

    def test_experiences_list_candidates(self):
        captured = {}

        def handler(request):
            captured["params"] = dict(request.url.params)
            return ok({"items": [{"id": "exp_1", "title": "候选经验", "status": "candidate"}],
                       "total": 1, "page": 1, "per_page": 20})

        api = self._api(handler)
        result = self.invoke(["experiences", "list", "--status", "candidate"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["params"]["status"], "candidate")

    def test_experiences_publish(self):
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            return ok({"id": "exp_1", "status": "published"})

        api = self._api(handler)
        result = self.invoke(["experiences", "publish", "exp_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["path"], "/api/v1/experiences/exp_1/publish")
        self.assertIn("published", result.output)

    def test_logs_list_status_draft(self):
        captured = {}

        def handler(request):
            captured["params"] = dict(request.url.params)
            return ok({"items": [{"id": "log_1", "status": "draft"}], "total": 1})

        api = self._api(handler)
        result = self.invoke(["logs", "list", "prj_1", "--status", "draft"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["params"].get("status"), "draft")
        self.assertIn("log_1", result.output)

    def test_logs_list_invalid_status_rejected(self):
        api = self._api(lambda request: ok({}))
        result = self.invoke(["logs", "list", "prj_1", "--status", "bogus"], api=api)
        self.assertEqual(result.exit_code, 2)  # click 参数校验失败
        self.assertIn("bogus", result.output)


class TestCliServiceToken(BaseCliTest):
    def test_service_token_reads(self):
        os.environ["LABCTL_SERVICE_TOKEN"] = "svc_jwt"
        try:
            captured = {}

            def handler(request):
                captured["auth"] = request.headers.get("Authorization")
                return ok([{"id": "prj_1"}])

            api = make_api(handler, access_token="")
            result = self.invoke(["projects", "list"], api=api)
            self.assertEqual(result.exit_code, 0, result.output)
            self.assertIn("prj_1", result.output)
        finally:
            os.environ.pop("LABCTL_SERVICE_TOKEN", None)

    def test_no_token_anywhere(self):
        api = make_api(handler=lambda request: ok([]), access_token="")
        result = self.invoke(["projects", "list"], api=api)
        self.assertEqual(result.exit_code, 2)
        self.assertIn("not_logged_in", result.output)


if __name__ == "__main__":
    unittest.main()
