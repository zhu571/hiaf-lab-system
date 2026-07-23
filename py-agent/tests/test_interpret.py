import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from starlette.testclient import TestClient  # noqa: E402
from tools.parse import ParseError, validate_interpretation  # noqa: E402
from serve import create_app, validate_request  # noqa: E402


class FakeInterpreter:
    def interpret(self, *_args):
        return {"status": "ok", "command": "identify", "params": {}, "confidence": 1}


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

    def test_http_endpoint_requires_internal_token(self):
        client = TestClient(create_app(FakeInterpreter(), "secret"))
        body = {
            "instrument_id": "e5063a", "instrument_name": "E5063A",
            "user_input": "读取标识", "history": [],
            "whitelist_commands": [{"name": "identify"}],
        }
        self.assertEqual(client.post("/v1/interpret", json=body).status_code, 401)
        response = client.post("/v1/interpret", json=body, headers={"Authorization": "Bearer secret"})
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["command"], "identify")


if __name__ == "__main__":
    unittest.main()
