import json
import os
import re
import secrets
from pathlib import Path

import uvicorn
from starlette.applications import Starlette
from starlette.requests import Request
from starlette.responses import JSONResponse
from starlette.routing import Route

from tools.parse import InstrumentInterpreter, ParseError, Parser
from tools.stepplan import StepPlanner
from tools.todoplan import TodoPlanner


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


REPORT_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def validate_daily_parse(data):
    if not isinstance(data, dict) or len(json.dumps(data, ensure_ascii=False)) > 64_000:
        raise ValueError("request too large")
    raw_text = data.get("raw_text")
    projects = data.get("projects")
    report_date = data.get("report_date")
    if not isinstance(raw_text, str) or not 1 <= len(raw_text.strip()) <= 4000:
        raise ValueError("raw_text is invalid")
    if not isinstance(projects, list) or not 1 <= len(projects) <= 50:
        raise ValueError("projects is invalid")
    for project in projects:
        if not isinstance(project, dict) or not isinstance(project.get("id"), str) or not isinstance(project.get("name"), str):
            raise ValueError("project is invalid")
    if not isinstance(report_date, str) or not REPORT_DATE.match(report_date):
        raise ValueError("report_date is invalid")
    return raw_text.strip(), projects, report_date


def validate_todo_add(data):
    if not isinstance(data, dict) or len(json.dumps(data, ensure_ascii=False)) > 64_000:
        raise ValueError("request too large")
    raw_text = data.get("raw_text")
    user_id = data.get("user_id")
    if not isinstance(raw_text, str) or not raw_text.strip() or len(raw_text.strip()) > 2000:
        raise ValueError("raw_text is invalid")
    if not isinstance(user_id, str) or not user_id.strip() or len(user_id.strip()) > 128:
        raise ValueError("user_id is invalid")
    return raw_text.strip(), user_id.strip()


def validate_todo_daily(data):
    if not isinstance(data, dict) or len(json.dumps(data, ensure_ascii=False)) > 64_000:
        raise ValueError("request too large")
    user_id = data.get("user_id")
    yesterday_report = data.get("yesterday_report", "")
    open_issues = data.get("open_issues", [])
    existing_titles = data.get("existing_titles", [])
    if not isinstance(user_id, str) or not user_id.strip() or len(user_id.strip()) > 128:
        raise ValueError("user_id is invalid")
    if not isinstance(yesterday_report, str) or len(yesterday_report) > 20_000:
        raise ValueError("yesterday_report is invalid")
    if not isinstance(open_issues, list) or len(open_issues) > 50:
        raise ValueError("open_issues is invalid")
    for issue in open_issues:
        if not isinstance(issue, dict):
            raise ValueError("open_issue is invalid")
        if not isinstance(issue.get("id"), str) or not issue.get("id") or \
           not isinstance(issue.get("title"), str) or not issue.get("title"):
            raise ValueError("open_issue fields are invalid")
    if not isinstance(existing_titles, list) or len(existing_titles) > 50:
        raise ValueError("existing_titles is invalid")
    for title in existing_titles:
        if not isinstance(title, str) or len(title) > 300:
            raise ValueError("existing_title is invalid")
    return user_id.strip(), yesterday_report, open_issues, existing_titles


def create_app(interpreter, planner, parser, todo_planner, token):
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

    async def step_plan(request: Request):
        supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
        if not token or not secrets.compare_digest(supplied, token):
            return JSONResponse({"error": "unauthorized"}, status_code=401)
        try:
            data = await request.json()
            if not isinstance(data, dict) or len(json.dumps(data, ensure_ascii=False)) > 64_000:
                raise ValueError("request too large")
            kind = data.get("kind")
            prompt = data.get("prompt")
            context = data.get("context") or {}
            if kind not in {"assembly", "experiment"}:
                raise ValueError("kind is invalid")
            if not isinstance(prompt, str) or not prompt.strip() or len(prompt) > 4000:
                raise ValueError("prompt is invalid")
            if not isinstance(context, dict):
                raise ValueError("context is invalid")
            result = planner.plan(kind, prompt.strip(), context)
            return JSONResponse(result)
        except (ValueError, json.JSONDecodeError):
            return JSONResponse({"error": "bad_request"}, status_code=400)
        except ParseError:
            return JSONResponse({"error": "planning_failed"}, status_code=422)
        except Exception:
            return JSONResponse({"error": "provider_unavailable"}, status_code=502)

    async def daily_parse(request: Request):
        supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
        if not token or not secrets.compare_digest(supplied, token):
            return JSONResponse({"error": "unauthorized"}, status_code=401)
        try:
            data = await request.json()
            raw_text, projects, report_date = validate_daily_parse(data)
            result = parser.parse_daily_logs(raw_text, projects, report_date)
            return JSONResponse(result)
        except (ValueError, json.JSONDecodeError):
            return JSONResponse({"error": "bad_request"}, status_code=400)
        except ParseError:
            return JSONResponse({"error": "daily_parse_failed"}, status_code=422)
        except Exception:
            return JSONResponse({"error": "provider_unavailable"}, status_code=502)

    async def todo_add(request: Request):
        supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
        if not token or not secrets.compare_digest(supplied, token):
            return JSONResponse({"error": "unauthorized"}, status_code=401)
        try:
            data = await request.json()
            raw_text, user_id = validate_todo_add(data)
            result = todo_planner.parse_add(raw_text, user_id)
            return JSONResponse(result)
        except (ValueError, json.JSONDecodeError):
            return JSONResponse({"error": "bad_request"}, status_code=400)
        except ParseError:
            return JSONResponse({"error": "todo_add_failed"}, status_code=422)
        except Exception:
            return JSONResponse({"error": "provider_unavailable"}, status_code=502)

    async def todo_daily(request: Request):
        supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
        if not token or not secrets.compare_digest(supplied, token):
            return JSONResponse({"error": "unauthorized"}, status_code=401)
        try:
            data = await request.json()
            user_id, yesterday_report, open_issues, existing_titles = validate_todo_daily(data)
            result = todo_planner.generate_daily(user_id, yesterday_report, open_issues, existing_titles)
            return JSONResponse(result)
        except (ValueError, json.JSONDecodeError):
            return JSONResponse({"error": "bad_request"}, status_code=400)
        except ParseError:
            return JSONResponse({"error": "todo_daily_failed"}, status_code=422)
        except Exception:
            return JSONResponse({"error": "provider_unavailable"}, status_code=502)

    return Starlette(routes=[
        Route("/health", health),
        Route("/v1/interpret", interpret, methods=["POST"]),
        Route("/v1/step-plan", step_plan, methods=["POST"]),
        Route("/v1/daily-parse", daily_parse, methods=["POST"]),
        Route("/v1/todo-add", todo_add, methods=["POST"]),
        Route("/v1/todo-daily", todo_daily, methods=["POST"]),
    ])


if __name__ == "__main__":
    api_key = os.getenv("DEEPSEEK_API_KEY")
    if not api_key:
        raise RuntimeError("DEEPSEEK_API_KEY environment variable is not set")
    daily_prompt = Path(__file__).parent / "prompts" / "daily_logs.txt"
    app = create_app(
        InstrumentInterpreter(api_key), StepPlanner(api_key),
        Parser(api_key, prompt_path=daily_prompt), TodoPlanner(api_key), read_token(),
    )
    uvicorn.run(app, host="0.0.0.0", port=8001)
