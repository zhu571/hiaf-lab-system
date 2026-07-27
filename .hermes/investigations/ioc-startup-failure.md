# IOC 启动钩子"未执行"排查报告

日期：2026-07-27
对象：`py-agent/ioc/hiaf_ioc_final.py`（容器 `lab-ioc`，compose 服务 `ioc`，build context `../py-agent/ioc`）

## 结论（TL;DR）

**startup 钩子大概率已经被正常调用，后台循环也都在运行——真正坏的是 logging。**

`hiaf_ioc_final.py:38-67` 的日志配置时序触发了 `logging.config.dictConfig` 的默认行为
`disable_existing_loggers=True`：模块级 `LOGGER`（第 38 行创建）在 `dictConfig`（第 67 行）
执行时已存在，而配置里没有 `loggers:` 段显式保留它，于是 `LOGGER.disabled` 被置为 `True`。
此后该 logger 的所有 `info/warning/error/debug` 调用被**静默丢弃**——包括
"HiafGasCellIOC starting up..."、"Sensor poll loop started"、"PI control loop started"、
"OPC UA connected — ..." 在内的全部 IOC 自定义日志。

caproto 自身的启动日志（"Asyncio server starting up..."、"Listening on 0.0.0.0:5064"）
来自 `caproto.ctx` logger，它在 `run()` 期间（即 dictConfig **之后**）才被实例化，
因此没有被禁用——这正好解释了"caproto 日志可见、自定义日志全灭"的症状组合。

## 证据链

1. **部署链路正确**：`deploy/docker-compose.yml:82-83` 中 `ioc` 服务 build 自 `../py-agent/ioc`，
   `py-agent/ioc/Dockerfile` 的 CMD 是 `python hiaf_ioc_final.py`。容器跑的就是这份代码，
   不是仓库根目录的旧版 `ioc/hiaf_ioc.py`（旧版用裸 `logging`，无此 bug）。

2. **caproto 机制验证**（本地装 caproto 1.3.0 读源码，与 Dockerfile 不锁定版本会装到的一致）：
   - `@pv.startup` 装饰器把钩子存进 PVSpec（`caproto/server/server.py:1217`）；
   - PVGroup 实例化时给 ChannelData 挂上 `server_startup`（`server.py:180-184`）；
   - `Context.startup_methods` 通过 `_find_hook_methods("server_startup", ...)` 收集
     （`caproto/server/common.py:1147-1159`）；
   - `Context.run()` 逐个 `tasks.create(method(async_lib))`（`caproto/asyncio/server.py:192-194`）。
   - 所以 **`run(ioc.pvdb)` 不需要任何额外参数就会触发 startup 钩子**（`startup_hook=` 参数
     是另一回事，那是给整server一个额外钩子的）。

3. **端到端复现**（`.hermes/investigations/caproto-test/test_full_repro.py`）：
   完全复刻生产结构——同样的 dictConfig、`@Piezo_Running.startup/putter/shutdown` 链式装饰、
   `run(pvdb)`。结果：

   ```text
   PVDB: ['TEST:Piezo:Running']
   module LOGGER.disabled = True
   2026-07-27 15:30:39,561 INFO caproto.ctx: Asyncio server starting up...
   2026-07-27 15:30:39,561 INFO caproto.ctx: Listening on 0.0.0.0:5064
   2026-07-27 15:30:39,561 INFO caproto.ctx: Server startup complete.
   STARTUP-HOOK-ACTUALLY-FIRED        ← print 输出，证明钩子确实执行了
   ```

   `LOGGER.info("HiafGasCellIOC starting up...")` 一行都没出现，但 marker 文件证明钩子被调用。
   与生产症状逐条吻合。

4. **纯 logging 复现**（`.hermes/investigations/caproto-test/test_logging_swallow.py`）：
   相同时序下 `LOGGER.disabled = True`，`isEnabledFor(INFO/WARNING) = False`，
   INFO/WARNING/ERROR 全部不可见；dictConfig 之后新建的 logger 不受影响。

5. **修复验证**：配置中加 `"disable_existing_loggers": False` 后，同一时序下
   `LOGGER.disabled = False`，日志恢复输出。

## 逐条回答排查问题

### 1. `@Piezo_Running.startup` 的触发条件？为什么 PV 已创建但 startup "不执行"？

触发链：PVGroup 实例化（挂 `server_startup`）→ `run(pvdb)` → `Context.run()` 里
`tasks.create(method(async_lib))`。**只要进程正常进入 `run()` 就会触发**，无其他前置条件。
它确实执行了——"不执行"的表象来自日志被吞。佐证就在现有观察里：

- **OPC UA 连接成功**：只有 startup 钩子启动的后台循环会调 `_ensure_connected()`
  （`_sensor_poll_loop` 在 `hiaf_ioc_final.py:548`、`_pi_control_loop` 同理）。
  钩子没跑就不可能有 OPC UA 连接。
- **docker 健康检查通过**：healthcheck 打的是 `:5080/health`，而 5080 的 health server
  也是这个 startup 钩子里 `asyncio.create_task(self._run_health_server())` 拉起的
  （`hiaf_ioc_final.py:932`）。钩子没跑，5080 根本不会有进程监听，容器会 unhealthy。

现场一行命令即可终局确认（在部署机上）：

```bash
curl -s http://localhost:5080/health
# 返回 {"status":"ok","opc_ua":true,"caproto":true} ⇒ startup 钩子 100% 执行过
```

### 2. Piezo_Running 定义（241 行）与 startup 函数（908 行）

两者都没有问题。`pvproperty` 的 `startup/putter/shutdown` 装饰器都返回同一个
pvproperty 对象（pvspec 就地更新），类体里 908（startup）→ 947（putter）→ 1016（shutdown）
的链式重绑定是 caproto 官方示例的标准写法，test_full_repro.py 按原样复刻后钩子正常触发。
签名 `(self, instance, async_lib)` 也通过了 caproto 的 `check_signature` 校验
（否则类定义时就抛异常，进程根本起不来，与"caproto 正常监听 5064"矛盾）。

### 3. 是否有异常在初始化过程中被静默吞掉？

没有"静默"路径：

- `__init__`（含 `HiafStorage` 创建）在 `run()` 之前同步执行，抛异常 = 进程崩溃 →
  docker restart 循环，`docker ps` 能看到，与"容器运行正常"矛盾。
- startup 钩子或后台任务若抛异常，`Context.run()` 末尾的 `asyncio.gather(*tasks.tasks)`
  会把异常抛出来 → `run()` 失败 → 进程退出。同样不静默。
- 真正的"静默吞没"只有 logging 这一处。另外文件里有若干 `except Exception: pass`
  （如 599、619、663 行），它们的告警日志本来会经 `LOGGER` 输出——现在同样被吞。

**附带风险（建议在修复时一并知悉）**：所有 WARNING/ERROR 级日志同样不可见，包括
"A5 TRIP: ... — valve closed"（652 行）、"Valve write failed"（976 行）、
"Sensor poll cycle error"（666 行）等安全相关告警。当前状态下容器日志对故障排查是
全盲的（ntfy 通知不走 logging，不受影响）。

### 4. `_sensor_poll_loop` / `_pi_control_loop` 是否因其他原因没启动？

不是。它们由 startup 钩子里的 `asyncio.create_task` 拉起（917、937 行），
钩子执行它们就执行。OPC UA 连上了、5080 健康检查过了，就是它们在跑的直接证据。
两个循环内部都有逐周期 `try/except`（如 665-667 行），单周期出错会 warning 后继续——
只是这些 warning 现在也被吞了。

## 修复建议

一行改动，`py-agent/ioc/hiaf_ioc_final.py:41` 的 `_LOGGING_CONFIG` 加键：

```python
_LOGGING_CONFIG: dict = {
    "version": 1,
    "disable_existing_loggers": False,   # ← 新增：否则 __main__ 等既有 logger 全部被禁用
    "formatters": { ... },
    ...
}
```

不推荐用"把第 38 行 `LOGGER = logging.getLogger(__name__)` 挪到 dictConfig 之后"来修：
那样 `hiaf_storage.py:20` 的模块 LOGGER（import 时创建，同样在 dictConfig 之前）
仍然被禁用，storage 层的日志还是丢。`disable_existing_loggers: False` 一处修好全部。

改完重新 build 镜像（`docker compose -f deploy/docker-compose.yml up -d --build ioc`），
启动日志里应立即出现 "HiafGasCellIOC starting up..." 等 6 条启动消息。

## 备注

- 排查用脚本保留在 `.hermes/investigations/caproto-test/`（venv 已删，可用
  `python3 -m venv .venv && .venv/bin/pip install caproto` 重建后重跑）。
- Dockerfile 中 caproto 未锁定版本（`pip install caproto`），本次按最新 1.3.0 验证；
  startup_methods 机制在 caproto 1.x 全系列一致存在，版本差异不影响本结论。
