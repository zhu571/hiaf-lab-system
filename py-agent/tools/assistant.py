import json
from pathlib import Path

from tools.parse import MODEL, BASE_URL, ensure_safe


class AssistantReplier:
    """多人助手单轮回复：无工具、无技能、无记忆的 LightAgent。

    只做“基于历史的自然语言回答”，不执行任何工具；写动作仍走候选审批链路。
    所有用户/助手文本过 ensure_safe，防止提示注入被当成系统指令。
    """

    TOTAL_TIMEOUT = 120

    def __init__(self, api_key, prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        prompts = Path(__file__).parents[1] / "prompts"
        self.agent = LightAgent(
            name="assistant-replier",
            model=MODEL,
            base_url=BASE_URL,
            api_key=api_key,
            instructions=Path(prompt_path or prompts / "assistant_chat.txt").read_text(),
            tools=[],
            filter_tools=True,
            tree_of_thought=False,
            memory=None,
            self_learning=False,
            auto_discover_skills=False,
            hooks=[NoToolHook()],
            debug=False,
        )
        self.agent.tool_registry = ToolRegistry()
        self.agent.loaded_tools = {}

    def reply(self, messages):
        """messages: [{role: user|assistant, content: str}]，返回 {answer, model}。"""
        safe_messages = []
        for message in messages:
            role = message.get("role")
            content = str(message.get("content", ""))
            ensure_safe(content)
            safe_messages.append({"role": role, "content": content[:4000]})
        query = json.dumps({"messages": safe_messages}, ensure_ascii=False)
        result = self.agent.run(
            query,
            tools=[],
            use_skills=False,
            max_retry=2,
            result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        answer = str(result).strip()
        if not answer:
            raise ValueError("empty assistant reply")
        return {"answer": answer[:8000], "model": MODEL}


def validate_assistant_reply(data):
    messages = data.get("messages")
    if not isinstance(messages, list) or not messages or len(messages) > 20:
        raise ValueError("messages must contain 1-20 items")
    out = []
    for message in messages:
        if not isinstance(message, dict):
            raise ValueError("message must be an object")
        role = message.get("role")
        content = message.get("content")
        if role not in ("user", "assistant"):
            raise ValueError("unsupported role")
        if not isinstance(content, str) or not content.strip() or len(content) > 4000:
            raise ValueError("invalid content")
        out.append({"role": role, "content": content})
    return out
