import hashlib
import json
import os
import time
import uuid
from urllib.parse import urlsplit

import httpx

DEFAULT_BASE_URL = os.getenv("LABCTL_BASE_URL", "http://127.0.0.1:8000")

RETRY_COUNT = 3


class LabctlError(RuntimeError):
    """CLI 统一错误：携带服务端错误码、HTTP 状态与 request_id（透传服务端）。"""

    def __init__(self, message, code="api_error", status=None, request_id="", details=None):
        super().__init__(message)
        self.code = code
        self.status = status
        self.request_id = request_id or ""
        self.details = details

    def __str__(self):
        text = super().__str__()
        if self.request_id:
            return f"{text} (request_id: {self.request_id})"
        return text

    def to_dict(self):
        body = {"code": self.code, "message": str(self)}
        if self.request_id:
            body["request_id"] = self.request_id
        if self.details is not None:
            body["details"] = self.details
        return body


def _stream_error(response):
    """流式响应的错误信封解析：透传 code/message/request_id，非 JSON 给通用错误。"""
    body = response.read()
    try:
        payload = json.loads(body.decode("utf-8"))
    except ValueError:
        payload = None
    err = payload.get("error", {}) if isinstance(payload, dict) else {}
    return LabctlError(
        err.get("message", f"下载失败（HTTP {response.status_code}）"),
        code=err.get("code", "api_error"),
        status=response.status_code,
        request_id=payload.get("request_id", "") if isinstance(payload, dict) else "",
    )


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

    def request(self, method, path, params=None, json=None, headers=None,
                data=None, files=None, timeout=None):
        """发起 API 请求；data/files 用于 multipart/form-data（与 json 互斥），
        timeout 为 per-call 覆盖（None 走 client 默认 20s，上传建议 120-300s）。"""
        return self._request(method, path, params=params, json=json, headers=headers,
                             data=data, files=files, timeout=timeout)

    def download(self, path, dest, params=None, timeout=None):
        """流式下载二进制响应写盘（附件下载专用），返回 {path, size, sha256}。

        attachments download handler 对 GET 也手动要求 Idempotency-Key 头
        （handler 级校验，不走 request 的写方法自动逻辑），这里统一补上。
        流式下载不做 429/5xx 自动重试（大文件重发浪费带宽），失败原样报错。
        """
        headers = {}
        token = os.getenv("LABCTL_SERVICE_TOKEN", "")
        if not token and not self.access_token:
            raise LabctlError("未登录：请先执行 labctl login 或设置 LABCTL_SERVICE_TOKEN",
                              code="not_logged_in")
        headers["Authorization"] = f"Bearer {token or self.access_token}"
        headers["Idempotency-Key"] = str(uuid.uuid4())
        digest = hashlib.sha256()
        size = 0
        try:
            with self.client.stream("GET", self.base_url + path, params=params,
                                    headers=headers, timeout=timeout) as response:
                if response.is_error:
                    raise _stream_error(response)
                with open(dest, "wb") as fh:
                    for chunk in response.iter_bytes():
                        fh.write(chunk)
                        digest.update(chunk)
                        size += len(chunk)
        except httpx.TimeoutException as exc:
            raise LabctlError("下载超时：请重试或调大 --timeout", code="download_timeout") from exc
        except httpx.TransportError as exc:
            raise LabctlError(f"下载连接中断（{exc.__class__.__name__}）",
                              code="download_disconnected") from exc
        return {"path": dest, "size": size, "sha256": digest.hexdigest()}

    def _request(self, method, path, params=None, json=None, headers=None,
                 data=None, files=None, timeout=None,
                 authenticate=True, refresh_on_401=True):
        if (data is not None or files is not None) and json is not None:
            raise LabctlError("json 与 data/files 互斥，multipart 请求不要传 json",
                              code="bad_request")
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
                # files 必须是内存 bytes（不可是文件句柄）：429/5xx 重试重发时
                # 句柄第二次读为空；上传命令侧先整体读入再构造。
                request_kwargs = {"params": params, "json": json, "headers": headers}
                if data is not None:
                    request_kwargs["data"] = data
                if files is not None:
                    request_kwargs["files"] = files
                if timeout is not None:
                    request_kwargs["timeout"] = timeout
                response = self.client.request(
                    method, self.base_url + path, **request_kwargs)
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
                details=err.get("details"),  # 如 test-data batch 422 的行级 errors[]
            )
        return payload.get("data")
