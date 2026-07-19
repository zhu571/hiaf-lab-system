import os
import time
import uuid
from pathlib import Path

import httpx


class APIError(RuntimeError):
    pass


class GoAPI:
    def __init__(self, base_url, username, password, timeout=20.0, client=None):
        self.base_url = base_url.rstrip("/")
        self.username = username
        self.password = password
        self.client = client or httpx.Client(timeout=timeout)
        self.access_token = ""
        self.refresh_token = ""
        self.csrf_token = ""

    @classmethod
    def from_env(cls):
        password = os.getenv("AGENT_PASSWORD", "")
        password_file = os.getenv("AGENT_PASSWORD_FILE", "")
        if not password and password_file:
            password = Path(password_file).read_text().strip()
        if not password:
            raise APIError("agent password is not configured")
        return cls(
            os.environ["GO_API_BASE"],
            os.getenv("AGENT_USERNAME", "agent@system"),
            password,
            float(os.getenv("REQUEST_TIMEOUT_SECONDS", "20")),
        )

    def login(self):
        resp = self.client.post(
            self.base_url + "/api/v1/auth/login",
            json={"username": self.username, "password": self.password},
        )
        resp.raise_for_status()
        data = resp.json()["data"]
        self.access_token = data["access_token"]
        self.refresh_token = data["refresh_token"]
        self.csrf_token = data.get("csrf_token", "")

    def refresh(self):
        data = self._request(
            "POST", "/api/v1/auth/refresh",
            json={"refresh_token": self.refresh_token},
            authenticate=False,
            headers={"Idempotency-Key": str(uuid.uuid4())},
        )
        self.access_token = data["access_token"]
        self.refresh_token = data["refresh_token"]

    def claim(self, lease_seconds=300):
        return self._request(
            "POST", "/api/v1/agent/tasks/claim",
            json={"lease_seconds": lease_seconds},
            headers={"Idempotency-Key": str(uuid.uuid4())},
        )

    def get_report(self, report_id, acting_user_id, task_id):
        return self._request(
            "GET", f"/api/v1/daily-reports/{report_id}",
            headers=self._agent_headers(acting_user_id, task_id),
        )

    def list_issues(self, project_id, status, keyword, acting_user_id, task_id):
        data = self._request(
            "GET", f"/api/v1/projects/{project_id}/issues",
            params={"status": status, "search": keyword, "per_page": 10},
            headers=self._agent_headers(acting_user_id, task_id),
        )
        return (data or {}).get("items") or []

    def complete(self, task_id, candidates, confidence=None):
        body = {
            "result": {"candidate_count": len(candidates)},
            "model": "deepseek-v4-pro",
            "prompt_version": "1.0",
            "candidates": candidates,
        }
        if confidence is not None:
            body["agent_confidence"] = confidence
        return self._request(
            "POST", f"/api/v1/agent/tasks/{task_id}/complete", json=body,
            headers={"Idempotency-Key": f"agent-complete-{task_id}"},
        )

    def fail(self, task_id, error):
        return self._request(
            "POST", f"/api/v1/agent/tasks/{task_id}/fail",
            json={"error": sanitize_error(error)},
            headers={"Idempotency-Key": f"agent-fail-{task_id}"},
        )

    @staticmethod
    def _agent_headers(acting_user_id, task_id):
        return {"X-Acting-User-ID": acting_user_id, "X-Agent-Task-ID": task_id}

    def _request(self, method, path, authenticate=True, headers=None, **kwargs):
        headers = dict(headers or {})
        if self.csrf_token and method.upper() not in ("GET", "HEAD", "OPTIONS"):
            headers["X-CSRF-Token"] = self.csrf_token
        refreshed = False
        for attempt in range(3):
            if authenticate:
                if not self.access_token:
                    self.login()
                headers["Authorization"] = f"Bearer {self.access_token}"
            try:
                response = self.client.request(method, self.base_url + path, headers=headers, **kwargs)
            except (httpx.TimeoutException, httpx.TransportError) as exc:
                if attempt == 2:
                    raise APIError("Go API unavailable") from exc
                time.sleep(2 ** attempt)
                continue
            if response.status_code == 401 and authenticate and self.refresh_token and not refreshed:
                self.refresh()
                refreshed = True
                headers["Authorization"] = f"Bearer {self.access_token}"
                continue
            if response.status_code == 429 or response.status_code >= 500:
                if attempt == 2:
                    raise APIError(f"Go API retry limit reached ({response.status_code})")
                time.sleep(2 ** attempt)
                continue
            try:
                payload = response.json()
            except ValueError as exc:
                raise APIError(f"Go API returned invalid JSON ({response.status_code})") from exc
            if response.is_error:
                error = payload.get("error", {})
                raise APIError(f"{error.get('code', 'api_error')}: {error.get('message', 'request failed')}")
            return payload.get("data")
        raise APIError("Go API retry limit reached")


def sanitize_error(error):
    if isinstance(error, BaseException):
        return f"agent task failed ({type(error).__name__})"
    text = " ".join(str(error).split())
    lowered = text.lower()
    if any(marker in lowered for marker in ("bearer ", "api_key", "api key", "token", "password")):
        return "agent task failed (sensitive detail redacted)"
    return text[:512] or "agent task failed"
