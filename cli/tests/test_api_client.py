import sys
import unittest
from pathlib import Path
from urllib.parse import urlsplit

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from cli.api_client import LabctlAPI, LabctlError  # noqa: E402

from cli.tests.helpers import TOKENS, err, make_api, ok  # noqa: E402


def _login_handler(request):
    if request.method != "POST" or request.url.path != "/api/v1/auth/login":
        return err(404, "not_found", "路径不存在")
    body = request.read().decode()
    if '"wrong"' in body:
        return err(401, "invalid_credentials", "用户名或密码错误", "req_login_fail")
    return ok(TOKENS, "req_login_ok")


class TestLogin(unittest.TestCase):
    def test_login_success_sets_tokens(self):
        api = make_api(_login_handler)
        data = api.login("zhangsan", "secret")
        self.assertEqual(data["access_token"], "at_1")
        self.assertEqual(api.access_token, "at_1")
        self.assertEqual(api.refresh_token, "rt_1")
        self.assertEqual(api.csrf_token, "csrf_1")
        self.assertEqual(api.username, "zhangsan")

    def test_login_failure_raises_with_request_id(self):
        api = make_api(_login_handler)
        with self.assertRaises(LabctlError) as cm:
            api.login("zhangsan", "wrong")
        exc = cm.exception
        self.assertEqual(exc.status, 401)
        self.assertEqual(exc.code, "invalid_credentials")
        self.assertEqual(exc.request_id, "req_login_fail")
        self.assertIn("req_login_fail", str(exc))

    def test_csrf_cookie_restored_after_login(self):
        api = make_api(_login_handler)
        api.login("zhangsan", "secret")
        cookie = api.client.cookies.get("csrf_token")
        self.assertEqual(cookie, "csrf_1")

    def test_write_headers_idempotency_and_csrf(self):
        captured = {}

        def handler(request):
            captured.update(method=request.method, headers=request.headers)
            return ok({"id": "log_1"})

        api = make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")
        api.request("POST", "/api/v1/projects/prj_1/logs", json={"category": "test"})
        self.assertIn("Idempotency-Key", captured["headers"])
        self.assertIn("X-CSRF-Token", captured["headers"])
        self.assertEqual(captured["headers"]["X-CSRF-Token"], "csrf_1")
        self.assertEqual(captured["headers"]["Authorization"], "Bearer at_1")

    def test_get_has_no_idempotency_key(self):
        captured = {}

        def handler(request):
            captured["headers"] = request.headers
            return ok([])

        api = make_api(handler, access_token="at_1", csrf_token="csrf_1")
        api.request("GET", "/api/v1/projects")
        self.assertNotIn("Idempotency-Key", captured["headers"])

    def test_idempotency_key_persisted_across_retry(self):
        seen = []

        def handler(request):
            seen.append(request.headers.get("Idempotency-Key"))
            if len(seen) == 1:
                return httpx.Response(429, json={"error": {"code": "rate_limit"}})
            return ok({"success": True})

        api = make_api(handler, access_token="at_1")
        api.request("POST", "/api/v1/alerts/resolve", json={"id": "x"})
        self.assertEqual(len(seen), 2)
        self.assertEqual(seen[0], seen[1])


class TestRefresh(unittest.TestCase):
    def test_401_triggers_refresh_and_retry(self):
        calls = {"count": 0}

        def handler(request):
            path = request.url.path
            if path == "/api/v1/auth/refresh":
                calls["count"] += 1
                return ok({**TOKENS, "access_token": "at_2", "csrf_token": "csrf_2"},
                          "req_refresh")
            if request.headers.get("Authorization") == "Bearer at_1":
                return err(401, "unauthorized", "token 过期")
            return ok({"id": "prj_1"})

        api = make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")
        data = api.request("GET", "/api/v1/projects/prj_1")
        self.assertEqual(data["id"], "prj_1")
        self.assertEqual(calls["count"], 1)
        self.assertEqual(api.access_token, "at_2")

    def test_refresh_failure_raises_auth_expired(self):
        def handler(request):
            if request.url.path == "/api/v1/auth/refresh":
                return err(401, "unauthorized", "refresh token 无效")
            return err(401, "unauthorized", "token 过期")

        api = make_api(handler, access_token="at_1", refresh_token="rt_1")
        with self.assertRaises(LabctlError) as cm:
            api.request("GET", "/api/v1/daily-reports")
        self.assertEqual(cm.exception.code, "auth_expired")
        self.assertIn("请重新执行 labctl login", str(cm.exception))

    def test_401_without_refresh_token_raises(self):
        api = make_api(lambda request: err(401, "unauthorized", "token 过期"),
                       access_token="at_1", refresh_token="")
        with self.assertRaises(LabctlError) as cm:
            api.request("GET", "/api/v1/daily-reports")
        self.assertEqual(cm.exception.status, 401)

    def test_no_token_raises_not_logged_in(self):
        api = make_api(lambda request: ok([]))
        with self.assertRaises(LabctlError) as cm:
            api.request("GET", "/api/v1/projects")
        self.assertEqual(cm.exception.code, "not_logged_in")

    def test_service_token_env_passthrough(self):
        import os
        os.environ["LABCTL_SERVICE_TOKEN"] = "svc_jwt"
        try:
            captured = {}

            def handler(request):
                captured["auth"] = request.headers.get("Authorization")
                return ok([{"id": "prj_1"}])

            api = LabctlAPI(base_url="http://lab.test",
                            client=httpx.Client(transport=httpx.MockTransport(handler)))
            api.request("GET", "/api/v1/projects")
            self.assertEqual(captured["auth"], "Bearer svc_jwt")
        finally:
            del os.environ["LABCTL_SERVICE_TOKEN"]

    def test_from_stored_explicit_base_url_wins(self):
        api = LabctlAPI.from_stored({"base_url": "http://stored.example:9000",
                                     "access_token": "at_1"},
                                    base_url="http://explicit.example")
        self.assertEqual(api.base_url, "http://explicit.example")

    def test_from_stored_falls_back_to_stored_base_url(self):
        api = LabctlAPI.from_stored({"base_url": "http://stored.example:9000",
                                     "access_token": "at_1"})
        self.assertEqual(api.base_url, "http://stored.example:9000")


class TestRetry(unittest.TestCase):
    def test_429_backoff_then_success(self):
        calls = {"count": 0}

        def handler(request):
            calls["count"] += 1
            if calls["count"] < 3:
                return httpx.Response(429, json={"error": {"code": "rate_limit_exceeded"}})
            return ok({"success": True})

        api = make_api(handler, access_token="at_1")
        data = api.request("GET", "/api/v1/alerts")
        self.assertEqual(calls["count"], 3)
        self.assertTrue(data["success"])

    def test_429_exhausted_raises_hint(self):
        api = make_api(lambda request: httpx.Response(
            429, json={"error": {"code": "rate_limit_exceeded"}}), access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            api.request("GET", "/api/v1/alerts")
        self.assertEqual(cm.exception.code, "retry_limit")
        self.assertIn("429", str(cm.exception))

    def test_502_exhausted_raises_degrade_hint(self):
        api = make_api(lambda request: httpx.Response(
            502, json={"error": {"code": "upstream_error"}}), access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            api.request("GET", "/api/v1/daily-reports")
        self.assertIn("502", str(cm.exception))
        self.assertIn("py-agent-interpret", str(cm.exception))

    def test_500_retry_then_success(self):
        calls = {"count": 0}

        def handler(request):
            calls["count"] += 1
            if calls["count"] == 1:
                return httpx.Response(500, json={"error": {"code": "internal"}})
            return ok({"success": True})

        api = make_api(handler, access_token="at_1")
        api.request("GET", "/api/v1/alerts")
        self.assertEqual(calls["count"], 2)


class TestErrors(unittest.TestCase):
    def test_403_passthrough_with_request_id(self):
        api = make_api(lambda request: err(403, "permission_denied", "当前用户无权访问该项目",
                                           "req_403"), access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            api.request("GET", "/api/v1/projects/prj_1/issues")
        exc = cm.exception
        self.assertEqual(exc.status, 403)
        self.assertEqual(exc.code, "permission_denied")
        self.assertEqual(exc.request_id, "req_403")
        self.assertEqual(exc.to_dict()["request_id"], "req_403")

    def test_404_passthrough(self):
        api = make_api(lambda request: err(404, "not_found", "项目不存在", "req_404"),
                       access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            api.request("GET", "/api/v1/projects/nope")
        self.assertEqual(cm.exception.status, 404)

    def test_409_idempotency_conflict_passthrough(self):
        api = make_api(lambda request: err(409, "duplicate_idempotency_key", "重复请求"),
                       access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            api.request("POST", "/api/v1/projects", json={"code": "x"})
        self.assertEqual(cm.exception.status, 409)

    def test_invalid_json_body(self):
        api = make_api(lambda request: httpx.Response(
            200, content=b"not json"), access_token="at_1")
        with self.assertRaises(LabctlError):
            api.request("GET", "/api/v1/alerts")


class TestMultipartAndTimeout(unittest.TestCase):
    def test_request_passes_data_files_and_timeout(self):
        captured = {}

        def handler(request):
            captured.update(
                method=request.method,
                path=request.url.path,
                content_type=request.headers.get("content-type", ""),
                timeout=request.extensions.get("timeout") if hasattr(request, "extensions") else None,
            )
            captured["body"] = request.read()
            return ok({"id": "att_1"})

        api = make_api(handler, access_token="at_1", csrf_token="csrf_1")
        api.request("POST", "/api/v1/attachments",
                    data={"entity_type": "log", "entity_id": "log_1"},
                    files={"file": ("a.png", b"\x89PNG", "image/png")},
                    timeout=300)
        self.assertEqual(captured["method"], "POST")
        self.assertEqual(captured["path"], "/api/v1/attachments")
        self.assertIn("multipart/form-data", captured["content_type"])
        self.assertIn("boundary=", captured["content_type"])
        self.assertIn(b'name="entity_type"', captured["body"])
        self.assertIn(b"log_1", captured["body"])
        self.assertIn(b'filename="a.png"', captured["body"])
        self.assertIn(b"\x89PNG", captured["body"])

    def test_request_json_with_files_rejected(self):
        api = make_api(lambda request: ok({}), access_token="at_1")
        with self.assertRaises(LabctlError) as cm:
            api.request("POST", "/api/v1/attachments", json={"a": 1},
                        files={"file": ("a", b"x")})
        self.assertEqual(cm.exception.code, "bad_request")

    def test_multipart_write_headers_kept(self):
        captured = {}

        def handler(request):
            captured["headers"] = request.headers
            request.read()
            return ok({"id": "att_1"})

        api = make_api(handler, access_token="at_1", refresh_token="rt_1", csrf_token="csrf_1")
        api.request("POST", "/api/v1/attachments", files={"file": ("a.txt", b"hi")})
        self.assertIn("Idempotency-Key", captured["headers"])  # POST 自动幂等键
        self.assertEqual(captured["headers"]["X-CSRF-Token"], "csrf_1")

    def test_multipart_retry_replays_body(self):
        bodies = []

        def handler(request):
            bodies.append(request.read())
            if len(bodies) == 1:
                return httpx.Response(500, json={"error": {"code": "internal"}})
            return ok({"id": "att_1"})

        api = make_api(handler, access_token="at_1")
        api.request("POST", "/api/v1/attachments", files={"file": ("a.txt", b"payload")})
        self.assertEqual(len(bodies), 2)
        # 内存 bytes 重放：boundary 每次随机，但两次请求体都完整携带文件内容
        # （若传文件句柄，第二次读为空、请求体不再含 payload）
        for body in bodies:
            self.assertIn(b"payload", body)
            self.assertIn(b'filename="a.txt"', body)


class TestDownload(unittest.TestCase):
    def test_download_writes_file_with_sha256(self):
        import hashlib
        captured = {}
        payload = b"binary-attachment-content"

        def handler(request):
            captured.update(method=request.method, path=request.url.path,
                            idempotency=request.headers.get("Idempotency-Key", ""),
                            auth=request.headers.get("Authorization", ""))
            return httpx.Response(200, content=payload,
                                  headers={"content-type": "application/octet-stream"})

        import tempfile
        with tempfile.NamedTemporaryFile(delete=False) as fh:
            dest = fh.name
        api = make_api(handler, access_token="at_1")
        result = api.download("/api/v1/attachments/att_1/content", dest)
        self.assertEqual(captured["method"], "GET")
        self.assertEqual(captured["path"], "/api/v1/attachments/att_1/content")
        self.assertTrue(captured["idempotency"])  # handler 级校验：GET 也须带幂等键
        self.assertEqual(captured["auth"], "Bearer at_1")
        self.assertEqual(result["path"], dest)
        self.assertEqual(result["size"], len(payload))
        self.assertEqual(result["sha256"], hashlib.sha256(payload).hexdigest())
        with open(dest, "rb") as fh:
            self.assertEqual(fh.read(), payload)

    def test_download_error_envelope_passthrough(self):
        def handler(request):
            return err(404, "attachment_not_found", "附件不存在", "req_dl_404")

        api = make_api(handler, access_token="at_1")
        import tempfile
        with tempfile.NamedTemporaryFile(delete=False) as fh:
            dest = fh.name
        with self.assertRaises(LabctlError) as cm:
            api.download("/api/v1/attachments/nope/content", dest)
        exc = cm.exception
        self.assertEqual(exc.status, 404)
        self.assertEqual(exc.code, "attachment_not_found")
        self.assertEqual(exc.request_id, "req_dl_404")

    def test_download_non_json_error(self):
        api = make_api(lambda request: httpx.Response(502, content=b"bad gateway"),
                       access_token="at_1")
        import tempfile
        with tempfile.NamedTemporaryFile(delete=False) as fh:
            dest = fh.name
        with self.assertRaises(LabctlError) as cm:
            api.download("/api/v1/attachments/att_1/content", dest)
        self.assertEqual(cm.exception.status, 502)

    def test_download_requires_login(self):
        api = make_api(lambda request: ok({}), access_token="")
        with self.assertRaises(LabctlError) as cm:
            api.download("/api/v1/attachments/att_1/content", "/tmp/x")
        self.assertEqual(cm.exception.code, "not_logged_in")

    def test_download_service_token_env(self):
        import os
        os.environ["LABCTL_SERVICE_TOKEN"] = "svc_jwt"
        try:
            captured = {}

            def handler(request):
                captured["auth"] = request.headers.get("Authorization")
                return httpx.Response(200, content=b"x")

            api = make_api(handler, access_token="")
            import tempfile
            with tempfile.NamedTemporaryFile(delete=False) as fh:
                dest = fh.name
            api.download("/api/v1/attachments/att_1/content", dest)
            self.assertEqual(captured["auth"], "Bearer svc_jwt")
        finally:
            del os.environ["LABCTL_SERVICE_TOKEN"]


if __name__ == "__main__":
    unittest.main()
