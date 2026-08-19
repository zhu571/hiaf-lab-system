import unittest

from tools.translation import validate_translation
from tools.parse import ParseError


class TranslationValidationTests(unittest.TestCase):
    def test_protected_terms_and_json_shape(self):
        result = validate_translation({"status": "ok", "translated_text": "The E5063A reached 5e-6 Pa."}, ["E5063A", "5e-6 Pa"])
        self.assertEqual(result["status"], "ok")

    def test_missing_term_is_rejected(self):
        with self.assertRaises(ParseError):
            validate_translation({"status": "ok", "translated_text": "The device passed."}, ["E5063A"])


if __name__ == "__main__":
    unittest.main()
