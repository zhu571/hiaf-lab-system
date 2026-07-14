# HIAF 实验室日志管理系统

HIAF 低温气体靶实验室的多人协作日志管理平台。

当前仓库处于设计完成、Phase 1 待启动状态。`go-server/`、`web-ui/`、`py-agent/`、`migrations/`、`deploy/` 是目标目录，可能只有占位文件；实现模块时按 `AGENTS.md` 和 `docs/` 下的专题文档推进。

## 技术栈

Go (chi) + Vue 3 (Element Plus) + PostgreSQL + Python (LightAgent)

## 入口文档

| 人类入口：[docs/设计总纲.md](docs/设计总纲.md) |
| AI 编程助手入口：[AGENTS.md](AGENTS.md) |
| Git 工作流与 PR 流程：[CONTRIBUTING.md](CONTRIBUTING.md) |
| 协作流程：[docs/collab-guide.md](docs/collab-guide.md) |
| API 契约：[docs/api-contract.md](docs/api-contract.md) |

## 快速开始

在对应模块真正落地后再运行这些命令：

```bash
# 启动 PostgreSQL
docker run -d --name lab-pg -e POSTGRES_USER=lab -e POSTGRES_PASSWORD=lab -e POSTGRES_DB=lab -p 5432:5432 postgres:16

# 运行迁移
for f in migrations/*.sql; do
  [ -e "$f" ] || { echo "no migrations/*.sql"; break; }
  PGPASSWORD=lab psql -h 127.0.0.1 -U lab -d lab -v ON_ERROR_STOP=1 -f "$f"
done

# 启动 Go 后端
cd go-server && go run .

# 启动前端
cd web-ui && npm ci && npm run dev
```

## CI

GitHub Actions 包含三个 job：Go 后端、Vue 前端、Python Agent。当前设计期如果对应模块还没有实现，job 会明确跳过；一旦提交 `go.mod`、`package.json` 或 Python 源文件，就会执行对应检查。

## 许可证

MIT
