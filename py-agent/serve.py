import asyncio
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

from tools.ask import AskEngine
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


def check(cond, msg):
    """请求校验辅助：条件不满足抛 ValueError（端点工厂统一映射 400）。"""
    if not cond:
        raise ValueError(msg)


def _size_ok(data):
    return isinstance(data, dict) and len(json.dumps(data, ensure_ascii=False)) <= 64_000


def validate_request(data):
    check(_size_ok(data), "request too large")
    user_input = data.get("user_input")
    history = data.get("history", [])
    commands = data.get("whitelist_commands")
    check(isinstance(user_input, str) and user_input.strip() and len(user_input) <= 1000, "user_input is invalid")
    check(isinstance(history, list) and len(history) <= 10, "history is invalid")
    for item in history:
        check(isinstance(item, dict) and item.get("role") in {"user", "assistant"}
              and isinstance(item.get("content"), str) and len(item["content"]) <= 1000, "history item is invalid")
    check(isinstance(commands, list) and commands and len(commands) <= 100, "whitelist_commands is invalid")
    for command in commands:
        check(isinstance(command, dict) and isinstance(command.get("name"), str), "whitelist command is invalid")
    return user_input.strip(), history, commands


def validate_step_plan(data):
    check(_size_ok(data), "request too large")
    kind = data.get("kind")
    prompt = data.get("prompt")
    context = data.get("context") or {}
    check(kind in {"assembly", "experiment"}, "kind is invalid")
    check(isinstance(prompt, str) and prompt.strip() and len(prompt) <= 4000, "prompt is invalid")
    check(isinstance(context, dict), "context is invalid")
    return kind, prompt.strip(), context


REPORT_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def validate_daily_parse(data):
    check(_size_ok(data), "request too large")
    raw_text = data.get("raw_text")
    projects = data.get("projects")
    report_date = data.get("report_date")
    check(isinstance(raw_text, str) and 1 <= len(raw_text.strip()) <= 4000, "raw_text is invalid")
    check(isinstance(projects, list) and 1 <= len(projects) <= 50, "projects is invalid")
    for project in projects:
        check(isinstance(project, dict) and isinstance(project.get("id"), str) and isinstance(project.get("name"), str),
              "project is invalid")
    check(isinstance(report_date, str) and REPORT_DATE.match(report_date), "report_date is invalid")
    return raw_text.strip(), projects, report_date


def validate_todo_add(data):
    check(_size_ok(data), "request too large")
    raw_text = data.get("raw_text")
    user_id = data.get("user_id")
    check(isinstance(raw_text, str) and raw_text.strip() and len(raw_text.strip()) <= 2000, "raw_text is invalid")
    check(isinstance(user_id, str) and user_id.strip() and len(user_id.strip()) <= 128, "user_id is invalid")
    return raw_text.strip(), user_id.strip()


def validate_todo_daily(data):
    check(_size_ok(data), "request too large")
    user_id = data.get("user_id")
    yesterday_report = data.get("yesterday_report", "")
    open_issues = data.get("open_issues", [])
    existing_titles = data.get("existing_titles", [])
    check(isinstance(user_id, str) and user_id.strip() and len(user_id.strip()) <= 128, "user_id is invalid")
    check(isinstance(yesterday_report, str) and len(yesterday_report) <= 20_000, "yesterday_report is invalid")
    check(isinstance(open_issues, list) and len(open_issues) <= 50, "open_issues is invalid")
    for issue in open_issues:
        check(isinstance(issue, dict), "open_issue is invalid")
        check(isinstance(issue.get("id"), str) and issue.get("id")
              and isinstance(issue.get("title"), str) and issue.get("title"), "open_issue fields are invalid")
    check(isinstance(existing_titles, list) and len(existing_titles) <= 50, "existing_titles is invalid")
    for title in existing_titles:
        check(isinstance(title, str) and len(title) <= 300, "existing_title is invalid")
    return user_id.strip(), yesterday_report, open_issues, existing_titles


def validate_ask(data):
    """AI 智能查询：question ≤ 1000 字符（strip 非空）、schema ≤ 64KB、history 可选（≤10 条）。"""
    check(_size_ok(data), "request too large")
    question = data.get("question")
    schema = data.get("schema")
    history = data.get("history", [])
    check(isinstance(question, str) and question.strip() and len(question) <= 1000, "question is invalid")
    check(isinstance(schema, str) and len(schema) <= 64_000, "schema is invalid")
    check(isinstance(history, list) and len(history) <= 10, "history is invalid")
    for item in history:
        check(isinstance(item, dict) and item.get("role") in {"user", "assistant"}
              and isinstance(item.get("content"), str) and len(item["content"]) <= 1000, "history item is invalid")
    return question.strip(), schema, history


def create_app(interpreter, planner, parser, todo_planner, token, ask_engine=None):
    def make_endpoint(validate, handler, parse_error_code):
        """端点工厂：统一 Bearer 鉴权、JSON 解析（64KB 上限）、三态异常映射（400/422/502）。

        handler 接收 (validated, 原始 dict)：interpret 需读原始 data 的
        instrument_id/instrument_name（validate_request 不校验这两字段）。
        """
        async def endpoint(request: Request):
            supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
            if not token or not secrets.compare_digest(supplied, token):
                return JSONResponse({"error": "unauthorized"}, status_code=401)
            try:
                data = await request.json()
                result = await handler(validate(data), data)
                return JSONResponse(result)
            except (ValueError, json.JSONDecodeError):
                return JSONResponse({"error": "bad_request"}, status_code=400)
            except ParseError:
                return JSONResponse({"error": parse_error_code}, status_code=422)
            except Exception:
                return JSONResponse({"error": "provider_unavailable"}, status_code=502)
        return endpoint

    async def health(_request):
        return JSONResponse({"status": "ok"})

    # 同步 LLM 调用一律 asyncio.to_thread 放线程池，避免慢请求阻塞整个事件循环（C1 缺口 C）。

    async def do_interpret(validated, data):
        user_input, history, commands = validated
        return await asyncio.to_thread(
            interpreter.interpret,
            str(data.get("instrument_id", ""))[:128], str(data.get("instrument_name", ""))[:256],
            commands, user_input, history,
        )

    async def do_step_plan(validated, _data):
        kind, prompt, context = validated
        return await asyncio.to_thread(planner.plan, kind, prompt, context)

    async def do_daily_parse(validated, _data):
        raw_text, projects, report_date = validated
        return await asyncio.to_thread(parser.parse_daily_logs, raw_text, projects, report_date)

    async def do_todo_add(validated, _data):
        raw_text, user_id = validated
        return await asyncio.to_thread(todo_planner.parse_add, raw_text, user_id)

    async def do_todo_daily(validated, _data):
        user_id, yesterday_report, open_issues, existing_titles = validated
        return await asyncio.to_thread(
            todo_planner.generate_daily, user_id, yesterday_report, open_issues, existing_titles,
        )

    async def do_ask(validated, _data):
        if ask_engine is None:
            raise RuntimeError("ask engine is not configured")
        question, schema, history = validated
        # 总超时 60s 预算（规划 25s + 执行 5s + 整合 25s + 余量；重试共享同一预算）：
        # 两步 LLM 规划 + 一次执行 + 一次整合整体 wait_for，超时 → 502 provider_unavailable。
        return await asyncio.wait_for(
            asyncio.to_thread(ask_engine.ask, question, schema, history),
            timeout=AskEngine.TOTAL_TIMEOUT,
        )

    return Starlette(routes=[
        Route("/health", health),
        Route("/v1/interpret", make_endpoint(validate_request, do_interpret, "interpretation_failed"), methods=["POST"]),
        Route("/v1/step-plan", make_endpoint(validate_step_plan, do_step_plan, "planning_failed"), methods=["POST"]),
        Route("/v1/daily-parse", make_endpoint(validate_daily_parse, do_daily_parse, "daily_parse_failed"), methods=["POST"]),
        Route("/v1/todo-add", make_endpoint(validate_todo_add, do_todo_add, "todo_add_failed"), methods=["POST"]),
        Route("/v1/todo-daily", make_endpoint(validate_todo_daily, do_todo_daily, "todo_daily_failed"), methods=["POST"]),
        Route("/v1/ask", make_endpoint(validate_ask, do_ask, "ask_failed"), methods=["POST"]),
    ])


if __name__ == "__main__":
    api_key = os.getenv("DEEPSEEK_API_KEY")
    if not api_key:
        raise RuntimeError("DEEPSEEK_API_KEY environment variable is not set")
    daily_prompt = Path(__file__).parent / "prompts" / "daily_logs.txt"
    app = create_app(
        InstrumentInterpreter(api_key), StepPlanner(api_key),
        Parser(api_key, prompt_path=daily_prompt), TodoPlanner(api_key), read_token(),
        ask_engine=AskEngine(api_key),
    )
    uvicorn.run(app, host="0.0.0.0", port=8001)
