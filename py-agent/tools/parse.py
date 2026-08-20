import json
import os
import re
from datetime import datetime
from pathlib import Path


# 模型与接入配置走环境变量（默认值保持历史硬编码行为）。
# stepplan.py/todoplan.py 经 `from tools.parse import MODEL, BASE_URL` 引用，同源生效。
MODEL = os.environ.get("DEEPSEEK_MODEL", "deepseek-v4-pro")
BASE_URL = os.environ.get("DEEPSEEK_BASE_URL", "https://api.deepseek.com")
# 备选模型：本期仅配置化，不实现自动降级（空 = 未配置）。
FALLBACK_MODEL = os.environ.get("DEEPSEEK_FALLBACK_MODEL", "")
# worker 对单次 LLM 解析的硬超时（秒），必须小于 worker 的 AGENT_LEASE_SECONDS。
LLM_TIMEOUT_SECONDS = int(os.environ.get("LLM_TIMEOUT_SECONDS", "180"))
DAILY_LOG_CATEGORIES = {"general", "assembly", "test", "cryo", "rf", "vacuum", "beam", "data_analysis"}
INJECTION = re.compile(
    r"忽略(?:之前|以上).*指令|ignore (?:all )?(?:previous|prior).*instructions?"
    r"|execute_python_code|upload_file_to_oss|动态.*tool|tool.*generation",
    re.IGNORECASE,
)
# 与 Go 侧 time.RFC3339 对齐：T 分隔、秒、可选小数、Z 或 ±hh:mm 时区。
RFC3339 = re.compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$"
)


class ParseError(RuntimeError):
    pass


def ensure_safe(raw_text):
    if INJECTION.search(raw_text):
        raise ParseError("prompt injection rejected")


class Parser:
    def __init__(self, api_key, prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        prompt_path = prompt_path or Path(__file__).parents[1] / "prompts" / "parse.txt"
        self.instructions = Path(prompt_path).read_text()
        self.agent = LightAgent(
            name="daily-report-parser", model=MODEL, base_url=BASE_URL, api_key=api_key,
            instructions=self.instructions, tools=[], filter_tools=True, tree_of_thought=False,
            memory=None, self_learning=False, auto_discover_skills=False,
            hooks=[NoToolHook()], debug=False,
        )
        # LightAgent 0.9.4 registers built-ins even when tools=[]; parsing needs none.
        self.agent.tool_registry = ToolRegistry()
        self.agent.loaded_tools = {}

    def parse(self, raw_text, existing_issues, project_ids):
        ensure_safe(raw_text)
        query = json.dumps({
            "trusted_context": {
                "allowed_actions": ["create_issue", "add_comment"],
                "project_ids": project_ids,
                "existing_issues": [
                    {key: issue.get(key) for key in ("id", "project_id", "title", "description")}
                    for issue in existing_issues[:10]
                ],
            },
            "untrusted_inputs": [{"type": "daily_report", "content": raw_text}],
        }, ensure_ascii=False)
        result = self.agent.run(
            query, tools=[], use_skills=False, max_retry=3, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return validate_candidates(_json_array(str(result)), existing_issues, project_ids)

    def parse_daily_logs(self, raw_text, projects, report_date, linked_logs=None):
        ensure_safe(raw_text)
        linked_logs = linked_logs or []
        for log in linked_logs:
            ensure_safe(log["content"])
        query = json.dumps({
            "trusted_context": {
                "report_date": report_date,
                "projects": [
                    {"id": project["id"], "name": project["name"]}
                    for project in projects
                ],
            },
            "untrusted_inputs": {
                "daily_report": raw_text,
                "linked_logs": linked_logs,
            },
        }, ensure_ascii=False)
        result = self.agent.run(
            query, tools=[], use_skills=False, max_retry=3, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        allowed = {project["id"] for project in projects}
        return validate_daily_logs(_json_object(str(result)), allowed, raw_text)


def _json_array(text):
    failure = re.search(r"\[(LA-[A-Z0-9]+)]", text)
    if failure:
        raise ParseError(f"model request failed ({failure.group(1)})")
    start, end = text.find("["), text.rfind("]")
    if start < 0 or end < start:
        raise ParseError("model did not return a JSON array")
    try:
        value = json.loads(text[start:end + 1])
    except json.JSONDecodeError as exc:
        raise ParseError("model returned invalid JSON") from exc
    if not isinstance(value, list):
        raise ParseError("model output must be a JSON array")
    return value


def _json_object(text):
    failure = re.search(r"\[(LA-[A-Z0-9]+)]", text)
    if failure:
        raise ParseError(f"model request failed ({failure.group(1)})")
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end < start:
        raise ParseError("model did not return a JSON object")
    try:
        value = json.loads(text[start:end + 1])
    except json.JSONDecodeError as exc:
        raise ParseError("model returned invalid JSON") from exc
    if not isinstance(value, dict):
        raise ParseError("model output must be a JSON object")
    return value


class InstrumentInterpreter:
    def __init__(self, api_key, prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        prompt_path = prompt_path or Path(__file__).parents[1] / "prompts" / "instrument_interpret.txt"
        self.agent = LightAgent(
            name="instrument-command-interpreter", model=MODEL, base_url=BASE_URL, api_key=api_key,
            instructions=Path(prompt_path).read_text(), tools=[], filter_tools=True, tree_of_thought=False,
            memory=None, self_learning=False, auto_discover_skills=False,
            hooks=[NoToolHook()], debug=False,
        )
        self.agent.tool_registry = ToolRegistry()
        self.agent.loaded_tools = {}

    def interpret(self, instrument_id, instrument_name, whitelist_commands, user_input, history):
        ensure_safe(user_input)
        for item in history:
            ensure_safe(item["content"])
        allowed = {item["name"] for item in whitelist_commands}
        query = json.dumps({
            "trusted_context": {
                "instrument_id": instrument_id, "instrument_name": instrument_name,
                "whitelist_commands": whitelist_commands,
            },
            "untrusted_inputs": {"user_input": user_input, "history": history},
        }, ensure_ascii=False)
        result = self.agent.run(
            query, tools=[], use_skills=False, max_retry=2, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return validate_interpretation(_json_object(str(result)), allowed)


def validate_interpretation(item, allowed_commands):
    status = item.get("status")
    if status not in {"ok", "clarify", "rejected"}:
        raise ParseError("interpretation status is invalid")
    confidence = item.get("confidence", 0)
    if not isinstance(confidence, (int, float)) or isinstance(confidence, bool) or not 0 <= confidence <= 1:
        raise ParseError("interpretation confidence is invalid")
    command = item.get("command")
    params = item.get("params", {})
    if status == "ok" and (command not in allowed_commands or not isinstance(params, dict)):
        raise ParseError("interpretation command or params are invalid")
    if status == "clarify" and not str(item.get("question", "")).strip():
        raise ParseError("clarification question is required")
    if status == "rejected" and not str(item.get("reason", "")).strip():
        raise ParseError("rejection reason is required")
    return {
        "status": status, "command": command if status == "ok" else None,
        "params": params if status == "ok" else {}, "confidence": float(confidence),
        "explanation": str(item.get("explanation", "")).strip(),
        "question": str(item.get("question", "")).strip() or None,
        "reason": str(item.get("reason", "")).strip() or None,
        "prompt_version": "1.0", "model": MODEL,
    }


def _raw_text_segments(raw_text):
    return [part.strip() for part in re.split(r"[。！？；\r\n]", raw_text) if part.strip()]


def validate_daily_logs(item, allowed_projects, raw_text):
    status = item.get("status")
    if status not in {"ok", "clarify", "rejected"}:
        raise ParseError("daily parse status is invalid")
    if status == "clarify" and not str(item.get("question", "")).strip():
        raise ParseError("clarification question is required")
    if status == "rejected" and not str(item.get("reason", "")).strip():
        raise ParseError("rejection reason is required")
    logs = item.get("logs")
    out = []
    if status == "ok":
        if not isinstance(logs, list) or not 1 <= len(logs) <= 20:
            raise ParseError("daily parse logs count must be 1-20")
        for entry in logs:
            if not isinstance(entry, dict):
                raise ParseError("daily parse log must be an object")
            category = entry.get("category")
            project_id = entry.get("project_id")
            content = entry.get("content")
            occurred_at = entry.get("occurred_at")
            raw_snippet = entry.get("raw_snippet")
            if not isinstance(content, str) or not isinstance(occurred_at, str) or not isinstance(raw_snippet, str):
                raise ParseError("daily parse log content, occurred_at or raw_snippet must be strings")
            content = content.strip()
            occurred_at = occurred_at.strip()
            if category not in DAILY_LOG_CATEGORIES or project_id not in allowed_projects:
                raise ParseError("daily parse log category or project is invalid")
            if not 1 <= len(content) <= 2000:
                raise ParseError("daily parse log content length is invalid")
            if not 1 <= len(raw_snippet) <= 4000 or raw_snippet not in _raw_text_segments(raw_text):
                raise ParseError("daily parse log raw_snippet is invalid")
            if not RFC3339.match(occurred_at):
                raise ParseError("daily parse log occurred_at is invalid")
            try:
                parsed = datetime.fromisoformat(occurred_at.replace("Z", "+00:00"))
            except ValueError as exc:
                raise ParseError("daily parse log occurred_at is invalid") from exc
            if parsed.tzinfo is None:
                raise ParseError("daily parse log occurred_at is invalid")
            out.append({
                "category": category, "project_id": project_id,
                "content": content, "occurred_at": occurred_at, "raw_snippet": raw_snippet,
            })
    summary = item.get("summary")
    if status == "ok":
        if not isinstance(summary, str) or not 1 <= len(summary.strip()) <= 1000:
            raise ParseError("daily parse summary is invalid")
        summary = summary.strip()
    else:
        summary = None
    return {
        "status": status, "logs": out, "summary": summary,
        "question": str(item.get("question", "")).strip() or None,
        "reason": str(item.get("reason", "")).strip() or None,
        "model": MODEL, "prompt_version": "1.2",
        }


def validate_candidates(items, existing_issues, project_ids):
    issue_projects = {item.get("id"): item.get("project_id") for item in existing_issues}
    allowed_projects = set(project_ids)
    out = []
    for item in items:
        if not isinstance(item, dict):
            raise ParseError("candidate must be an object")
        action = item.get("action_type")
        project_id = item.get("project_id")
        title = str(item.get("title", "")).strip()
        description = str(item.get("description", "")).strip()
        severity = item.get("severity", "medium")
        confidence = item.get("confidence")
        duplicate = item.get("is_duplicate")
        duplicate_id = item.get("duplicate_issue_id")
        if action not in {"create_issue", "add_comment"} or project_id not in allowed_projects:
            raise ParseError("candidate action or project is invalid")
        if not title or not description or severity not in {"low", "medium", "high", "critical"}:
            raise ParseError("candidate content is invalid")
        if not isinstance(confidence, (int, float)) or not 0 <= confidence <= 1:
            raise ParseError("candidate confidence is invalid")
        if not isinstance(duplicate, bool) or (duplicate and duplicate_id not in issue_projects):
            raise ParseError("candidate duplicate reference is invalid")
        if duplicate and issue_projects[duplicate_id] != project_id:
            raise ParseError("candidate duplicate project is invalid")
        if duplicate != (action == "add_comment"):
            raise ParseError("candidate duplicate action is inconsistent")
        out.append({
            "action_type": action, "project_id": project_id, "title": title,
            "description": description, "severity": severity, "confidence": float(confidence),
            "is_duplicate": duplicate, "duplicate_issue_id": duplicate_id if duplicate else None,
        })
    return out
