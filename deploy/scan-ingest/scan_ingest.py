#!/usr/bin/env python3
"""GC 扫描文件 watcher / backfill（scan-archive v0.4 §6）。

- 轮询 SCAN_WATCH_DIR（SMB 上不用 inotify），txt 稳定后才入库；
- 只读源目录，不写/不删/不移动源文件；
- 通过 SERVICE_TOKEN 调 POST /api/v1/scans/ingest；
- state 原子写，断点续跑；--backfill --dry-run 先出 manifest。

环境变量见 config.env.example；本脚本不依赖仓库内路径。
"""

import argparse
import hashlib
import json
import os
import re
import sys
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from zoneinfo import ZoneInfo

import requests

NAME_RE = re.compile(r"^gc(\d{6})-(\d{6})-(.*)\.(txt|png)$")
TAG_RE = re.compile(r"[^A-Za-z0-9._-]+")
CN_TZ = ZoneInfo("Asia/Shanghai")


def env(name, default=""):
    value = os.getenv(name, default)
    return value.strip() if isinstance(value, str) else value


def read_token():
    path = env("SERVICE_TOKEN_FILE")
    if path:
        try:
            return Path(path).read_text(encoding="utf-8").strip()
        except OSError:
            pass
    return env("SERVICE_TOKEN")


@dataclass
class Config:
    watch_dir: Path
    project_id: str
    machine: str = "gascell"
    api_url: str = "http://127.0.0.1:8000"
    token: str = ""
    state_file: Path = Path("/var/lib/lab-scan-ingest/state.json")
    heartbeat_file: Path = Path("/var/lib/lab-scan-ingest/heartbeat")
    poll_interval: float = 15.0
    stable_age: float = 5.0
    png_grace: float = 300.0
    dry_run: bool = False
    max_attempts: int = 5

    @classmethod
    def from_env(cls):
        return cls(
            watch_dir=Path(env("SCAN_WATCH_DIR", "/mnt/mtof/DJ/GC/test")),
            project_id=env("SCAN_PROJECT_ID"),
            machine=env("SCAN_MACHINE", "gascell"),
            api_url=env("LAB_API_URL", "http://127.0.0.1:8000").rstrip("/"),
            token=read_token(),
            state_file=Path(env("STATE_FILE", "/var/lib/lab-scan-ingest/state.json")),
            heartbeat_file=Path(env("HEARTBEAT_FILE", "/var/lib/lab-scan-ingest/heartbeat")),
            poll_interval=float(env("POLL_INTERVAL", "15")),
            stable_age=float(env("STABLE_AGE_SECONDS", "5")),
            png_grace=float(env("PNG_GRACE_SECONDS", "300")),
            dry_run=env("DRY_RUN", "0") in {"1", "true", "yes"},
        )


def parse_name(filename):
    """返回 (scan_at, source_tag, scan_at_source) 或 (None, '', 'mtime')。"""
    match = NAME_RE.match(filename)
    if not match:
        return None, "", "mtime"
    yymmdd, hhmmss, tag, _ = match.groups()
    tag = TAG_RE.sub("", tag).lower() or "untagged"
    try:
        scan_at = datetime.strptime("20" + yymmdd + hhmmss, "%Y%m%d%H%M%S").replace(tzinfo=CN_TZ)
        return scan_at, tag, "filename"
    except ValueError:
        return None, tag, "mtime"


def sha256_file(path):
    digest = hashlib.sha256()
    with open(path, "rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def detect_format(path):
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as fh:
            first = fh.readline().strip()
    except OSError:
        return "unknown"
    if not first:
        return "unknown"
    fields = [field.strip() for field in first.split("\t")]
    numeric = 0
    for field in fields:
        try:
            float(field)
            numeric += 1
        except ValueError:
            pass
    if numeric == len(fields):
        return "legacy3" if len(fields) == 3 else "unknown"
    return {7: "v7", 11: "v11", 12: "v12"}.get(len(fields), "unknown")


class State:
    def __init__(self, path):
        self.path = path
        self.files = {}

    def load(self):
        try:
            data = json.loads(self.path.read_text(encoding="utf-8"))
            if isinstance(data.get("files"), dict):
                self.files = data["files"]
        except FileNotFoundError:
            self.files = {}
        except (OSError, ValueError):
            # 损坏的 state 不阻塞启动：备份后重建，后续会重新入库（服务端去重）。
            try:
                self.path.replace(self.path.with_suffix(".corrupt"))
            except OSError:
                pass
            self.files = {}

    def save(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.path.with_suffix(".tmp")
        tmp.write_text(json.dumps({"version": 1, "files": self.files}, ensure_ascii=False, indent=2),
                       encoding="utf-8")
        os.replace(tmp, self.path)

    def get(self, filename):
        return self.files.get(filename) or {}

    def mark(self, filename, **fields):
        item = self.files.get(filename, {})
        item.update(fields)
        item["updated_at"] = datetime.now(CN_TZ).isoformat()
        self.files[filename] = item


class Watcher:
    def __init__(self, cfg):
        self.cfg = cfg
        self.state = State(cfg.state_file)
        self.samples = {}
        self.failures = 0

    # ---------------- HTTP ----------------
    def post_ingest(self, txt_path, png_path=None, ingest_source="watcher"):
        scan_at, source_tag, scan_at_source = parse_name(txt_path.name)
        if scan_at is None:
            scan_at = datetime.fromtimestamp(txt_path.stat().st_mtime, CN_TZ)
        data = {
            "project_id": self.cfg.project_id,
            "machine": self.cfg.machine,
            "scan_at": scan_at.isoformat(),
            "scan_at_source": scan_at_source,
            "source_tag": source_tag,
            "tags": source_tag,
            "source_path": str(txt_path),
            "ingest_source": ingest_source,
        }
        files = {"txt": (txt_path.name, txt_path.read_bytes(), "text/plain")}
        if png_path is not None:
            files["png"] = (png_path.name, png_path.read_bytes(), "image/png")
        key = "scan-" + sha256_file(txt_path)
        headers = {"Authorization": "Bearer " + self.cfg.token, "Idempotency-Key": key}
        return requests.post(self.cfg.api_url + "/api/v1/scans/ingest",
                             data=data, files=files, headers=headers, timeout=30)

    def alert(self, title, detail, level="error"):
        if not self.cfg.token:
            return
        try:
            requests.post(self.cfg.api_url + "/api/v1/alerts/report",
                          json={"level": level, "source": "scan-ingest", "title": title, "detail": detail},
                          headers={"Authorization": "Bearer " + self.cfg.token}, timeout=10)
        except requests.RequestException:
            pass

    # ---------------- heartbeat ----------------
    def heartbeat(self, pending=0):
        try:
            self.cfg.heartbeat_file.parent.mkdir(parents=True, exist_ok=True)
            payload = {
                "ts": datetime.now(CN_TZ).isoformat(),
                "pending": pending,
                "failures": self.failures,
            }
            self.cfg.heartbeat_file.write_text(json.dumps(payload), encoding="utf-8")
        except OSError:
            pass

    # ---------------- ingest ----------------
    def ingest_file(self, txt_path, png_path, ingest_source="watcher"):
        stat = txt_path.stat()
        png_stat = png_path.stat() if png_path else None
        entry = self.state.get(txt_path.name)
        if entry.get("next_retry_at", 0) > time.time():
            return False
        if entry.get("status") == "success":
            same = entry.get("size") == stat.st_size and entry.get("mtime_ns") == stat.st_mtime_ns
            has_png = entry.get("has_png", False)
            if same and (png_path is None or has_png):
                return False
            if same and png_path is not None and not has_png:
                # png 迟到：重发同一 txt+png，服务端按唯一键补挂。
                pass
            elif same:
                return False
        if self.cfg.dry_run:
            print(json.dumps({"dry_run": True, "filename": txt_path.name, "size": stat.st_size,
                              "png": bool(png_path), "format": detect_format(txt_path)},
                             ensure_ascii=False))
            return False

        try:
            response = self.post_ingest(txt_path, png_path, ingest_source=ingest_source)
        except requests.RequestException as exc:
            self.failures += 1
            attempts = entry.get("attempts", 0) + 1
            self.state.mark(txt_path.name, status="retry", attempts=attempts,
                            last_error=type(exc).__name__,
                            next_retry_at=time.time() + min(300, (2 ** attempts) * self.cfg.poll_interval))
            self.state.save()
            if self.failures >= self.cfg.max_attempts:
                self.alert("扫描入库连续失败", f"{txt_path.name}: {type(exc).__name__}")
                self.failures = 0
            return False

        if response.status_code in (200, 201):
            self.failures = 0
            self.state.mark(txt_path.name, status="success", size=stat.st_size,
                            mtime_ns=stat.st_mtime_ns, sha256=sha256_file(txt_path),
                            has_png=png_path is not None, attempts=0, last_error="", next_retry_at=0)
            self.state.save()
            return True

        detail = response.text[:500]
        self.failures += 1
        attempts = entry.get("attempts", 0) + 1
        self.state.mark(txt_path.name, status="failed", attempts=attempts,
                        last_error=f"HTTP {response.status_code}: {detail}",
                        next_retry_at=time.time() + min(300, (2 ** attempts) * self.cfg.poll_interval))
        self.state.save()
        if 400 <= response.status_code < 500:
            self.alert("扫描入库被拒绝", f"{txt_path.name}: HTTP {response.status_code} {detail}")
        return False

    def poll_once(self):
        if not self.cfg.watch_dir.is_dir():
            self.alert("扫描源目录不可读", str(self.cfg.watch_dir))
            self.heartbeat()
            return 0
        now = time.time()
        pending = 0
        for txt_path in sorted(self.cfg.watch_dir.glob("gc*.txt")):
            try:
                stat = txt_path.stat()
            except OSError:
                continue
            signature = (stat.st_size, stat.st_mtime_ns)
            if self.samples.get(txt_path.name) != signature:
                self.samples[txt_path.name] = signature
                pending += 1
                continue
            if now - stat.st_mtime < self.cfg.stable_age:
                pending += 1
                continue
            png_path = txt_path.with_suffix(".png")
            if png_path.exists():
                try:
                    png_stat = png_path.stat()
                except OSError:
                    continue
                if now - png_stat.st_mtime < self.cfg.stable_age:
                    pending += 1
                    continue
            else:
                png_path = None
                if now - stat.st_mtime < self.cfg.png_grace:
                    pending += 1
                    continue
            self.ingest_file(txt_path, png_path)
        self.heartbeat(pending)
        return pending

    def run_forever(self):
        self.state.load()
        while True:
            try:
                self.poll_once()
            except Exception as exc:  # watcher 不能因单轮异常退出
                self.alert("扫描 watcher 异常", type(exc).__name__)
            time.sleep(self.cfg.poll_interval)

    # ---------------- backfill ----------------
    def backfill(self, dry_run):
        self.state.load()
        if not self.cfg.watch_dir.is_dir():
            print("watch dir not found", file=sys.stderr)
            return 2
        files = sorted(self.cfg.watch_dir.glob("gc*.txt"), key=lambda p: p.stat().st_mtime)
        manifest = []
        for txt_path in files:
            png_path = txt_path.with_suffix(".png")
            png_path = png_path if png_path.exists() else None
            entry = self.state.get(txt_path.name)
            stat = txt_path.stat()
            skip = (entry.get("status") == "success" and entry.get("size") == stat.st_size
                    and entry.get("mtime_ns") == stat.st_mtime_ns)
            manifest.append({
                "filename": txt_path.name,
                "size": stat.st_size,
                "format": detect_format(txt_path),
                "has_png": png_path is not None,
                "already_ingested": skip,
            })
            if dry_run:
                continue
            if skip and png_path is None:
                continue
            if self.ingest_file(txt_path, png_path, ingest_source="backfill"):
                time.sleep(0.2)  # 5 req/s
        print(json.dumps({"count": len(manifest), "files": manifest}, ensure_ascii=False, indent=2))
        return 0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--backfill", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    cfg = Config.from_env()
    if not cfg.project_id:
        print("SCAN_PROJECT_ID is required", file=sys.stderr)
        return 2
    if not cfg.token:
        print("SERVICE_TOKEN_FILE/SERVICE_TOKEN is required", file=sys.stderr)
        return 2
    watcher = Watcher(cfg)
    if args.backfill:
        return watcher.backfill(args.dry_run or cfg.dry_run)
    watcher.run_forever()
    return 0


if __name__ == "__main__":
    sys.exit(main())
