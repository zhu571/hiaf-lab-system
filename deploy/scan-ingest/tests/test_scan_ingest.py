import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import scan_ingest as si


def make_config(tmp: Path, **overrides):
    base = dict(
        watch_dir=tmp,
        project_id="11111111-1111-1111-1111-111111111111",
        token="test-token",
        state_file=tmp / "state.json",
        heartbeat_file=tmp / "heartbeat",
        poll_interval=1,
        stable_age=0,
        png_grace=0,
    )
    base.update(overrides)
    return si.Config(**base)


class FakeResponse:
    status_code = 201
    text = ""


class ParseNameTest(unittest.TestCase):
    def test_valid(self):
        scan_at, tag, source = si.parse_name("gc261125-101500-qpig.txt")
        self.assertEqual(scan_at.year, 2026)
        self.assertEqual(scan_at.hour, 10)
        self.assertEqual(tag, "qpig")
        self.assertEqual(source, "filename")

    def test_empty_tag(self):
        _, tag, _ = si.parse_name("gc261125-101500-.txt")
        self.assertEqual(tag, "untagged")

    def test_bad_name(self):
        self.assertEqual(si.parse_name("bad.txt"), (None, "", "mtime"))


class FormatTest(unittest.TestCase):
    def test_v7_and_legacy(self):
        with tempfile.TemporaryDirectory() as tmp:
            v7 = Path(tmp) / "v7.txt"
            v7.write_text("ref\tpA\tsigma\tVpp1\tVpp2\tVpp3\tVpp4\n", encoding="utf-8")
            legacy = Path(tmp) / "legacy.txt"
            legacy.write_text("1\t2\t3\n", encoding="utf-8")
            self.assertEqual(si.detect_format(v7), "v7")
            self.assertEqual(si.detect_format(legacy), "legacy3")


class StateTest(unittest.TestCase):
    def test_atomic_roundtrip(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = si.State(Path(tmp) / "state.json")
            state.mark("a.txt", status="success")
            state.save()
            loaded = si.State(Path(tmp) / "state.json")
            loaded.load()
            self.assertEqual(loaded.get("a.txt")["status"], "success")

    def test_corrupt_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "state.json"
            path.write_text("{not-json", encoding="utf-8")
            state = si.State(path)
            state.load()
            self.assertEqual(state.files, {})


class IngestTest(unittest.TestCase):
    def test_success_marks_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            txt = tmp_path / "gc261125-101500-qpig.txt"
            txt.write_text("ref\tpA\n0.1\t1\n", encoding="utf-8")
            watcher = si.Watcher(make_config(tmp_path))
            watcher.post_ingest = lambda *args, **kwargs: FakeResponse()
            self.assertTrue(watcher.ingest_file(txt, None))
            state = si.State(tmp_path / "state.json")
            state.load()
            self.assertEqual(state.get(txt.name)["status"], "success")
            self.assertFalse(state.get(txt.name)["has_png"])

    def test_dry_run_does_not_post(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            txt = tmp_path / "gc261125-101500-qpig.txt"
            txt.write_text("ref\tpA\n0.1\t1\n", encoding="utf-8")
            watcher = si.Watcher(make_config(tmp_path, dry_run=True))
            watcher.post_ingest = lambda *args, **kwargs: (_ for _ in ()).throw(AssertionError("must not post"))
            self.assertFalse(watcher.ingest_file(txt, None))


if __name__ == "__main__":
    unittest.main()
