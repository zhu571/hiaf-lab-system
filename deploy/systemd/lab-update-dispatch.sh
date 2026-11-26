#!/usr/bin/env bash
# AR-06b：宿主侧更新请求分发器。由 lab-update.path 触发，串行执行请求目录内的 *.json。
# 所有实际更新仍由 lab-update 的 7 步流水线与仓库锁完成。
set -uo pipefail

REQ_DIR="${UPDATE_REQUEST_DIR:-/opt/hiaf-lab-system/.hermes/update-requests}"
PROCESSED="$REQ_DIR/processed"
mkdir -p "$PROCESSED"

shopt -s nullglob
for req in "$REQ_DIR"/*.json; do
  [ -e "$req" ] || continue
  sid="$(basename "$req" .json)"
  if [ -e "$req.cancel" ]; then
    echo "cancel $sid"
    mv -f "$req" "$PROCESSED/$sid.json" 2>/dev/null || rm -f "$req"
    rm -f "$req.cancel"
    continue
  fi
  /usr/local/bin/lab-update --request "$req"
  rc=$?
  mv -f "$req" "$PROCESSED/$sid.json" 2>/dev/null || rm -f "$req"
  rm -f "$req.cancel"
  echo "finished $sid rc=$rc"
done
