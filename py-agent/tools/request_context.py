"""请求级 request_id 上下文（AR-07）。

Go server 通过 X-Request-Id 透传；py-agent 在中间件里写入 contextvar，
tools/api.py 回调 Go 时继续带上，使一次问答可在两边日志中用同一 ID 关联。
"""

from contextvars import ContextVar

REQUEST_ID: ContextVar[str] = ContextVar("request_id", default="")
