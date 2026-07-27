# IOC / OPC UA 排查结论

> 基于 `.hermes/investigations/ocp-ua-data.md` 的诊断数据，排查代码：
> `py-agent/ioc/hiaf_ioc_final.py`、`go-server/instruments/{service,handler}.go`、
> `go-server/epics-gateway/epics-gateway.py`、`deploy/docker-compose.yml`。

## 根因（一句话）

**`_ensure_connected()` 在 `hiaf_ioc_final.py:391` 有定义，但全文件没有任何调用点。**
它是唯一创建 OPC UA 客户端的函数（`Client(OPC_URL)` + `connect()`），从未执行 →
`self._opc` 永远是 `None`。三个症状（unhealthy、硬件无反应、无日志）都是这一个根因的不同表现。

## 问题 1：OPC UA 客户端为什么没连上（self._opc is None）

`_ensure_connected()` 的定义存在（`py-agent/ioc/hiaf_ioc_final.py:391`），但 grep 全文件
仅命中定义行，**零调用点**。本应调用它的各处全部只检查、不重连：

- 启动钩子 `Piezo_Running.startup`（`hiaf_ioc_final.py:906-941`）：docstring 写着
  "connect OPC UA, launch all background tasks"，但实际只启动了 5 个后台任务
  （sensor poll / sub consumer / heartbeat / health server / PI loop），**没有建连调用**。
- `_sensor_poll_loop`（`hiaf_ioc_final.py:547-549`）：
  `if self._opc is None or self._valve_node is None: sleep; continue` —— 死等，不重连。
- `_pi_control_loop`（`hiaf_ioc_final.py:689-691`）：
  `if self._opc is None: warning; continue` —— 同样不重连。
- `_heartbeat_check` / `_maybe_recover_subscription` 依赖 `_subscription_healthy` 曾为 True
  且 `_last_callback_ts[0] > 0`，两者初始都不满足，订阅恢复路径也永远触发不了。

### Git 考古：这是第二次出现的同一个回归

- `042d67b`（丢失前的版本）在 `_sensor_poll_loop` 中有正确的调用模式：

  ```python
  if self._opc is None or self._valve_node is None:
      await self._ensure_connected()
      if self._opc is None or self._valve_node is None:
          await asyncio.sleep(hiaf_config.SENSOR_POLL_SEC)
          continue
  ```

- `63d6e70`（PR #51，7 月 24 日）就曾修复过一次 "sensor poll 恢复 `_ensure_connected` 调用"。
- `95bccbe` 合入时在 `hiaf_ioc_final.py` 留下 16 个未解决的冲突标记。
- `1333b58` "fix: resolve merge conflicts"（7 月 26 日）对所有冲突**全部接受 origin/main
  版本**，净删 139 行，把 `_ensure_connected()` 的调用点又删掉了。当前 HEAD 即此状态。

部署确认：`deploy/docker-compose.yml` 的 `ioc` 服务 build `../py-agent/ioc`，
其 Dockerfile `CMD ["python", "hiaf_ioc_final.py"]` —— 跑的就是这个断掉的文件。
（仓库根目录的 `ioc/hiaf_ioc.py` 是旧副本，不参与部署。）

## 问题 2：API 写入返回 200 但硬件没反应

写入链路每一环都"成功"，断点只在最后一段（IOC → OPC UA）：

1. `POST /api/v1/instruments/gascell/start` → `GasCellStart`（`handler.go:492`）
   → `WriteGasCellPV(role, "GasCell:Piezo:Running", 1)`（`service.go:417`）。
2. 权限校验 + `ValidatePVParams` 通过 → `putPV`（`service.go:350`）
   → POST `http://epics-gateway:5070/GasCell:Piezo:Running`。
3. 网关白名单放行该 PV 写（`epics-gateway.py:28`）→ `caput(pv, 1, wait=True)`。
4. IOC 的 `Piezo_Running.putter`（`hiaf_ioc_final.py:945-958`）**只设置内存状态**
   `self._running = True` 并更新 PV 值，完全不触碰 OPC UA —— 所以 caput 成功返回。
5. Go 端回读 `GasCell:Piezo:Running` = 1，与期望值一致 → HTTP 200。

随后 `_pi_control_loop` 每 100ms 醒来：`self._running` 为 True，但 `self._opc is None`
→ `continue`，永远走不到 `_pi_cycle` → `_valve_node.write_value()`。
阀门节点从未被写入，硬件自然无反应。

同类假成功：`Piezo_ValveSP.putter`（`hiaf_ioc_final.py:960-974`）在 `self._opc is None`
时**静默跳过** OPC UA 写入、照样返回成功 —— 手动阀控制（`/gascell/valve`）同样是假成功。

## 问题 3：日志为什么没有 OPC UA 输出

不是异常被静默吞掉，而是**包含日志的代码路径根本没执行**：

- `_ensure_connected()` 内的 `LOGGER.error("OPC UA connect failed...")` /
  `LOGGER.info("OPC UA connected...")` 从未运行。
- 启动后未按 start 时：sensor loop 走 `_opc is None → sleep → continue`（无日志）；
  PI loop 走 `not self._running` 分支（无日志）。与诊断数据"启动正常、无 OPC UA 输出"完全吻合。
- 唯一会输出的是 Running=1 后 PI loop 的
  `OPC UA disconnected, skipping cycle`（`hiaf_ioc_final.py:690`，10Hz warning）。
  **验证方法**：API 调一次 `/gascell/start` 后 `docker logs lab-ioc` 应看到这条刷屏，
  看到即坐实本诊断。

健康检查 503 → unhealthy 也与此一致：`opc_ok = self._opc is not None`
（`hiaf_ioc_final.py:505`）永远为 False。

## 修复建议

1. **恢复调用点**（最小修复）：把 `_sensor_poll_loop` 的 `_opc is None` 分支改回
   `042d67b` 的模式 —— 先 `await self._ensure_connected()`，仍失败再 sleep+continue。
   PI loop 依赖 sensor loop 建连即可，无需单独加。
2. 修复后若仍连不上，才需要排查 OPC UA 服务器 `opc.tcp://10.51.12.158:4862`
   的网络可达性 —— 当前客户端压根没发起连接，可达性不是瓶颈。
3. **防再犯**：同一个 bug 已出现两次（#51 修过、1333b58 merge 又丢掉）。建议：
   - 在 `py-agent/tests/` 加一个启动路径测试（mock asyncua，断言启动后
     `_ensure_connected` 被调用 / `_opc` 非 None）；
   - 或至少在 CI 对 `hiaf_ioc_final.py` 做 grep 级检查：`_ensure_connected` 调用数 ≥ 1。
4. 可选加固：`Piezo_ValveSP.putter` 在 `_opc is None` 时应抛错而不是静默成功，
   让网关/上层拿到真实失败，避免"200 但硬件没动"的假象。
