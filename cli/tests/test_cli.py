import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import httpx

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
            "items": [{"id": "rep_1", "content_status": "submitted",
                       "report_date": "2026-08-11", "summary": "完成低温测试"}],
            "total": 1, "page": 1}))
        result = self.invoke(["--human", "daily-report", "history"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("2026-08-11 | submitted | 完成低温测试", result.output)

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

    def test_logs_list_human_and_project_name(self):
        paths = []

        def handler(request):
            paths.append(request.url.path)
            if request.url.path == "/api/v1/projects":
                return ok([{"id": "00000000-0000-0000-0000-000000000001",
                            "code": "HIAF", "name": "低温气体靶"}])
            return ok({"items": [{"id": "12345678-aaaa", "occurred_at":
                                   "2026-08-30T10:20:30+08:00", "category": "cryo",
                                   "content_status": "confirmed", "content": "第一行\n第二行"}],
                       "total": 1, "page": 1})

        result = self.invoke(["--human", "logs", "list", "低温"], api=self._api(handler))
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("第 1 页 / 共 1 条", result.output)
        self.assertIn("12345678 |", result.output)
        self.assertIn("cryo | confirmed | 第一行 第二行", result.output)
        self.assertEqual(paths[-1],
                         "/api/v1/projects/00000000-0000-0000-0000-000000000001/logs")

    def test_logs_get_human_sections(self):
        api = self._api(lambda request: ok({
            "id": "log_1", "project_id": "prj_1", "author_id": "usr_1",
            "occurred_at": "2026-08-30T10:20:30+08:00", "category": "rf",
            "content_status": "draft", "source": "manual", "content": "完整正文",
            "raw_snippet": "原文一\n原文二"}))
        result = self.invoke(["--human", "logs", "get", "log_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("元信息", result.output)
        self.assertIn("正文\n完整正文", result.output)
        self.assertIn("原文引用\n> 原文一\n> 原文二", result.output)


def _update_stream_response(lines):
    body = "\n".join(lines) + "\n"
    return httpx.Response(200, content=body.encode("utf-8"),
                          headers={"content-type": "text/event-stream"})


class TestCliUpdate(BaseCliTest):
    def _api(self, handler):
        return make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")

    @staticmethod
    def _stream_handler(lines, trigger=None):
        def handler(request):
            if request.url.path == "/api/v1/admin/system/update":
                return trigger or ok({"session_id": "upd_1", "current": "fc9e4f7"})
            return _update_stream_response(lines)
        return handler

    def test_update_status(self):
        api = self._api(lambda request: ok({"current": "fc9e4f7", "latest": "b0ef9e7",
                                            "behind": 3, "can_update": True}))
        result = self.invoke(["update", "status"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn('"behind": 3', result.output)
        self.assertIn("fc9e4f7", result.output)

    def test_update_run_streams_until_done_exit_0(self):
        lines = [
            'data: {"seq":1,"type":"step","step":3,"step_total":7,"title":"git pull"}',
            "",
            'data: {"seq":2,"type":"line","text":"Already up to date."}',
            "",
            'data: {"seq":3,"type":"done","exit_code":0,"success":true,"old_sha":"aaaa1111","new_sha":"bbbb2222"}',
            "",
        ]
        api = self._api(self._stream_handler(lines))
        result = self.invoke(["update", "run"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn('"session_id": "upd_1"', result.output)
        self.assertIn('"event": "step"', result.output)
        self.assertIn('"event": "done"', result.output)

    def test_update_run_human_render(self):
        lines = [
            'data: {"seq":1,"type":"step","step":3,"step_total":7,"title":"git pull"}',
            "",
            'data: {"seq":2,"type":"done","exit_code":0,"success":true,"old_sha":"aaaa1111","new_sha":"bbbb2222"}',
            "",
        ]
        api = self._api(self._stream_handler(lines))
        result = self.invoke(["--human", "update", "run"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("[UPDATE] 步骤 3/7：git pull", result.output)
        self.assertIn("[UPDATE] 完成 exit_code=0（aaaa1111..bbbb2222）", result.output)

    def test_update_run_error_event_exit_1(self):
        lines = [
            'data: {"seq":1,"type":"line","text":"fatal: 无法访问远程"}',
            "",
            'data: {"seq":2,"type":"error","message":"git pull 失败（exit 128）"}',
            "",
        ]
        api = self._api(self._stream_handler(lines))
        result = self.invoke(["update", "run"], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn('"event": "error"', result.output)

    def test_update_run_done_failed_exit_1(self):
        lines = [
            'data: {"seq":1,"type":"done","exit_code":1,"success":false}',
            "",
        ]
        api = self._api(self._stream_handler(lines))
        result = self.invoke(["update", "run"], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn('"event": "done"', result.output)

    def test_update_run_no_wait(self):
        captured = {}

        def handler(request):
            captured["method"] = request.method
            captured["path"] = request.url.path
            return ok({"session_id": "upd_1", "current": "fc9e4f7"})

        api = self._api(handler)
        result = self.invoke(["update", "run", "--no-wait"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["path"], "/api/v1/admin/system/update")
        self.assertIn('"session_id": "upd_1"', result.output)
        self.assertIn("/api/v1/admin/system/update/stream/upd_1", result.output)

    def test_update_run_conflict_409(self):
        api = self._api(lambda request: err(409, "update_in_progress", "已有更新任务正在执行"))
        result = self.invoke(["update", "run"], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn("update_in_progress", result.output)

    def test_update_run_csrf_403_hints_admin_login(self):
        api = self._api(lambda request: err(403, "csrf_failed", "CSRF 校验失败", "req_csrf"))
        result = self.invoke(["update", "run"], api=api)
        self.assertEqual(result.exit_code, 2)  # csrf_failed 属认证类退出码
        self.assertIn("csrf_failed", result.output)
        self.assertIn("admin 账号交互登录", result.output)

    def test_update_run_stream_404(self):
        def handler(request):
            if request.url.path == "/api/v1/admin/system/update":
                return ok({"session_id": "upd_gone", "current": "fc9e4f7"})
            return err(404, "session_not_found", "session 不存在")

        api = self._api(handler)
        result = self.invoke(["update", "run"], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn("stream_failed", result.output)


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


class TestCliP0(BaseCliTest):
    def _api(self, handler):
        return make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")

    def test_logs_create_content(self):
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"id": "log_9", "category": "rf", "content_status": "draft"})

        api = self._api(handler)
        result = self.invoke(["logs", "create", "prj_1", "--content", "RF 完成",
                              "--category", "rf"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("log_9", result.output)
        self.assertIn('"category":"rf"', captured["body"])

    def test_logs_create_from_file(self):
        import tempfile
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False, encoding="utf-8") as fh:
            fh.write("从文件来的长正文")
            path = fh.name
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"id": "log_9"})

        api = self._api(handler)
        result = self.invoke(["logs", "create", "prj_1", "--file", path,
                              "--daily-report-id", "rep_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("从文件来的长正文", captured["body"])
        self.assertIn("rep_1", captured["body"])

    def test_logs_create_requires_exactly_one_source(self):
        api = self._api(lambda request: ok({}))
        result = self.invoke(["logs", "create", "prj_1"], api=api)
        self.assertEqual(result.exit_code, 2)  # click UsageError
        result = self.invoke(["logs", "create", "prj_1", "--content", "x",
                              "--file", "/etc/hosts"], api=api)
        self.assertEqual(result.exit_code, 2)

    def test_attachments_upload(self):
        import tempfile
        with tempfile.NamedTemporaryFile(suffix=".txt", delete=False) as fh:
            fh.write(b"hello attachment")
            path = fh.name
        captured = {}

        def handler(request):
            captured["content_type"] = request.headers.get("content-type", "")
            captured["body"] = request.read()
            return ok({"attachment": {"id": "att_7", "original_name": "note.txt",
                                      "file_size": 17, "mime_type": "text/plain"},
                       "links": [{"id": "lnk_1", "entity_type": "log", "entity_id": "log_1"}]})

        api = self._api(handler)
        result = self.invoke(["attachments", "upload", path, "--entity-type", "log",
                              "--entity-id", "log_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("att_7", result.output)
        self.assertIn("multipart/form-data", captured["content_type"])
        self.assertIn(b"hello attachment", captured["body"])

    def test_attachments_upload_duplicate_key_hint(self):
        import tempfile
        with tempfile.NamedTemporaryFile(delete=False) as fh:
            fh.write(b"x")
            path = fh.name
        api = self._api(lambda request: err(409, "duplicate_idempotency_key", "重复请求"))
        result = self.invoke(["attachments", "upload", path], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn("duplicate_idempotency_key", result.output)
        self.assertIn("不要盲目重试", result.output)

    def test_attachments_list_and_download(self):
        import hashlib
        payload = b"file-content"
        sha = hashlib.sha256(payload).hexdigest()

        def handler(request):
            if request.url.path == "/api/v1/attachments":
                return ok({"items": [{"id": "att_1", "original_name": "a.txt"}], "total": 1})
            if request.url.path == "/api/v1/attachments/att_1":
                return ok({"id": "att_1", "original_name": "a.txt", "sha256": sha})
            return httpx.Response(200, content=payload)

        api = self._api(handler)
        result = self.invoke(["attachments", "list"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("att_1", result.output)

        import tempfile
        with tempfile.TemporaryDirectory() as tmp:
            out = tmp + "/dl.bin"
            result = self.invoke(["attachments", "download", "att_1", "--output", out], api=api)
            self.assertEqual(result.exit_code, 0, result.output)
            self.assertIn(sha, result.output)
            self.assertIn('"sha256_match": true', result.output)
            with open(out, "rb") as fh:
                self.assertEqual(fh.read(), payload)


class TestCliP1(BaseCliTest):
    def _api(self, handler):
        return make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")

    def test_daily_report_submit_blocked_exit_1(self):
        api = self._api(lambda request: ok({
            "report": {"id": "rep_1", "status": "draft"},
            "warnings": [{"code": "no_logs", "message": "日报没有任何日志"}],
            "blocked": True}))
        result = self.invoke(["daily-report", "submit", "rep_1"], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn("blocked", result.output)
        self.assertIn("--force", result.output)

    def test_daily_report_submit_force_success(self):
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"report": {"id": "rep_1", "status": "submitted"},
                       "warnings": [], "blocked": False})

        api = self._api(handler)
        result = self.invoke(["daily-report", "submit", "rep_1", "--force"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn('"force":true', captured["body"])
        self.assertIn("submitted", result.output)

    def test_daily_report_ai_parse(self):
        api = self._api(lambda request: ok({"status": "ok", "candidates": [
            {"category": "cryo", "content": "降温到 4.2K", "confidence": 0.9}]}))
        result = self.invoke(["daily-report", "ai-parse", "rep_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("clarify_candidates" not in result.output and "ok", result.output)

    def test_logs_update_confirm(self):
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"id": "log_1", "content_status": "confirmed"})

        api = self._api(handler)
        result = self.invoke(["logs", "update", "log_1", "--confirm"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("confirmed", captured["body"])

    def test_issues_update_and_comment(self):
        api = self._api(lambda request: ok({"id": "iss_1", "severity": "critical"}))
        result = self.invoke(["issues", "update", "iss_1", "--severity", "critical"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)

        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            captured["body"] = request.read().decode()
            return ok({"id": "cmt_1", "content": "已核实"})

        api2 = self._api(handler)
        result2 = self.invoke(["issues", "comment", "iss_1", "--content", "已核实"], api=api2)
        self.assertEqual(result2.exit_code, 0, result2.output)
        self.assertEqual(captured["path"], "/api/v1/issues/iss_1/comments")

    def test_projects_members_flow(self):
        captured = {}

        def handler(request):
            captured["method"] = request.method
            captured["path"] = request.url.path
            captured["body"] = request.read().decode()
            return ok({"items": [{"user_id": "usr_2", "role": "viewer"}], "total": 1})

        api = self._api(handler)
        result = self.invoke(["projects", "members", "list", "prj_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["method"], "GET")

        result2 = self.invoke(["projects", "members", "add", "prj_1", "usr_2",
                               "--role", "viewer"], api=api)
        self.assertEqual(result2.exit_code, 0, result2.output)
        self.assertIn('"role":"viewer"', captured["body"])

    def test_projects_transition_ignore_warnings(self):
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"id": "prj_1", "status": "completed"})

        api = self._api(handler)
        result = self.invoke(["projects", "transition", "prj_1", "--action", "complete",
                              "--ignore-warnings", "--reason", "遗留 issue 不阻塞"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn('"ignore_warnings":true', captured["body"])
        self.assertIn("遗留 issue 不阻塞", captured["body"])

    def test_test_data_batch_csv_file(self):
        import tempfile
        csv_text = ("data_type,measurement,value,unit\n"
                    "cryo,target_temp,4.2,K\n"
                    "pressure,cell_pressure,912,\n")
        with tempfile.NamedTemporaryFile("w", suffix=".csv", delete=False,
                                         encoding="utf-8") as fh:
            fh.write(csv_text)
            path = fh.name
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            captured["body"] = request.read().decode()
            return ok({"created": 2})

        api = self._api(handler)
        result = self.invoke(["test-data", "batch", "prj_1", "--file", path], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["path"], "/api/v1/projects/prj_1/test-data/batch")
        body = json.loads(captured["body"])
        self.assertEqual(body[0], {"data_type": "cryo", "measurement": "target_temp",
                                   "value": 4.2, "unit": "K"})

    def test_test_data_batch_422_details_passthrough(self):
        api = self._api(lambda request: httpx.Response(422, json={
            "error": {"code": "batch_validation_failed", "message": "部分行校验失败",
                      "details": {"errors": [{"index": 1, "field": "value",
                                              "code": "required"}]}},
            "request_id": "req_422"}))
        result = self.invoke(["test-data", "batch", "prj_1",
                              "--json", '[{"data_type":"cryo","measurement":"m","value":1},'
                                         '{"data_type":"cryo","measurement":"m2"}]'], api=api)
        self.assertEqual(result.exit_code, 1)
        self.assertIn("batch_validation_failed", result.output)
        self.assertIn("errors", result.output)  # 行级错误在 details 原样透出
        self.assertIn("req_422", result.output)

    def test_todos_add_and_done(self):
        captured = {}

        def handler(request):
            captured["method"] = request.method
            captured["path"] = request.url.path
            return ok({"id": "td_1", "title": "换气瓶", "status": "done"})

        api = self._api(handler)
        result = self.invoke(["todos", "add", "换气瓶", "--priority", "high"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        result2 = self.invoke(["todos", "done", "td_1"], api=api)
        self.assertEqual(result2.exit_code, 0, result2.output)
        self.assertEqual(captured["path"], "/api/v1/todos/td_1/done")

    def test_audit_verify(self):
        api = self._api(lambda request: ok({"valid": True, "checked": 42, "chain_intact": True}))
        result = self.invoke(["audit", "verify", "--from-id", "1", "--to-id", "42"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("chain_intact", result.output)

    def test_attachments_link(self):
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            captured["body"] = request.read().decode()
            return ok({"id": "lnk_1", "attachment_id": "att_1", "entity_type": "log",
                       "entity_id": "log_1"})

        api = self._api(handler)
        result = self.invoke(["attachments", "link", "att_1", "--entity-type", "log",
                              "--entity-id", "log_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["path"], "/api/v1/attachments/att_1/links")
        self.assertIn("log_1", captured["body"])

    def test_experiences_create_linked(self):
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"id": "exp_1", "status": "candidate"})

        api = self._api(handler)
        result = self.invoke(["experiences", "create", "--title", "经验", "--content", "内容",
                              "--project-id", "prj_1", "--tag", "cryo",
                              "--linked-project", "prj_2:primary"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        body = json.loads(captured["body"])
        self.assertEqual(body["tags"], ["cryo"])
        self.assertEqual(body["linked_projects"],
                         [{"project_id": "prj_2", "relation": "primary"}])

    def test_runs_create(self):
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"id": "run_1", "name": "第 3 轮降温", "status": "planned"})

        api = self._api(handler)
        result = self.invoke(["runs", "create", "prj_1", "--name", "第 3 轮降温",
                              "--run-type", "cooldown", "--gas-type", "He",
                              "--target-temp", "4.2", "--device", "rfq",
                              "--device", "rf_carpet"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        body = json.loads(captured["body"])
        self.assertEqual(body["devices"], ["rfq", "rf_carpet"])
        self.assertEqual(body["target_temp"], 4.2)


class TestCliP2(BaseCliTest):
    def _api(self, handler):
        return make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")

    def test_login_still_works_as_group(self):
        stub = StubAPI(login_data=TOKENS)
        with mock.patch.object(cli_module, "LabctlAPI", return_value=stub):
            result = self.runner.invoke(cli, ["login", "zhangsan"], input="secret\n")
        self.assertEqual(result.exit_code, 0, result.output)
        data = auth.load_token()
        self.assertEqual(data["username"], "zhangsan")

    def test_login_whoami_still_works(self):
        api = self._api(lambda request: ok({"user": {"username": "zhangsan"}}))
        result = self.invoke(["login", "--whoami"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)

    def test_login_set_language(self):
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            captured["body"] = request.read().decode()
            return ok({"user": {"username": "zhangsan", "language": "en"}})

        api = self._api(handler)
        result = self.invoke(["login", "set-language", "en"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["path"], "/api/v1/auth/profile")
        self.assertIn('"language":"en"', captured["body"])

    def test_login_password_prompts(self):
        captured = {}

        def handler(request):
            captured["body"] = request.read().decode()
            return ok({"success": True})

        api = self._api(handler)
        patcher = mock.patch.object(cli_module, "build_api", return_value=api)
        with patcher:
            result = self.runner.invoke(cli, ["login", "password"],
                                        input="old_secret\nNewStrong!2026\nNewStrong!2026\n")
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("old_secret", captured["body"])

    def test_sensors_history(self):
        captured = {}

        def handler(request):
            captured["params"] = dict(request.url.params)
            return ok({"points": [{"time": "2026-09-01T10:00:00Z", "tag": "p",
                                   "value": 912.3}]})

        api = self._api(handler)
        result = self.invoke(["sensors", "history", "--tag", "cell_pressure",
                              "--from", "-24h", "--interval", "5m"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["params"], {"tag": "cell_pressure", "from": "-24h",
                                              "interval": "5m"})

    def test_ask_chat(self):
        api = self._api(lambda request: ok({"answer": "上周降温到 4.2K", "question": "…"}))
        result = self.invoke(["ask", "chat", "上周降温到多少K？"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("4.2K", result.output)

    def test_daily_report_by_date(self):
        api = self._api(lambda request: ok({"id": "rep_9", "report_date": "2026-08-30"}))
        result = self.invoke(["daily-report", "by-date", "--date", "2026-08-30",
                              "--latest"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("rep_9", result.output)

    def test_instruments_readonly(self):
        api = self._api(lambda request: ok([{"id": "keysight_33210a", "state": "online"}]))
        result = self.invoke(["instruments", "list"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)

        api2 = self._api(lambda request: ok(
            {"instrument_id": "keysight_33210a", "state": "online", "rate_limited": False}))
        result2 = self.invoke(["instruments", "status", "keysight_33210a"], api=api2)
        self.assertEqual(result2.exit_code, 0, result2.output)

    def test_agent_candidates_flow(self):
        api = self._api(lambda request: ok(
            {"items": [{"id": "cand_1", "action_type": "create_log",
                        "status": "pending_review"}], "total": 1}))
        result = self.invoke(["agent", "candidates", "list"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)

        api2 = self._api(lambda request: ok({"id": "cand_1", "status": "approved"}))
        result2 = self.invoke(["agent", "candidates", "approve", "cand_1"], api=api2)
        self.assertEqual(result2.exit_code, 0, result2.output)

    def test_admin_users_create(self):
        api = self._api(lambda request: ok({"user": {"id": "usr_9", "username": "lisi"},
                                            "temporary_password": "Tmp!12345"}))
        result = self.invoke(["admin", "users", "create", "--username", "lisi",
                              "--role", "member"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("temporary_password", result.output)

    def test_automation_rules(self):
        api = self._api(lambda request: ok({"id": "rule_1", "name": "日报自动解析",
                                            "enabled": True}))
        result = self.invoke(["automation", "rules", "create", "--name", "日报自动解析"],
                             api=api)
        self.assertEqual(result.exit_code, 0, result.output)

    def test_runs_steps_reorder(self):
        captured = {}

        def handler(request):
            captured["path"] = request.url.path
            captured["body"] = request.read().decode()
            return ok({"reordered": 2})

        api = self._api(handler)
        result = self.invoke(["runs", "steps", "reorder", "run_1", "--steps",
                              '[{"id":"st_2","step_order":1}]'], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertEqual(captured["path"], "/api/v1/run-steps/reorder")
        self.assertIn("st_2", captured["body"])

    def test_assembly_list(self):
        api = self._api(lambda request: ok(
            {"items": [{"id": "ast_1", "name": "安装真空规", "status": "planned"}],
             "total": 1}))
        result = self.invoke(["assembly", "list", "prj_1"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)
        self.assertIn("ast_1", result.output)

    def test_rf_matching_create(self):
        api = self._api(lambda request: ok({"id": "rfm_1", "device": "rfq",
                                            "frequency_mhz": 18.3, "status": "pass"}))
        result = self.invoke(["rf-matching", "create", "prj_1", "--device", "rfq",
                              "--frequency-mhz", "18.3", "--status", "pass",
                              "--s11", "-15.2"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)

    def test_test_data_update(self):
        api = self._api(lambda request: ok({"id": "td_1", "value": 4.5,
                                            "quality": "outlier"}))
        result = self.invoke(["test-data", "update", "td_1", "--value", "4.5",
                              "--quality", "outlier"], api=api)
        self.assertEqual(result.exit_code, 0, result.output)


if __name__ == "__main__":
    unittest.main()
