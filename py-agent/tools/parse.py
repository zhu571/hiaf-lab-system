import json
import re
from pathlib import Path

from LightAgent import HookDecision, LightAgent, ToolRegistry


MODEL = "deepseek-v4-pro"
BASE_URL = "https://api.deepseek.com"
INJECTION = re.compile(
    r"忽略(?:之前|以上).*指令|ignore (?:all )?(?:previous|prior).*instructions?"
    r"|execute_python_code|upload_file_to_oss|动态.*tool|tool.*generation",
    re.IGNORECASE,
)


class ParseError(RuntimeError):
    pass


def ensure_safe(raw_text):
    if INJECTION.search(raw_text):
        raise ParseError("prompt injection rejected")


class NoToolHook:
    def __call__(self, context):
        if context.phase == "before_model_request":
            params = context.payload["params"]
            if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                return HookDecision.block("model request escaped the no-tool non-thinking boundary")
        return HookDecision.continue_()


class Parser:
    def __init__(self, api_key, prompt_path=None):
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
