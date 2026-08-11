import json
import os
import tempfile
from pathlib import Path

TOKEN_DIR = Path(os.getenv("LABCTL_TOKEN_DIR", str(Path.home() / ".labctl")))
TOKEN_FILE = TOKEN_DIR / "token"


def save_token(data):
    """原子写 token 文件：目录 0700、文件 0600；密码不落盘。"""
    TOKEN_DIR.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=str(TOKEN_DIR), prefix=".token-")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
        os.chmod(tmp, 0o600)
        os.replace(tmp, TOKEN_FILE)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


def load_token():
    """读取 token；文件缺失/损坏/无 access_token 时返回 None（视为未登录）。"""
    if not TOKEN_FILE.exists():
        return None
    try:
        data = json.loads(TOKEN_FILE.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict) or not data.get("access_token"):
        return None
    return data


def clear_token():
    try:
        TOKEN_FILE.unlink()
    except FileNotFoundError:
        pass
