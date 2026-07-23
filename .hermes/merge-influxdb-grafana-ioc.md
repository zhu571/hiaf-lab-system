# 合并 influxdb / grafana / ioc 到 hiaf-lab-system

## 变更

### 1. 新建 `py-agent/ioc/Dockerfile`

```dockerfile
FROM docker.m.daocloud.io/library/python:3.11-slim

RUN pip install -i https://pypi.tuna.tsinghua.edu.cn/simple \
    caproto asyncua aiosqlite influxdb-client

WORKDIR /app
COPY hiaf_config.py hiaf_storage.py hiaf_ioc_final.py ./

EXPOSE 5064
CMD ["python", "hiaf_ioc_final.py"]
```

### 2. 改 `deploy/docker-compose.yml` — 加 3 个 service + 1 volume

```yaml
influxdb:
  image: docker.m.daocloud.io/influxdb:2-alpine
  container_name: lab-influxdb
  restart: unless-stopped
  ports:
    - "8086:8086"
  volumes:
    - /var/lib/influxdb:/var/lib/influxdb2
    - influxdb_config:/etc/influxdb2
  environment:
    DOCKER_INFLUXDB_INIT_MODE: setup
    DOCKER_INFLUXDB_INIT_USERNAME: admin
    DOCKER_INFLUXDB_INIT_PASSWORD: ${INFLUX_PASSWORD}
    DOCKER_INFLUXDB_INIT_ORG: ${INFLUXDB_ORG:-lab-org}
    DOCKER_INFLUXDB_INIT_BUCKET: ${INFLUXDB_BUCKET:-lab-bucket}
    DOCKER_INFLUXDB_INIT_ADMIN_TOKEN: ${INFLUX_TOKEN}
  healthcheck:
    test: ["CMD-SHELL", "influx ping || exit 1"]
    interval: 10s
    timeout: 5s
    retries: 5

grafana:
  image: docker.m.daocloud.io/grafana/grafana:latest
  container_name: lab-grafana
  restart: unless-stopped
  ports:
    - "3000:3000"
  volumes:
    - /opt/lab-monitor/grafana:/var/lib/grafana
  depends_on:
    influxdb:
      condition: service_healthy

ioc:
  build: ../py-agent/ioc
  container_name: lab-ioc
  restart: unless-stopped
  ports:
    - "5064:5064"
  environment:
    EPICS_CAS_INTF_ADDR_LIST: 0.0.0.0
    EPICS_CAS_SERVER_PORT: "5064"
    PYTHONUNBUFFERED: "1"
    OPC_URL: opc.tcp://10.51.12.158:4862
    INFLUX_URL: http://influxdb:8086
    INFLUX_TOKEN_FILE: /run/secrets/influxdb_token   # ← secrets 模式，匹配 _read_secret()
    INFLUX_ORG: ${INFLUXDB_ORG:-lab-org}
    INFLUX_BUCKET: ${INFLUXDB_BUCKET:-lab-bucket}
  secrets:
    - influxdb_token
  depends_on:
    influxdb:
      condition: service_healthy

volumes:
  influxdb_config:

secrets:
  influxdb_token:
    file: ./secrets/influxdb_token.txt
```

### 已知问题（暂不改代码）

- `hiaf_config.py:201` SQLite 路径硬编码 `~/work/hiaf-plc-agent/sensor_history.db`，容器内 `/root` 无 volume mount → 重启丢数据。Ponytail: 不影响核心功能（InfluxDB 是主存储），需要时加 volume mount。

## InfluxDB 数据

**不丢。** `/var/lib/influxdb` 是 host bind mount，新容器直接复用。

## 部署步骤

```bash
# gascell 上
cd /opt/lab-monitor && docker compose down influxdb grafana ioc
cd /opt/hiaf-lab-system && git pull && cd deploy
# 确认 .env 有 INFLUX_PASSWORD 和 INFLUX_TOKEN
docker compose up -d --build influxdb grafana ioc
```
