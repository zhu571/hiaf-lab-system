#!/usr/bin/env bash
# scripts/test-migrations.sh —— 迁移 up/down 回滚验证（按迁移粒度）
#
# 用途：验证 migrations/ 下全部迁移（当前 001-038）可以「全量 up → 反序 down」完整回滚，
#      任何一步失败即退出非零并打印失败迁移号。从 P2 提前到 P1（测试策略方案 §4.3.2）。
#
# 流程：
#   1. 起临时 postgres:16-alpine 容器（不映射宿主端口，全程 docker exec，仅依赖 docker）；
#   2. 等待 pg_isready；
#   3. 全量 up（按文件名序号 001→036，glob 字典序天然有序）；
#   4. 仅当全部 up 成功 → 反序 down（036→001）；
#   5. 每步按迁移粒度输出 [OK]/[FAIL]；任一失败 → 非零退出并列出失败迁移号。
#
# 设计要点：
#   - 绝不做「逐对 up→down」：001.down 会 DROP users 表而 002.up 依赖它，逐对必炸。
#     正确语义是先全量 up 验证正向建库，再反序 down 验证逐层回滚。
#   - 重点盯的复杂 down：029（触发器/函数/列拆除）、031（重建触发器 + ADD CONSTRAINT）、
#     032（重建 trg_submit_enqueue_agent_task 触发器函数）、034（UPDATE 数据回写 + SET NOT NULL）、
#     009（多表级联 DELETE）；13 个单语句 DROP（002/010/013/015/018/019/020/022/023/025/026/027/035）
#     价值低但同样覆盖。
#   - 失败不中断：继续跑完剩余步骤，收集全部失败迁移号后统一报告（便于一次抓全坏 down）。
#   - 可重复执行：容器名带 $$ 唯一化，trap EXIT 清理；残留容器用 docker rm -f 兜底。

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v docker >/dev/null 2>&1; then
    echo "错误：需要 docker（起临时 postgres 容器）" >&2
    exit 1
fi

MIG_DIR="$REPO_ROOT/migrations"
mapfile -t UP_FILES < <(ls "$MIG_DIR"/*.up.sql | sort)
# sort -r：零填充序号下等价反序（036→001），避免依赖 tac（macOS 无此命令）
mapfile -t DOWN_FILES < <(ls "$MIG_DIR"/*.down.sql | sort -r)

if [ "${#UP_FILES[@]}" -ne "${#DOWN_FILES[@]}" ] || [ "${#UP_FILES[@]}" -eq 0 ]; then
    echo "错误：migrations 目录不完整（up/down 数量不一致或为空）：${#UP_FILES[@]} up / ${#DOWN_FILES[@]} down" >&2
    exit 1
fi

CID=""
cleanup() {
    if [ -n "$CID" ] && docker ps -q --filter "id=$CID" | grep -q .; then
        docker rm -f "$CID" >/dev/null 2>&1 || true
        echo "== 临时容器已清理"
    fi
}
trap cleanup EXIT

echo "== 启动临时 postgres:16-alpine（容器名 hiaf-mig-test-$$，退出自动清理）"
CID="$(docker run -d --name "hiaf-mig-test-$$" \
    -e POSTGRES_USER=lab -e POSTGRES_PASSWORD=lab -e POSTGRES_DB=lab_test \
    postgres:16-alpine)"
[ -n "$CID" ]

echo "== 等待数据库就绪（pg_isready 需连续 2 次通过，最长 40s）"
# postgres:16-alpine entrypoint 会先启临时 server 执行 setup SQL 再停掉正式启动，
# pg_isready 若打中临时 server 存活窗口，001 迁移会撞上 shutting down 全挂，
# 故要求连续 2 次通过（中途失败计数清零重来），只有正式 server 稳定后才放行
tries=40
hits=0
until [ "$hits" -ge 2 ]; do
    if docker exec -i "$CID" pg_isready -U lab -d lab_test -q >/dev/null 2>&1; then
        hits=$((hits + 1))
    else
        hits=0
    fi
    tries=$((tries - 1))
    if [ "$tries" -le 0 ]; then
        echo "错误：pg_isready 超时（40s 内未能连续 2 次通过）" >&2
        exit 1
    fi
    sleep 1
done

FAILED_UP=()
FAILED_DOWN=()
num=0
total=$(( ${#UP_FILES[@]} ))

# psql 统一执行入口：容器不挂载工作区，必须用 stdin 管道喂 SQL（禁止 -f 宿主路径）
run_sql() {
    local file="$1"
    docker exec -i "$CID" psql -U lab -d lab_test -v ON_ERROR_STOP=1 -q < "$file"
}

echo "== 阶段 1/2：全量 up（按序 001→036）"
for f in "${UP_FILES[@]}"; do
    num=$((num + 1))
    name="$(basename "$f" .up.sql)"
    if run_sql "$f"; then
        printf '[OK] %s up (%d/%d)\n' "$name" "$num" "$total"
    else
        printf '[FAIL] %s up —— 迁移文件: %s\n' "$name" "$(basename "$f")"
        FAILED_UP+=("$name up")
    fi
done

if [ "${#FAILED_UP[@]}" -ne 0 ]; then
    echo
    echo "== 失败汇总（up 阶段）：${#FAILED_UP[@]} 个"
    printf '   - %s\n' "${FAILED_UP[@]}"
    echo "== 由于 up 存在失败，跳过 down 阶段（先修 up 再验回滚）"
    exit 1
fi

echo "== 阶段 2/2：反序 down（036→001）"
for f in "${DOWN_FILES[@]}"; do
    name="$(basename "$f" .down.sql)"
    if run_sql "$f"; then
        printf '[OK] %s down\n' "$name"
    else
        printf '[FAIL] %s down —— 迁移文件: %s\n' "$name" "$(basename "$f")"
        FAILED_DOWN+=("$name down")
    fi
done

if [ "${#FAILED_DOWN[@]}" -ne 0 ]; then
    echo
    echo "== 失败汇总（down 阶段）：${#FAILED_DOWN[@]} 个"
    printf '   - %s\n' "${FAILED_DOWN[@]}"
    echo "== 全量 up 通过，但以下 down 迁移无法回滚（坏 down 通常不影响生产 up，需人工修复）"
    exit 1
fi

echo
echo "== 全部通过：${total} 个迁移 up + ${total} 个迁移 down 均可执行"
