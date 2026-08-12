import json
from pathlib import Path

from tools.parse import (
    MODEL,
    BASE_URL,
    ParseError,
    ensure_safe,
    _json_object,
)


class WeeklySummarizer:
    """周报自动摘要（AI-1）：两步 LLM —— digest 提炼 → write 成稿。

    照 TodoPlanner 模式：NoToolHook + tools=[] + registry 重置三层叠加，强制无工具边界；
    所有不可信输入（report raw_text/summary、issue_stats 文本化结果）过 ensure_safe。
    两步共用一次类实例（两个 LightAgent），mock 友好：校验器 validate_weekly_summary 可独立测试。
    """

    # 两步 LLM 总预算（秒）：serve 层 asyncio.wait_for 兜底（每步 ≤180s + 余量）。
    TOTAL_TIMEOUT = 300

    def __init__(self, api_key, digest_prompt_path=None, write_prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        prompts = Path(__file__).parents[1] / "prompts"
        self.digest_agent = self._build_agent(
            api_key, "weekly-digester",
            digest_prompt_path or prompts / "weekly_digest.txt",
            HookDecision, LightAgent, ToolRegistry, NoToolHook,
        )
        self.write_agent = self._build_agent(
            api_key, "weekly-writer",
            write_prompt_path or prompts / "weekly_summary.txt",
            HookDecision, LightAgent, ToolRegistry, NoToolHook,
        )

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

    def summarize(self, week_start, week_end, reports, issue_stats):
        """生成周报。reports 为 [{report_date, author_name, raw_text, summary}]，
        issue_stats 为 {created, resolved, open_high_critical}。返回 validate_weekly_summary 校验后的字典。"""
        for report in reports:
            ensure_safe(str(report.get("raw_text", "")))
            ensure_safe(str(report.get("summary", "")))
            ensure_safe(str(report.get("author_name", "")))
        stats_text = json.dumps(issue_stats, ensure_ascii=False) if issue_stats else "{}"
        ensure_safe(stats_text)
        digest = self._digest(week_start, week_end, reports, stats_text)
        return validate_weekly_summary(self._write(week_start, week_end, digest))

    def _digest(self, week_start, week_end, reports, stats_text):
        query = json.dumps({
            "trusted_context": {"week_start": week_start, "week_end": week_end},
            "untrusted_inputs": {
                "issue_stats": stats_text,
                "reports": [
                    {"report_date": r.get("report_date", ""),
                     "author_name": r.get("author_name", ""),
                     "raw_text": r.get("raw_text", ""),
                     "summary": r.get("summary", "")}
                    for r in reports
                ],
            },
        }, ensure_ascii=False)
        result = self.digest_agent.run(
            query, tools=[], use_skills=False, max_retry=2, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return _json_object(str(result))

    def _write(self, week_start, week_end, digest):
        ensure_safe(json.dumps(digest, ensure_ascii=False))
        query = json.dumps({
            "trusted_context": {"week_start": week_start, "week_end": week_end},
            "untrusted_inputs": {"digest": digest},
        }, ensure_ascii=False)
        result = self.write_agent.run(
            query, tools=[], use_skills=False, max_retry=2, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return _json_object(str(result))


def validate_weekly_summary(item):
    """周报输出校验：结构、长度、枚举全量收紧（对齐 validate_todo_daily 风格）。"""
    status = item.get("status")
    if status != "ok":
        raise ParseError("weekly summary status is invalid")
    title = str(item.get("title", "")).strip()
    summary = str(item.get("summary", "")).strip()
    markdown = str(item.get("markdown", "")).strip()
    if not title or len(title) > 256:
        raise ParseError("weekly summary title is invalid")
    if not summary or len(summary) > 1000:
        raise ParseError("weekly summary summary is invalid")
    if not markdown or len(markdown) > 30_000:
        raise ParseError("weekly summary markdown is invalid")
    return {
        "status": "ok",
        "title": title,
        "summary": summary,
        "markdown": markdown,
        "highlights": _validate_bullets(item.get("highlights", [])),
        "problems": _validate_bullets(item.get("problems", [])),
        "data_points": _validate_bullets(item.get("data_points", [])),
        "model": MODEL,
    }


def _validate_bullets(items):
    if items is None:
        return []
    if not isinstance(items, list) or len(items) > 25:
        raise ParseError("weekly summary bullet list is invalid")
    out = []
    for entry in items:
        if not isinstance(entry, str):
            raise ParseError("weekly summary bullet item must be a string")
        text = entry.strip()
        if not text or len(text) > 1000:
            raise ParseError("weekly summary bullet item is invalid")
        out.append(text)
    return out
