import json
from pathlib import Path

from tools.parse import (
    MODEL,
    BASE_URL,
    ParseError,
    ensure_safe,
    _json_object,
)


class ExperienceExtractor:
    """经验库自动提取（AI-2）：从最近 7 天 resolved/closed 的 issue 提炼 0-N 条经验候选。

    照 WeeklySummarizer 模式：NoToolHook + tools=[] + registry 重置三层叠加，强制无工具边界；
    所有不可信输入（issue 标题/描述/评论/run_id 文本）过 ensure_safe。
    mock 友好：校验器 validate_experience_candidates 可独立测试。
    """

    # 单步 LLM 总预算（秒）：serve 层 asyncio.wait_for 兜底（≤180s 重试预算 + 余量）。
    TOTAL_TIMEOUT = 240

    def __init__(self, api_key, prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        prompt_path = prompt_path or Path(__file__).parents[1] / "prompts" / "experience_extract.txt"
        self.agent = LightAgent(
            name="experience-extractor", model=MODEL, base_url=BASE_URL, api_key=api_key,
            instructions=Path(prompt_path).read_text(), tools=[], filter_tools=True, tree_of_thought=False,
            memory=None, self_learning=False, auto_discover_skills=False,
            hooks=[NoToolHook()], debug=False,
        )
        # LightAgent 0.9.4 registers built-ins even when tools=[]; extraction needs none.
        self.agent.tool_registry = ToolRegistry()
        self.agent.loaded_tools = {}

    def extract(self, issues):
        """提炼经验候选。issues 为 [{id, project_id, title, description, comments, run_id}]。

        所有 issue 文本（含评论）视为不可信输入，逐条过 ensure_safe；
        返回 validate_experience_candidates 校验后的 entries 列表。
        """
        for issue in issues:
            ensure_safe(str(issue.get("title", "")))
            ensure_safe(str(issue.get("description", "")))
            for comment in issue.get("comments", []) or []:
                ensure_safe(str(comment))
        allowed = {str(issue.get("id")) for issue in issues if issue.get("id")}
        query = json.dumps({
            "trusted_context": {"issue_ids": sorted(allowed)},
            "untrusted_inputs": {
                "issues": [
                    {
                        "id": str(issue.get("id", "")),
                        "project_id": str(issue.get("project_id", "")),
                        "title": str(issue.get("title", "")),
                        "description": str(issue.get("description", "")),
                        "comments": [str(c) for c in (issue.get("comments", []) or [])],
                        "run_id": str(issue.get("run_id", "")),
                    }
                    for issue in issues
                ],
            },
        }, ensure_ascii=False)
        result = self.agent.run(
            query, tools=[], use_skills=False, max_retry=2, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return validate_experience_candidates(_json_object(str(result)), allowed)


def validate_experience_candidates(item, allowed_issue_ids):
    """经验候选输出校验：status=ok、entries 0-10 条、字段长度/枚举/置信度全量收紧。

    issue_id 必须是输入 issue 清单内的合法 id（防模型幻觉引用不存在的 issue）。
    照 validate_weekly_summary 风格，可独立测试。
    """
    if item.get("status") != "ok":
        raise ParseError("experience extract status is invalid")
    entries = item.get("entries", [])
    if not isinstance(entries, list) or len(entries) > 10:
        raise ParseError("experience entries count must be 0-10")
    out = []
    for entry in entries:
        if not isinstance(entry, dict):
            raise ParseError("experience entry must be an object")
        title = str(entry.get("title", "")).strip()
        content = str(entry.get("content", "")).strip()
        if not title or len(title) > 256:
            raise ParseError("experience title is invalid")
        if not content or len(content) > 2000:
            raise ParseError("experience content is invalid")
        raw_tags = entry.get("tags", [])
        if not isinstance(raw_tags, list) or len(raw_tags) > 10:
            raise ParseError("experience tags are invalid")
        tags = []
        for tag in raw_tags:
            tag = str(tag).strip().lower()
            if not tag or len(tag) > 32:
                raise ParseError("experience tag is invalid")
            tags.append(tag)
        issue_id = entry.get("issue_id")
        if issue_id not in allowed_issue_ids:
            raise ParseError("experience issue_id is invalid")
        confidence = entry.get("confidence", 0)
        if not isinstance(confidence, (int, float)) or isinstance(confidence, bool) or not 0 <= confidence <= 1:
            raise ParseError("experience confidence is invalid")
        out.append({
            "issue_id": issue_id,
            "title": title,
            "content": content,
            "tags": tags,
            "confidence": float(confidence),
        })
    return {
        "status": "ok",
        "entries": out,
        "model": MODEL,
        "prompt_version": "1.0",
    }
