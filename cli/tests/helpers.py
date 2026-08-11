import logging

import httpx

logging.getLogger("httpx").setLevel(logging.WARNING)

from cli.api_client import LabctlAPI  # noqa: E402


def make_api(handler, **kwargs):
    client = httpx.Client(transport=httpx.MockTransport(handler), timeout=10)
    return LabctlAPI(base_url="http://lab.test", client=client, **kwargs)


def ok(data, request_id="req_test_ok"):
    return httpx.Response(200, json={"data": data, "request_id": request_id})


def created(data, request_id="req_test_created"):
    return httpx.Response(201, json={"data": data, "request_id": request_id})


def err(status, code, message, request_id="req_test_err"):
    return httpx.Response(status, json={
        "error": {"code": code, "message": message},
        "request_id": request_id,
    })


TOKENS = {
    "access_token": "at_1",
    "refresh_token": "rt_1",
    "csrf_token": "csrf_1",
    "user": {"id": "usr_1", "name": "张三", "roles": ["member"], "language": "zh"},
}
