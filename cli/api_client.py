import os
import time
import uuid
from urllib.parse import urlsplit

import httpx

DEFAULT_BASE_URL = os.getenv("LABCTL_BASE_URL", "http://127.0.0.1:8000")

RETRY_COUNT = 3


class LabctlError(RuntimeError):
    """CLI 统一错误：携带服务端错误码、HTTP 状态与 request_id（透传服务端）。"""

    def __init__(self, message, code="api_error", status=None, request_id=""):
        super().__init__(message)
        self.code = code
        self.status = status
        self.request_id = request_id or ""

    def __str__(self):
        text = super().__str__()
        if self.request_id:
            return f"{text} (request_id: {self.request_id})"
        return text

    def to_dict(self):
        body = {"code": self.code, "message": str(self)}
        if self.request_id:
            body["request_id"] = self.request_id
        return body


class LabctlAPI:
    """REST 客户端：Bearer 认证 + refresh 自动续期 + CSRF/幂等 + 退避重试。

    结构参照 py-agent/tools/api.py 的 GoAPI：
    - 写操作自动带 Idempotency-Key（uuid4）与 X-CSRF-Token；
    - 401 且持有 refresh_token 时自动 refresh 一次后续期重试；
    - 429/5xx 指数退避（2^attempt），重试耗尽后带场景提示抛错；
    - 服务端错误体透传 code/message/request_id。
    """

    def __init__(self, base_url=DEFAULT_BASE_URL, access_token="", refresh_token="",
                 csrf_token="", username="", timeout=20.0, client=None):
        self.base_url = (base_url or DEFAULT_BASE_URL).rstrip("/")
        self.username = username
        self.client = client or httpx.Client(timeout=timeout)
        self.access_token = access_token
        self.refresh_token = refresh_token
        self.csrf_token = csrf_token
        self._restore_csrf_cookie()

    @classmethod
    def from_env(cls):
        """LABCTL_SERVICE_TOKEN 无人值守通道：纯透传，不做 login/refresh。"""
        token = os.getenv("LABCTL_SERVICE_TOKEN", "")
        if not token:
            raise LabctlError("LABCTL_SERVICE_TOKEN 未设置", code="not_logged_in")
        return cls(DEFAULT_BASE_URL, access_token=token)

    @classmethod
    def from_stored(cls, data, base_url=None):
        # 显式 --base-url / LABCTL_BASE_URL 优先；未显式指定时才回退存储值。
        return cls(
            base_url=base_url or data.get("base_url") or DEFAULT_BASE_URL,
            access_token=data.get("access_token", ""),
            refresh_token=data.get("refresh_token", ""),
            csrf_token=data.get("csrf_token", ""),
            username=data.get("username", ""),
        )

    def to_token_payload(self):
        return {
            "base_url": self.base_url,
            "username": self.username,
            "access_token": self.access_token,
            "refresh_token": self.refresh_token,
            "csrf_token": self.csrf_token,
        }

    def _restore_csrf_cookie(self):
        if not self.csrf_token:
            return
        host = urlsplit(self.base_url).hostname
        if host:
            self.client.cookies.set("csrf_token", self.csrf_token, domain=host, path="/")

    def _apply_tokens(self, data):
        self.access_token = data.get("access_token", "")
        self.refresh_token = data.get("refresh_token", "")
        self.csrf_token = data.get("csrf_token", "")
        self._restore_csrf_cookie()

    def login(self, username, password):
        response = self.client.post(
            self.base_url + "/api/v1/auth/login",
            json={"username": username, "password": password},
        )
        data = self._parse_response(response)
        self.username = username
        self._apply_tokens(data)
        return data

    def refresh(self):
        data = self._request(
            "POST", "/api/v1/auth/refresh",
            json={"refresh_token": self.refresh_token},
            authenticate=False, refresh_on_401=False,
        )
        self._apply_tokens(data)
        return data

    def request(self, method, path, params=None, json=None, headers=None):
        return self._request(method, path, params=params, json=json, headers=headers)

    def _request(self, method, path, params=None, json=None, headers=None,
                 authenticate=True, refresh_on_401=True):
        headers = dict(headers or {})
        token = os.getenv("LABCTL_SERVICE_TOKEN", "")
        if authenticate:
            if not token and not self.access_token:
                raise LabctlError("未登录：请先执行 labctl login 或设置 LABCTL_SERVICE_TOKEN", code="not_logged_in")
            headers["Authorization"] = f"Bearer {token or self.access_token}"
        is_write = method.upper() not in ("GET", "HEAD", "OPTIONS")
        if is_write:
            headers.setdefault("Idempotency-Key", str(uuid.uuid4()))
            if self.csrf_token:
                headers["X-CSRF-Token"] = self.csrf_token
        refreshed = False
        for attempt in range(RETRY_COUNT):
            try:
                response = self.client.request(
                    method, self.base_url + path, params=params, json=json, headers=headers)
            except (httpx.TimeoutException, httpx.TransportError) as exc:
                if attempt == RETRY_COUNT - 1:
                    raise LabctlError(
                        "无法连接服务器，请检查 LABCTL_BASE_URL 与网络", code="connection_error") from exc
                time.sleep(2 ** attempt)
                continue
            if (response.status_code == 401 and authenticate and refresh_on_401
                    and self.refresh_token and not refreshed):
                refreshed = True
                try:
                    self.refresh()
                except LabctlError:
                    raise LabctlError("登录已过期，请重新执行 labctl login", code="auth_expired")
                headers["Authorization"] = f"Bearer {self.access_token}"
                if is_write and self.csrf_token:
                    headers["X-CSRF-Token"] = self.csrf_token
                continue
            if response.status_code == 429 or response.status_code >= 500:
                if attempt == RETRY_COUNT - 1:
                    raise LabctlError(self._retry_hint(response.status_code),
                                      code="retry_limit", status=response.status_code)
                time.sleep(2 ** attempt)
                continue
            return self._parse_response(response)
        raise LabctlError("重试次数已达上限", code="retry_limit")

    @staticmethod
    def _retry_hint(status):
        if status == 429:
            return "请求过于频繁（429 限流），请稍后再试"
        if status == 502:
            return "上游服务未就绪（502，AI 辅助功能可能降级），请稍后再试或检查 py-agent-interpret 状态"
        if status == 503:
            return "服务暂不可用（503），请稍后再试"
        return f"服务端错误（HTTP {status}），请稍后再试"

    @staticmethod
    def _parse_response(response):
        try:
            payload = response.json()
        except ValueError:
            raise LabctlError(
                f"服务端返回非 JSON 响应（HTTP {response.status_code}）", status=response.status_code)
        request_id = payload.get("request_id", "") if isinstance(payload, dict) else ""
        if response.is_error:
            err = payload.get("error", {}) if isinstance(payload, dict) else {}
            raise LabctlError(
                err.get("message", "request failed"),
                code=err.get("code", "api_error"),
                status=response.status_code,
                request_id=request_id,
            )
        return payload.get("data")
