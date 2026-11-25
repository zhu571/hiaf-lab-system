#!/usr/bin/env bash
# check-doc-stats.sh — 防止 AGENTS.md 的视图/迁移数字漂移（AR-18）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

views="$(find web-ui/src/views -maxdepth 1 -type f -name '*.vue' | wc -l | tr -d ' ')"
migrations="$(find migrations -maxdepth 1 -type f -name '*.up.sql' | wc -l | tr -d ' ')"
failed=0

if ! grep -Eq "web-ui/.*${views} 个业务视图" AGENTS.md; then
  echo "AGENTS.md 的 web-ui 视图数不是 ${views}（实际 views 目录）" >&2
  failed=1
fi
if ! grep -Eq "src/views/.*${views} 个业务视图" AGENTS.md; then
  echo "AGENTS.md 目录树的 src/views 视图数不是 ${views}" >&2
  failed=1
fi
if ! grep -Eq "migrations/.*${migrations} 个版本" AGENTS.md; then
  echo "AGENTS.md 的迁移版本数不是 ${migrations}（实际 *.up.sql）" >&2
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  echo "请同步更新 AGENTS.md 后再提交。" >&2
  exit 1
fi
echo "doc stats OK: views=${views}, migrations=${migrations}"
