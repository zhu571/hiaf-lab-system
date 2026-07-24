import json
import os
import secrets
from pathlib import Path

import uvicorn
from starlette.applications import Starlette
from starlette.requests import Request
from starlette.responses import JSONResponse
from starlette.routing import Route

from tools.parse import InstrumentInterpreter, ParseError


def read_token():
    path = os.getenv("PY_AGENT_INTERNAL_TOKEN_FILE")
    if path:
        try:
            return Path(path).read_text().strip()
        except FileNotFoundError:
            import sys
            print(f"WARNING: PY_AGENT_INTERNAL_TOKEN_FILE not found: {path}", file=sys.stderr)
    return os.getenv("PY_AGENT_INTERNAL_TOKEN", "")


def validate_request(data):
    if not isinstance(data, dict) or len(json.dumps(data, ensure_ascii=False)) > 64_000:
        raise ValueError("request too large")
    user_input = data.get("user_input")
    history = data.get("history", [])
    commands = data.get("whitelist_commands")
    if not isinstance(user_input, str) or not user_input.strip() or len(user_input) > 1000:
        raise ValueError("user_input is invalid")
    if not isinstance(history, list) or len(history) > 10:
        raise ValueError("history is invalid")
    for item in history:
        if not isinstance(item, dict) or item.get("role") not in {"user", "assistant"} or not isinstance(item.get("content"), str) or len(item["content"]) > 1000:
            raise ValueError("history item is invalid")
    if not isinstance(commands, list) or not commands or len(commands) > 100:
        raise ValueError("whitelist_commands is invalid")
    for command in commands:
        if not isinstance(command, dict) or not isinstance(command.get("name"), str):
            raise ValueError("whitelist command is invalid")
    return user_input.strip(), history, commands


def create_app(interpreter, token):
    async def health(_request):
        return JSONResponse({"status": "ok"})

    async def interpret(request: Request):
        supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
        if not token or not secrets.compare_digest(supplied, token):
            return JSONResponse({"error": "unauthorized"}, status_code=401)
        try:
            data = await request.json()
            user_input, history, commands = validate_request(data)
            result = interpreter.interpret(
                str(data.get("instrument_id", ""))[:128], str(data.get("instrument_name", ""))[:256],
                commands, user_input, history,
            )
            return JSONResponse(result)
        except (ValueError, json.JSONDecodeError):
            return JSONResponse({"error": "bad_request"}, status_code=400)
        except ParseError:
            return JSONResponse({"error": "interpretation_failed"}, status_code=422)
        except Exception:
            return JSONResponse({"error": "provider_unavailable"}, status_code=502)

    return Starlette(routes=[Route("/health", health), Route("/v1/interpret", interpret, methods=["POST"])])


if __name__ == "__main__":
    api_key = os.getenv("DEEPSEEK_API_KEY")
    if not api_key:
        raise RuntimeError("DEEPSEEK_API_KEY environment variable is not set")
    app = create_app(InstrumentInterpreter(api_key), read_token())
    uvicorn.run(app, host="0.0.0.0", port=8001)
