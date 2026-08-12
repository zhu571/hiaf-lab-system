# hiaf-lab-system 性能基准（2026-08-12）

测试工具：k6 v0.54.0（本地栈，不打生产 gascell）
测试环境：本地机器（go server + postgres:16 容器，SOURCE_GATE_ENABLED=false，LOGIN_RATE_LIMIT_IP_MAX=0 基准条件）
脚本：benchmark/*.js（login VU=5；读场景 VU=20、duration 30s）
种子数据：迁移 009（5 用户 + 低温气体靶项目）

## 结果

| 场景 | 请求数 | QPS | p95 延迟 | 错误率 |
|------|--------|-----|----------|--------|
| login（POST /auth/login） | 663 | 22/s | 274ms | 0% |
| projects list | 50,159 | 1,663/s | 27.5ms | 0% |
| daily-report today | 12,124 | 402/s | 54.4ms | 0% |
| test-data list | 64,333 | 2,132/s | 20.9ms | 0% |
| issues list | 51,797 | 1,720/s | 28.2ms | 0% |

## 分析

1. **读接口性能充裕**：1,600-2,100 QPS、p95 21-55ms——数据库/服务端不是瓶颈。瓶颈只可能在网络带宽。
2. **登录是成本点**：argon2id(time=3, 64MB) 校验单次 ~226ms avg——**22/s 是单进程登录上限**。若未来同事集中登录（如早上一齐打开系统），20 人同时登录约 1 秒队列延迟，可接受；暴力破解防护成本也在此（每尝试 226ms 服务端 CPU）。
3. **4Mbps VPS 带宽预估**：4Mbps ≈ 500KB/s 上行。按典型响应 5-20KB：
   - 单用户页面刷新（首页含 5-10 个请求，~100KB）→ 0.2-0.4s 加载
   - 同时在线 **10-20 人**日常使用（每 5-10s 一个操作）无压力
   - 若做大数据量导出（测试数据批量 1MB+）会占满带宽 ~2s/次——建议大导出走内网或错峰
4. **Caddy brotli 的收益**：前端 JS/CSS 压缩后 ~1.4MB→~300KB（brotli 15%），首屏加载显著受益于 4M 带宽。

## 结论

- 公网开放给全组（预计 5-10 人同时在线）**带宽和性能均无压力**
- 关注点：登录限流已启用（20 次/15min/IP，S1）防暴力破解；ask AI 查询未纳入基准（LLM 外部延迟，与系统性能无关）
- 后续若在线人数 >30 或有大文件需求，再评估 VPS 带宽升级

## 复现

```bash
# 1. 起本地栈（postgres + server，LOGIN_RATE_LIMIT_IP_MAX=0）
# 2. k6（~/.local/bin/k6）
k6 run -e BENCH_BASE_URL=http://127.0.0.1:8000 benchmark/login.js
k6 run -e BENCH_BASE_URL=http://127.0.0.1:8000 benchmark/projects-list.js
# ... 其余场景同理
```
