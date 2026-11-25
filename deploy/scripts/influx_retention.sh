#!/usr/bin/env bash
# influx_retention.sh — 把现有 InfluxDB bucket 的保留期更新为 90 天（AR-15）。
# 新部署由 compose 的 DOCKER_INFLUXDB_INIT_RETENTION 生效；本脚本用于已有 bucket。
#
# 用法：
#   INFLUXDB_ORG=lab-org INFLUXDB_BUCKET=lab-bucket INFLUXDB_RETENTION=90d \
#     deploy/scripts/influx_retention.sh
# token 优先取 INFLUX_TOKEN，否则读 deploy/secrets/influxdb_token.txt。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER="${INFLUXDB_CONTAINER:-lab-influxdb}"
BUCKET="${INFLUXDB_BUCKET:-lab-bucket}"
ORG="${INFLUXDB_ORG:-lab-org}"
RETENTION="${INFLUXDB_RETENTION:-90d}"
TOKEN="${INFLUX_TOKEN:-}"

if [ -z "$TOKEN" ] && [ -f "$SCRIPT_DIR/../secrets/influxdb_token.txt" ]; then
  TOKEN="$(tr -d '[:space:]' < "$SCRIPT_DIR/../secrets/influxdb_token.txt")"
fi
if [ -z "$TOKEN" ]; then
  echo "错误：缺少 INFLUX_TOKEN 或 deploy/secrets/influxdb_token.txt" >&2
  exit 1
fi
if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
  echo "错误：容器不存在（$CONTAINER）" >&2
  exit 1
fi

if ! docker exec "$CONTAINER" influx bucket list --org "$ORG" --token "$TOKEN" \
    --name "$BUCKET" --hide-headers 2>/dev/null | grep -q "$BUCKET"; then
  echo "错误：bucket 不存在（$BUCKET / $ORG）" >&2
  exit 1
fi

echo "更新 bucket $BUCKET 保留期 → $RETENTION（org=$ORG）"
docker exec "$CONTAINER" influx bucket update \
  --name "$BUCKET" --org "$ORG" --token "$TOKEN" --retention "$RETENTION"
echo "完成。注意：保留期只影响新写入数据的过期，不会立即删除历史数据。"
