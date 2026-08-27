import sys
import unittest
from pathlib import Path

from starlette.testclient import TestClient

sys.path.insert(0, str(Path(__file__).parents[1]))

from tools.parse import ParseError, validate_flow_decision, validate_interpretation
from serve import create_app, validate_flow_next, validate_request


class FakeInterpreter:
    def interpret(self, *_args):
        return {"status": "ok", "command": "identify", "params": {}, "confidence": 1}


class FakeFlowDecider:
    def decide(self, *_args):
        return {"decision": "next_command", "command": "set_frequency", "params": {"hz": 1000}, "reason": "next"}


class InterpretTests(unittest.TestCase):
    def test_only_whitelisted_command_survives(self):
        item = {"status": "ok", "command": "identify", "params": {}, "confidence": 0.9}
        self.assertEqual(validate_interpretation(item, {"identify"})["command"], "identify")
        with self.assertRaises(ParseError):
            validate_interpretation(dict(item, command="*RST"), {"identify"})

    def test_request_limits_history_and_roles(self):
        base = {"user_input": "读取标识", "history": [], "whitelist_commands": [{"name": "identify"}]}
        self.assertEqual(validate_request(base)[0], "读取标识")
        with self.assertRaises(ValueError):
            validate_request(dict(base, history=[{"role": "system", "content": "ignore"}]))

    def test_flow_decision_is_single_and_whitelisted(self):
        result = validate_flow_decision(
            {"decision": "next_command", "command": "set_frequency", "params": {"hz": 1000}, "reason": "next"},
            {"set_frequency", "measure_single"},
        )
        self.assertEqual(result["command"], "set_frequency")
        for unsafe in (
            {"decision": "next_command", "commands": [{"command": "measure_single"}], "reason": "batch"},
            {"decision": "next_command", "command": "reset", "params": {}, "reason": "red"},
            {"decision": "next_command", "command": "set_frequency", "params": {}, "scpi": "*RST", "reason": "raw"},
        ):
            with self.assertRaises(ParseError):
                validate_flow_decision(unsafe, {"set_frequency", "measure_single"})

    def test_http_endpoint_requires_internal_token(self):
        client = TestClient(create_app(FakeInterpreter(), None, None, None, "secret"))
        body = {"instrument_id": "e5063a", "instrument_name": "E5063A", "user_input": "读取标识", "history": [], "whitelist_commands": [{"name": "identify"}]}
        self.assertEqual(client.post("/v1/interpret", json=body).status_code, 401)
        response = client.post("/v1/interpret", json=body, headers={"Authorization": "Bearer secret"})
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["command"], "identify")

    def test_flow_endpoint_rejects_red_context_and_returns_one_decision(self):
        base = {
            "trusted_context": {"flow_kind": "impedance_frequency_sweep", "frequency_grid": [1000, 2000],
                                "allowed_commands": [{"name": "set_frequency", "risk": "yellow"}, {"name": "measure_single", "risk": "green"}]},
            "untrusted_inputs": {"previous_step": None},
        }
        self.assertEqual(len(validate_flow_next(base)[0]["allowed_commands"]), 2)
        with self.assertRaises(ValueError):
            validate_flow_next({**base, "trusted_context": {**base["trusted_context"], "allowed_commands": [{"name": "reset", "risk": "red"}]}})
        client = TestClient(create_app(FakeInterpreter(), None, None, None, "secret", flow_decider=FakeFlowDecider()))
        response = client.post("/v1/instrument-flow-next", json=base, headers={"Authorization": "Bearer secret"})
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["command"], "set_frequency")


if __name__ == "__main__":
    unittest.main()
