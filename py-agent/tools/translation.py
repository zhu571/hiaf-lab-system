import json
from pathlib import Path

from tools.parse import BASE_URL, MODEL, ParseError, ensure_safe

LOCALES = {"zh", "en"}
SOURCE_LOCALES = {"zh", "en", "mixed", "und"}
FIELDS = {"log.content", "daily_report.raw_text", "daily_report.summary"}


class Translator:
    def __init__(self, api_key, prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("translation must not use tools or thinking")
                return HookDecision.continue_()

        prompt = prompt_path or Path(__file__).parents[1] / "prompts" / "translate.txt"
        self.agent = LightAgent(name="content-translator", model=MODEL, base_url=BASE_URL, api_key=api_key,
                                instructions=Path(prompt).read_text(), tools=[], filter_tools=True,
                                tree_of_thought=False, memory=None, self_learning=False,
                                auto_discover_skills=False, hooks=[NoToolHook()], debug=False)
        self.agent.tool_registry = ToolRegistry()
        self.agent.loaded_tools = {}

    def translate(self, source_text, source_locale, target_locale, field, protected_terms):
        ensure_safe(source_text)
        query = json.dumps({"trusted_context": {"source_locale": source_locale,
            "target_locale": target_locale, "field": field, "protected_terms": protected_terms},
            "untrusted_inputs": {"source_text": source_text}}, ensure_ascii=False)
        result = self.agent.run(query, tools=[], use_skills=False, max_retry=1, result_format="str",
                                metadata={"extra_body": {"thinking": {"type": "disabled"}}})
        return validate_translation(_json_object(str(result)), protected_terms)


def validate_translation(item, protected_terms):
    if item.get("status") != "ok":
        raise ParseError("translation status is invalid")
    text = item.get("translated_text")
    if not isinstance(text, str) or not 1 <= len(text.strip()) <= 4000:
        raise ParseError("translation text is invalid")
    for term in protected_terms:
        if not isinstance(term, str) or term not in text:
            raise ParseError("protected term is missing")
    return {"status": "ok", "translated_text": text.strip(), "model": MODEL, "prompt_version": "1.0"}


def _json_object(text):
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end < start:
        raise ParseError("model did not return a JSON object")
    try:
        value = json.loads(text[start:end + 1])
    except json.JSONDecodeError as exc:
        raise ParseError("model returned invalid JSON") from exc
    if not isinstance(value, dict):
        raise ParseError("model output must be an object")
    return value
