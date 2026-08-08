import json
import unicodedata
from pathlib import Path

from tools.parse import (
    MODEL,
    BASE_URL,
    ParseError,
    ensure_safe,
    _json_object,
)


def normalize_title(title):
    """归一化去重：NFKC 全半角 + ToLower + 空白折叠（与 Go 侧 trim+ToLower+空白折叠对齐）。"""
    text = unicodedata.normalize("NFKC", str(title).strip().lower())
    return " ".join(text.split())


class TodoPlanner:
    """待办规划器：todo-add（自然语言 → 单条草稿）与 todo-daily（日报/issue → 明日待办列表）。

    照 StepPlanner 模式：NoToolHook + tools=[] + registry 重置三层叠加，强制无工具边界；
    所有不可信输入（raw_text、日报正文、issue 标题）过 ensure_safe。
    """

    def __init__(self, api_key, add_prompt_path=None, daily_prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        add_prompt_path = add_prompt_path or Path(__file__).parents[1] / "prompts" / "todo_add.txt"
        daily_prompt_path = daily_prompt_path or Path(__file__).parents[1] / "prompts" / "todo_daily.txt"
        self.add_agent = self._build_agent(api_key, "todo-planner", add_prompt_path, HookDecision, LightAgent, ToolRegistry, NoToolHook)
        self.daily_agent = self._build_agent(api_key, "todo-daily-planner", daily_prompt_path, HookDecision, LightAgent, ToolRegistry, NoToolHook)

    @staticmethod
    def _build_agent(api_key, name, prompt_path, HookDecision, LightAgent, ToolRegistry, NoToolHook):
        agent = LightAgent(
            name=name, model=MODEL, base_url=BASE_URL, api_key=api_key,
            instructions=Path(prompt_path).read_text(), tools=[], filter_tools=True, tree_of_thought=False,
            memory=None, self_learning=False, auto_discover_skills=False,
            hooks=[NoToolHook()], debug=False,
        )
        agent.tool_registry = ToolRegistry()
        agent.loaded_tools = {}
        return agent

    def parse_add(self, raw_text, user_id):
        """解析自然语言为单条待办草稿。返回 {status, title, priority, reason}。"""
        ensure_safe(raw_text)
        query = json.dumps({
            "trusted_context": {"user_id": user_id},
            "untrusted_inputs": {"raw_text": raw_text},
        }, ensure_ascii=False)
        result = self.add_agent.run(
            query, tools=[], use_skills=False, max_retry=2, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return validate_todo_add(_json_object(str(result)))

    def generate_daily(self, user_id, yesterday_report, open_issues, existing_titles):
        """由昨日日报与 open issues 生成明日待办列表。返回 {status, items:[{title, priority}]}。"""
        ensure_safe(yesterday_report)
        for issue in open_issues:
            ensure_safe(str(issue.get("title", "")))
        query = json.dumps({
            "trusted_context": {
                "user_id": user_id,
                "existing_titles": existing_titles,
            },
            "untrusted_inputs": {
                "yesterday_report": yesterday_report,
                "open_issues": [
                    {"id": str(i.get("id", "")), "title": str(i.get("title", "")), "severity": str(i.get("severity", ""))}
                    for i in open_issues
                ],
            },
        }, ensure_ascii=False)
        result = self.daily_agent.run(
            query, tools=[], use_skills=False, max_retry=2, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return validate_todo_daily(_json_object(str(result)), existing_titles)


def validate_todo_add(item):
    status = item.get("status")
    if status not in {"ok", "rejected"}:
        raise ParseError("todo add status is invalid")
    title = str(item.get("title", "")).strip()
    priority = str(item.get("priority", "medium")).strip()
    if priority not in {"high", "medium", "low"}:
        priority = "medium"
    if status == "ok":
        if not title or len(title) > 256:
            raise ParseError("todo title is invalid")
    if status == "rejected" and not str(item.get("reason", "")).strip():
        raise ParseError("rejection reason is required")
    return {
        "status": status,
        "title": title if status == "ok" else "",
        "priority": priority if status == "ok" else "",
        "reason": str(item.get("reason", "")).strip() or None,
    }


def validate_todo_daily(item, existing_titles):
    status = item.get("status")
    if status != "ok":
        raise ParseError("todo daily status is invalid")
    items = item.get("items", [])
    if not isinstance(items, list) or len(items) > 8:
        raise ParseError("todo daily items must be 0-8")  # 去重后由 Go 侧截断到 4
    existing = {normalize_title(t) for t in existing_titles}
    out = []
    for entry in items:
        title = str(entry.get("title", "")).strip()
        if not title or len(title) > 256:
            raise ParseError("todo daily item title is invalid")
        priority = str(entry.get("priority", "medium")).strip()
        if priority not in {"high", "medium", "low"}:
            priority = "medium"
        key = normalize_title(title)
        if key in existing:
            continue  # LLM 去重归一：与 issue/本批已生成项重复 → 丢弃
        existing.add(key)
        out.append({"title": title, "priority": priority})
    return {"status": "ok", "items": out}
