import json
import re
from pathlib import Path

from tools.parse import (
    MODEL,
    BASE_URL,
    ParseError,
    ensure_safe,
    _json_object,
)


class StepPlanner:
    def __init__(self, api_key, prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        prompt_path = prompt_path or Path(__file__).parents[1] / "prompts" / "step_plan.txt"
        self.agent = LightAgent(
            name="step-planner", model=MODEL, base_url=BASE_URL, api_key=api_key,
            instructions=Path(prompt_path).read_text(), tools=[], filter_tools=True, tree_of_thought=False,
            memory=None, self_learning=False, auto_discover_skills=False,
            hooks=[NoToolHook()], debug=False,
        )
        self.agent.tool_registry = ToolRegistry()
        self.agent.loaded_tools = {}

    def plan(self, kind, prompt, context):
        ensure_safe(prompt)
        query = json.dumps({
            "trusted_context": {
                "kind": kind,
                "run_types": context.get("run_types", []),
                "gas_types": context.get("gas_types", []),
                "devices": context.get("devices", []),
                "existing_step_names": context.get("existing_step_names", []),
            },
            "untrusted_inputs": {"user_input": prompt},
        }, ensure_ascii=False)
        result = self.agent.run(
            query, tools=[], use_skills=False, max_retry=2, result_format="str",
            metadata={"extra_body": {"thinking": {"type": "disabled"}}},
        )
        return validate_step_plan(_json_object(str(result)))


def validate_step_plan(item):
    status = item.get("status")
    if status not in {"ok", "clarify", "rejected"}:
        raise ParseError("step plan status is invalid")
    name_suggestion = str(item.get("name_suggestion", "")).strip()
    steps = item.get("steps", [])
    if status == "ok":
        if not name_suggestion or len(name_suggestion) > 256:
            raise ParseError("name_suggestion is invalid")
        if not isinstance(steps, list) or len(steps) < 1 or len(steps) > 30:
            raise ParseError("steps count must be 1-30")
        orders = set()
        for step in steps:
            name = str(step.get("name", "")).strip()
            if not name or len(name) > 256:
                raise ParseError("step name is invalid")
            description = str(step.get("description", "")).strip()
            if len(description) > 2000:
                raise ParseError("step description is too long")
            step_order = step.get("step_order")
            if not isinstance(step_order, int) or step_order <= 0:
                raise ParseError("step_order must be a positive integer")
            if step_order in orders:
                raise ParseError("step_order must be unique")
            orders.add(step_order)
            depends_on = step.get("depends_on_order")
            if depends_on is not None:
                if not isinstance(depends_on, int) or depends_on not in orders or depends_on >= step_order:
                    raise ParseError("depends_on_order must be an earlier step_order")

    if status == "clarify" and not str(item.get("question", "")).strip():
        raise ParseError("clarification question is required")
    if status == "rejected" and not str(item.get("reason", "")).strip():
        raise ParseError("rejection reason is required")

    return {
        "status": status,
        "name_suggestion": name_suggestion if status == "ok" else "",
        "steps": [
            {
                "name": str(s.get("name", "")).strip(),
                "description": str(s.get("description", "")).strip(),
                "step_order": s["step_order"],
                "depends_on_order": s.get("depends_on_order"),
            }
            for s in steps
        ] if status == "ok" else [],
        "question": str(item.get("question", "")).strip() or None,
        "reason": str(item.get("reason", "")).strip() or None,
        "model": MODEL,
        "prompt_version": "1.0",
    }
