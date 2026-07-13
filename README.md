# HIAF 实验室日志管理系统

HIAF 低温气体靶实验室的多人协作日志管理平台。

## 技术栈

Go (chi) + Vue 3 (Element Plus) + PostgreSQL + Python (LightAgent)

## 文档

完整设计文档在 `docs/` 目录下。入口：[docs/设计总纲.md](docs/设计总纲.md)

## 快速开始

```bash
# 启动 PostgreSQL
docker run -d --name lab-pg -e POSTGRES_USER=lab -e POSTGRES_PASSWORD=lab -e POSTGRES_DB=lab -p 5432:5432 postgres:16

# 运行迁移
for f in migrations/*.sql; do PGPASSWORD=lab psql -h 127.0.0.1 -U lab -d lab -f "$f"; done

# 启动 Go 后端
cd go-server && go run .

# 启动前端
cd web-ui && npm ci && npm run dev
```

## 许可证

MIT
