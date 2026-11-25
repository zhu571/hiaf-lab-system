# 安装（gascell 宿主机，root）：
#   useradd --system --home /var/lib/lab-scan-ingest --shell /usr/sbin/nologin labscan
#   install -d -m 700 -o labscan -g labscan /var/lib/lab-scan-ingest /etc/lab-scan-ingest
#   install -d -m 755 /opt/lab-scan-ingest
#   install -m 755 deploy/scan-ingest/scan_ingest.py /opt/lab-scan-ingest/
#   install -m 644 deploy/scan-ingest/requirements.txt /opt/lab-scan-ingest/
#   /usr/bin/python3 -m pip install -r /opt/lab-scan-ingest/requirements.txt
#   install -m 600 -o labscan -g labscan deploy/scan-ingest/config.env.example /etc/lab-scan-ingest/config.env
#   # 编辑 /etc/lab-scan-ingest/config.env：SCAN_PROJECT_ID、SCAN_WATCH_DIR、token 路径
#   install -m 600 -o labscan -g labscan deploy/secrets/service_token.txt /etc/lab-scan-ingest/service_token
#   install -m 644 deploy/scan-ingest/lab-scan-ingest.service /etc/systemd/system/
#   systemctl daemon-reload && systemctl enable --now lab-scan-ingest
#
# 只读验证：确认 NAS 挂载为 ro，且 unit 仅 ReadOnlyPaths=/mnt/mtof。
# 存量导入：
#   sudo -u labscan /usr/bin/python3 /opt/lab-scan-ingest/scan_ingest.py --backfill --dry-run > manifest.json
#   sudo -u labscan /usr/bin/python3 /opt/lab-scan-ingest/scan_ingest.py --backfill
