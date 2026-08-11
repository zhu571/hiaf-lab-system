import json
import os
import stat
import tempfile
import unittest
from pathlib import Path

import cli.auth as auth
from cli.auth import clear_token, load_token, save_token


class TestTokenStore(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self._old = os.environ.get("LABCTL_TOKEN_DIR")
        os.environ["LABCTL_TOKEN_DIR"] = self._tmp.name
        auth.TOKEN_DIR = Path(self._tmp.name)
        auth.TOKEN_FILE = auth.TOKEN_DIR / "token"

    def tearDown(self):
        if self._old is None:
            os.environ.pop("LABCTL_TOKEN_DIR", None)
        else:
            os.environ["LABCTL_TOKEN_DIR"] = self._old
        self._tmp.cleanup()

    def test_save_creates_0600_file_and_0700_dir(self):
        save_token({"access_token": "at", "refresh_token": "rt", "csrf_token": "cs",
                    "username": "zhangsan", "base_url": "http://lab.test"})
        mode = stat.S_IMODE(os.stat(auth.TOKEN_FILE).st_mode)
        self.assertEqual(mode, 0o600)
        dir_mode = stat.S_IMODE(os.stat(auth.TOKEN_DIR).st_mode)
        self.assertEqual(dir_mode, 0o700)

    def test_password_never_persisted(self):
        save_token({"access_token": "at", "username": "zhangsan"})
        content = auth.TOKEN_FILE.read_text(encoding="utf-8")
        self.assertNotIn("password", content.lower())
        data = json.loads(content)
        self.assertNotIn("password", data)

    def test_load_roundtrip(self):
        save_token({"access_token": "at", "refresh_token": "rt", "csrf_token": "cs",
                    "username": "u", "base_url": "http://lab.test"})
        data = load_token()
        self.assertEqual(data["access_token"], "at")
        self.assertEqual(data["refresh_token"], "rt")
        self.assertEqual(data["csrf_token"], "cs")

    def test_load_missing_file_returns_none(self):
        self.assertIsNone(load_token())

    def test_load_corrupt_file_returns_none(self):
        auth.TOKEN_FILE.write_text("{not json", encoding="utf-8")
        self.assertIsNone(load_token())

    def test_load_empty_token_returns_none(self):
        save_token({"username": "u"})
        self.assertIsNone(load_token())

    def test_clear_removes_file(self):
        save_token({"access_token": "at"})
        clear_token()
        self.assertFalse(auth.TOKEN_FILE.exists())
        clear_token()  # 幂等


if __name__ == "__main__":
    unittest.main()
