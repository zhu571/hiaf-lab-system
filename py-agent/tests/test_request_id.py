import sys
import unittest
from pathlib import Path

from starlette.applications import Starlette
from starlette.responses import JSONResponse
from starlette.routing import Route
from starlette.testclient import TestClient

sys.path.insert(0, str(Path(__file__).parents[1]))

from serve import RequestIDMiddleware
from tools.api import GoAPI
from tools.request_context import REQUEST_ID


async def echo_request_id(_request):
    return JSONResponse({"request_id": REQUEST_ID.get()})


class RequestIDMiddlewareTests(unittest.TestCase):
    def _client(self):
        app = Starlette(routes=[Route("/", echo_request_id)])
        app.add_middleware(RequestIDMiddleware)
        return TestClient(app)

    def test_reuses_incoming_header(self):
        response = self._client().get("/", headers={"X-Request-Id": "req-abc"})
        self.assertEqual(response.headers["X-Request-Id"], "req-abc")
        self.assertEqual(response.json()["request_id"], "req-abc")

    def test_generates_when_missing(self):
        response = self._client().get("/")
        self.assertTrue(response.headers["X-Request-Id"].startswith("py-"))
        self.assertEqual(response.json()["request_id"], response.headers["X-Request-Id"])


class _FakeResponse:
    status_code = 200
    is_error = False

    def json(self):
        return {"data": {"ok": True}}


class _FakeClient:
    def __init__(self):
        self.calls = []

    def request(self, method, url, headers=None, **kwargs):
        self.calls.append({"method": method, "url": url, "headers": dict(headers or {})})
        return _FakeResponse()


class GoAPIRequestIDTests(unittest.TestCase):
    def test_outbound_call_carries_request_id(self):
        fake = _FakeClient()
        api = GoAPI("http://go", "user", "pass", client=fake)
        api.access_token = "token"
        REQUEST_ID.set("req-outbound")
        api._request("GET", "/api/v1/ping")
        self.assertEqual(fake.calls[0]["headers"]["X-Request-Id"], "req-outbound")

    def test_no_header_when_context_empty(self):
        fake = _FakeClient()
        api = GoAPI("http://go", "user", "pass", client=fake)
        api.access_token = "token"
        REQUEST_ID.set("")
        api._request("GET", "/api/v1/ping")
        self.assertNotIn("X-Request-Id", fake.calls[0]["headers"])


if __name__ == "__main__":
    unittest.main()
