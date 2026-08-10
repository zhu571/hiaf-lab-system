#!/usr/bin/env bash
# scripts/test-py.sh —— py-agent unittest + 覆盖率（P3，测试策略方案 §7）
#
# 流程：定位解释器（优先 py-agent/.venv，无则 python3）→ 缺 coverage 时安装
#       requirements-dev.txt → coverage run -m unittest discover -s tests -v
#       → coverage report -m（输出 total 行 + 各文件明细）。
#
# 设计要点：
#   - 生产依赖 requirements.txt 不含 coverage.py（unittest 无内置覆盖率），
#     覆盖率工具走独立 dev 依赖文件，绝不污染生产镜像。
#   - 每次运行先删旧 .coverage 数据文件，再重新采集，避免跨轮次残留污染报告。
#   - 不设硬阈值（方案 §5），只报告，人工盯。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AGENT_DIR="$REPO_ROOT/py-agent"

PYTHON_BIN=""
if [ -x "$AGENT_DIR/.venv/bin/python" ]; then
    PYTHON_BIN="$AGENT_DIR/.venv/bin/python"
elif command -v python3 >/dev/null 2>&1; then
    PYTHON_BIN="$(command -v python3)"
else
    echo "错误：找不到 python3（py-agent/.venv/bin/python 或系统 python3）" >&2
    exit 1
fi
echo "== 使用解释器：$PYTHON_BIN（$("$PYTHON_BIN" --version)）"

if ! "$PYTHON_BIN" -c "import coverage" >/dev/null 2>&1; then
    echo "== 未安装 coverage.py，安装 requirements-dev.txt（仅测试环境）"
    if ! "$PYTHON_BIN" -m pip install -q -r "$AGENT_DIR/requirements-dev.txt"; then
        echo "错误：安装 coverage.py 失败。" >&2
        echo "      若系统 Python 受 PEP 668（externally-managed-environment）限制，请先建 venv：" >&2
        echo "      python3 -m venv py-agent/.venv && py-agent/.venv/bin/pip install -r py-agent/requirements-dev.txt" >&2
        exit 1
    fi
fi

cd "$AGENT_DIR"
# .coverage 是 coverage 的本地数据文件：运行前清除旧数据，退出时删除避免残留到仓库目录。
trap 'rm -f .coverage' EXIT
rm -f .coverage
echo "== coverage run -m unittest discover -s tests -v"
"$PYTHON_BIN" -m coverage run -m unittest discover -s tests -v

echo
echo "== Python 覆盖率汇总（P3，不设门禁，人工盯下降趋势）"
"$PYTHON_BIN" -m coverage report -m
