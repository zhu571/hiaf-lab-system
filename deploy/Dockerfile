# Stage 0: ntfy CLI（版本必须与 compose 中 ntfy 服务镜像一致——auth-file schema 兼容性）
FROM binwiederhier/ntfy:v2.27.0 AS ntfy-cli

# Stage 1: build frontend
FROM node:20-alpine AS frontend-builder

WORKDIR /web-ui
COPY web-ui/package.json web-ui/package-lock.json ./
RUN npm ci
COPY web-ui/ ./
RUN npm run build

# Stage 2: build Go server
FROM golang:1.22-alpine AS builder

ENV GOPROXY=https://goproxy.cn,https://goproxy.io,direct
ENV GOSUMDB=sum.golang.google.cn

WORKDIR /app

COPY go-server/go.mod go-server/go.sum ./
RUN go mod download

COPY go-server/ ./
COPY --from=frontend-builder /web-ui/dist ./static/
RUN CGO_ENABLED=0 go build -o /lab-server .
RUN CGO_ENABLED=0 go build -o /lab-update ./cmd/update-runner

FROM alpine:3.20

# docker-cli-compose: runner 容器内执行 docker compose；coreutils: update.sh 使用 stdbuf。
# openssh-client: git 走 SSH 远程（git@github.com）必须有 ssh 二进制，git 包不携带它
# （2026-08-20 gascell 事故根因之一：fetch 报 cannot run ssh）。
RUN apk add --no-cache ca-certificates tzdata docker-cli docker-cli-compose git curl bash coreutils openssh-client

# 容器内以 UID 1000 运行、无 HOME，写不了 global git config；
# 仓库属主与容器 UID 不一致会触发 git dubious ownership，构建期写入 system 级配置。
RUN git config --system --add safe.directory '*'

COPY --from=builder /lab-server /lab-server
COPY --from=builder /lab-update /usr/local/bin/lab-update
# 前端产物 builder 阶段已构建到 /app/static（在 builder 镜像的 /app 下），
# 必须随最终镜像一起拷出，否则 server 容器 /app/static 为空（R9 健康检查
# "前端产物为空/白屏风险" 即因此触发；2026-08-24 实测 server 容器 static 缺失）。
COPY --from=builder /app/static /app/static
# ntfy CLI：跨容器直写共享 auth-file（账号/ACL 管理），镜像锁定与 ntfy 服务同版本。
COPY --from=ntfy-cli /usr/bin/ntfy /usr/local/bin/ntfy
# 打包自检：两个二进制 + ntfy CLI 必须已就位且可执行。
RUN test -x /lab-server && test -x /usr/local/bin/lab-update && test -x /usr/local/bin/ntfy && ntfy --version

# 非 root 运行时 cwd 不能是 /（UID 1000 无法在 / 下建目录，
# 例如 attachments 默认 ATTACHMENT_DIR=./uploads 会落到 /uploads 并失败）。
WORKDIR /app
RUN mkdir -p /app/uploads && chown 1000:1000 /app/uploads

# passwd 必须有 UID 1000 条目：git/ssh 启动时解析当前用户家目录走 getpwuid，
# 无条目时报 "No user exists for uid 1000" 直接失败（2026-08-24 实测：
# gitLsRemote/预检在此容器内全部因此失败）。HOME 由 compose 注入 runner-home。
RUN addgroup -g 1000 gascell && adduser -D -u 1000 -G gascell -h /home/gascell gascell \
    && mkdir -p /home/gascell && chown 1000:1000 /home/gascell

# 非 root 运行；UID/GID 由 compose 的 UPDATE_RUN_UID/UPDATE_RUN_GID 覆盖，
# docker.sock 访问依赖 compose 的 group_add（UPDATE_DOCKER_GID）。
USER 1000:1000

EXPOSE 8000

CMD ["/lab-server"]
