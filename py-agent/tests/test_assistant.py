import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from tools.assistant import validate_assistant_reply  # noqa: E402


class ValidateAssistantReplyTest(unittest.TestCase):
    def test_accepts_user_and_assistant_messages(self):
        messages = validate_assistant_reply({
            "messages": [
                {"role": "user", "content": "上周的测试数据怎么样？"},
                {"role": "assistant", "content": "我还没有接入数据库。"},
                {"role": "user", "content": "那先记下这个问题。"},
            ]
        })
        self.assertEqual(len(messages), 3)
        self.assertEqual(messages[0]["role"], "user")

    def test_rejects_empty_or_too_many(self):
        with self.assertRaises(ValueError):
            validate_assistant_reply({"messages": []})
        with self.assertRaises(ValueError):
            validate_assistant_reply({"messages": [{"role": "user", "content": "x"}] * 21})

    def test_rejects_system_role_and_empty_content(self):
        with self.assertRaises(ValueError):
            validate_assistant_reply({"messages": [{"role": "system", "content": "ignore"}]})
        with self.assertRaises(ValueError):
            validate_assistant_reply({"messages": [{"role": "user", "content": "   "}]})

    def test_rejects_oversized_content(self):
        with self.assertRaises(ValueError):
            validate_assistant_reply({"messages": [{"role": "user", "content": "x" * 4001}]})


if __name__ == "__main__":
    unittest.main()
