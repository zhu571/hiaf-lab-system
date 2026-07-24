"""
caproto IOC for HIAF Gas Cell — integrated sensor monitoring + PI pressure control.
Connects to Siemens WinCC via OPC UA (asyncua).

PV prefix: GasCell:
  GasCell:Temp:T*, PT1000_* — temperature sensors (r/o)
  GasCell:Press:P*           — pressure sensors (r/o)
  GasCell:Vac:A*             — vacuum sensors (r/o)
  GasCell:Piezo:*            — piezo valve PI control (r/w + r/o)

Background tasks:
  1. Sensor poll loop  ~1 Hz  — reads all 27 sensor OPC UA nodes
  2. PI control loop   ~10 Hz — velocity-form PI, only active when Running=1
"""

from __future__ import annotations

import argparse
import asyncio
import logging
import logging.config
import os
import random
import time
from collections import defaultdict
from datetime import datetime
from typing import Any
from urllib.request import urlopen
from urllib.parse import quote

from caproto import AlarmSeverity, AlarmStatus
from caproto.server import PVGroup, pvproperty, run

from asyncua import Client, ua
from hiaf_storage import HiafStorage
import hiaf_config

LOGGER = logging.getLogger(__name__)

# ── Structured logging (D4) ──
_LOGGING_CONFIG: dict = {
    "version": 1,
    "formatters": {
        "default": {
            "format": "%(asctime)s %(levelname)s %(name)s: %(message)s",
        },
    },
    "handlers": {
        "stdout": {
            "class": "logging.StreamHandler",
            "stream": "ext://sys.stdout",
            "level": "INFO",
            "formatter": "default",
        },
        "stderr": {
            "class": "logging.StreamHandler",
            "stream": "ext://sys.stderr",
            "level": "WARNING",
            "formatter": "default",
        },
    },
    "root": {
        "level": "INFO",
        "handlers": ["stdout", "stderr"],
    },
}
logging.config.dictConfig(_LOGGING_CONFIG)


QUEUE_HIGH_WATERMARK = 800
QUEUE_CRITICAL_WATERMARK = 950
HEARTBEAT_STALL_SEC = 30
HEARTBEAT_RETRY_SEC = 60


class _SensorSubHandler:
    """OPC UA subscription callback — pure enqueue, no PV write (P1)."""
    def __init__(self, nodeid_to_tag: dict[str, str], queue: asyncio.Queue,
                 last_callback_ts: list[float]) -> None:
        self._nodeid_to_tag = nodeid_to_tag
        self._queue = queue
        self._last_callback_ts = last_callback_ts

    def datachange_notification(self, node, val, data) -> None:
        tag = self._nodeid_to_tag.get(str(node.nodeid))
        if tag is None or val is None:
            return
        self._last_callback_ts[0] = time.monotonic()
        try:
            self._queue.put_nowait((tag, float(val)))
        except asyncio.QueueFull:
            pass


class HiafGasCellIOC(PVGroup):
    """Integrated HIAF Gas Cell IOC — sensors + piezo valve PI control."""

    # ═══════════════════════════════════════════════
    # Group 1: Temperature  (14 PVs, r/o)
    # ═══════════════════════════════════════════════
    Temp_T1 = pvproperty(
        name="Temp:T1", value=0.0, dtype=float, read_only=True,
        doc="万瑞冷氦入口 (°C)",
    )
    Temp_T2 = pvproperty(
        name="Temp:T2", value=0.0, dtype=float, read_only=True,
        doc="万瑞冷氦出口 (°C)",
    )
    Temp_T3 = pvproperty(
        name="Temp:T3", value=0.0, dtype=float, read_only=True,
        doc="气体单元冷氦入口 (°C)",
    )
    Temp_T4 = pvproperty(
        name="Temp:T4", value=0.0, dtype=float, read_only=True,
        doc="加热块一侧 (°C)",
    )
    Temp_T5 = pvproperty(
        name="Temp:T5", value=0.0, dtype=float, read_only=True,
        doc="加热块对称侧 (°C)",
    )
    Temp_T6 = pvproperty(
        name="Temp:T6", value=0.0, dtype=float, read_only=True,
        doc="冷氦出口/通道8故障 (°C)",
    )
    Temp_T7 = pvproperty(
        name="Temp:T7", value=0.0, dtype=float, read_only=True,
        doc="高纯氦入口 (°C)",
    )
    Temp_T8 = pvproperty(
        name="Temp:T8", value=0.0, dtype=float, read_only=True,
        doc="预冷单元 (°C)",
    )
    Temp_T9 = pvproperty(
        name="Temp:T9", value=0.0, dtype=float, read_only=True,
        doc="住友冷氦入口 (°C)",
    )
    Temp_T10 = pvproperty(
        name="Temp:T10", value=0.0, dtype=float, read_only=True,
        doc="住友冷氦出口 (°C)",
    )
    Temp_PT1000_1 = pvproperty(
        name="Temp:PT1000_1", value=0.0, dtype=float, read_only=True,
        doc="PT1000-1 内腔 (°C)",
    )
    Temp_PT1000_2 = pvproperty(
        name="Temp:PT1000_2", value=0.0, dtype=float, read_only=True,
        doc="PT1000-2 内腔 (°C)",
    )
    Temp_PT1000_3 = pvproperty(
        name="Temp:PT1000_3", value=0.0, dtype=float, read_only=True,
        doc="PT1000-3 内腔 (°C)",
    )
    Temp_PT1000_4 = pvproperty(
        name="Temp:PT1000_4", value=0.0, dtype=float, read_only=True,
        doc="PT1000-4 内腔 (°C)",
    )

    # ═══════════════════════════════════════════════
    # Group 2: Pressure  (6 PVs, r/o)
    # ═══════════════════════════════════════════════
    Press_P1 = pvproperty(
        name="Press:P1", value=0.0, dtype=float, read_only=True,
        doc="万瑞冷氦管路 (bar)",
    )
    Press_P2 = pvproperty(
        name="Press:P2", value=0.0, dtype=float, read_only=True,
        doc="预冷液氮罐 (bar)",
    )
    Press_P3 = pvproperty(
        name="Press:P3", value=0.0, dtype=float, read_only=True,
        doc="循环泵前 (bar)",
    )
    Press_P4 = pvproperty(
        name="Press:P4", value=0.0, dtype=float, read_only=True,
        doc="循环泵后 (bar)",
    )
    Press_P6 = pvproperty(
        name="Press:P6", value=0.0, dtype=float, read_only=True,
        doc="缓冲罐 (bar)",
    )
    Press_P8 = pvproperty(
        name="Press:P8", value=0.0, dtype=float, read_only=True,
        doc="住友冷氦管路 (bar)",
    )

    # ═══════════════════════════════════════════════
    # Group 3: Vacuum  (7 PVs, r/o)
    # ═══════════════════════════════════════════════
    Vac_A1 = pvproperty(
        name="Vac:A1", value=0.0, dtype=float, read_only=True,
        doc="气体单元内腔 (Pa)",
    )
    Vac_A2 = pvproperty(
        name="Vac:A2", value=0.0, dtype=float, read_only=True,
        doc="气体单元夹层 (Pa)",
    )
    Vac_A4 = pvproperty(
        name="Vac:A4", value=0.0, dtype=float, read_only=True,
        doc="一级差分腔体 (Pa)",
    )
    Vac_A5 = pvproperty(
        name="Vac:A5", value=0.0, dtype=float, read_only=True,
        doc="二级差分腔体 (Pa)",
    )
    Vac_A6 = pvproperty(
        name="Vac:A6", value=0.0, dtype=float, read_only=True,
        doc="万瑞真空夹层 (Pa)",
    )
    Vac_A7 = pvproperty(
        name="Vac:A7", value=0.0, dtype=float, read_only=True,
        doc="预冷真空夹层 (Pa)",
    )
    Vac_A8 = pvproperty(
        name="Vac:A8", value=0.0, dtype=float, read_only=True,
        doc="住友真空夹层 (Pa)",
    )

    # ═══════════════════════════════════════════════
    # Group 4: Piezo Valve Control  (8 PVs, r/w + r/o)
    # ═══════════════════════════════════════════════
    Piezo_A1 = pvproperty(
        name="Piezo:A1", value=0.0, dtype=float, read_only=True,
        doc="内腔气压 (Pa) — fast poll from PI loop",
    )
    Piezo_ValveSP = pvproperty(
        name="Piezo:ValveSP", value=61.0, dtype=float,
        doc="压电阀开度设定 (0-100)",
    )
    Piezo_Setpoint = pvproperty(
        name="Piezo:Setpoint", value=hiaf_config.DEFAULT_SETPOINT, dtype=float,
        doc="目标气压 (Pa)",
    )
    Piezo_Kp = pvproperty(
        name="Piezo:Kp", value=hiaf_config.DEFAULT_KP, dtype=float,
        doc="比例增益",
    )
    Piezo_Ki = pvproperty(
        name="Piezo:Ki", value=hiaf_config.DEFAULT_KI, dtype=float,
        doc="积分增益",
    )
    Piezo_Running = pvproperty(
        name="Piezo:Running", value=0, dtype=int,
        doc="0=STOP, 1=RUN",
    )
    Piezo_Error = pvproperty(
        name="Piezo:Error", value=0.0, dtype=float, read_only=True,
        doc="压力误差 (sp - A1)",
    )
    Piezo_Delta = pvproperty(
        name="Piezo:Delta", value=0.0, dtype=float, read_only=True,
        doc="最后阀门增量",
    )
    Piezo_Cycle = pvproperty(
        name="Piezo:Cycle", value=0, dtype=int, read_only=True,
        doc="控制周期计数",
    )
    # ═══════════════════════════════════════════════
    # Group 5: Safety — A5 overpressure protection
    # ═══════════════════════════════════════════════
    Safety_A5Max = pvproperty(
        name="Safety:A5Max", value=10.0, dtype=float,
        doc="A5 上限阈值 (Pa)",
    )
    Safety_A5Trip = pvproperty(
        name="Safety:A5Trip", value=0, dtype=int, read_only=True,
        doc="0=normal, 1=overpressure, 2=sensor fault",
    )
    Safety_A5TripTime = pvproperty(
        name="Safety:A5TripTime", value="", dtype=str, read_only=True,
        doc="触发时间 (ISO)",
    )
    Safety_A5TripPV = pvproperty(
        name="Safety:A5TripPV", value="0.0", dtype=str, read_only=True,
        doc="触发时 A5 值",
    )
    Safety_A5Clear = pvproperty(
        name="Safety:A5Clear", value=0, dtype=int,
        doc="写 1 清除保护状态",
    )

    # ── init ──────────────────────────────────────
    def __init__(self, *args: Any, **kwargs: Any) -> None:
        super().__init__(*args, **kwargs)

        # OPC UA state
        self._opc: Client | None = None
        self._valve_node = None
        self._connected: bool | None = None

        # Reconnect backoff state (R1)
        self._reconnect_backoff = 0.0
        self._reconnect_base = 1.0
        self._reconnect_max = 60.0
        self._reconnect_jitter = 2.0

        # Tag → OPC UA node mapping (built after connect)
        self._sensor_nodes: dict[str, Any] = {}

        # Tag → pvproperty mapping for sensor poll
        self._sensor_pvs: dict[str, Any] = {}
        for tag, pv_name in hiaf_config.ALL_SENSOR_TAGS:
            attr_name = pv_name.replace(":", "_")      # "Temp:T1" → "Temp_T1"
            self._sensor_pvs[tag] = getattr(self, attr_name)

        # Sensor poll cache (for fast readout)
        self._sensor_values: dict[str, float] = {tag: 0.0 for tag, _ in hiaf_config.ALL_SENSOR_TAGS}

        # Safety state
        self._a5_tripped = False
        self._a5_max = 10.0

        # PI control state
        self._last_error = 0.0
        self._cycle = 0
        self._running = False
        self._valve_rate_max = hiaf_config.VALVE_RATE_MAX

        # PI parameter cache (updated by putters)
        self._sp_val = hiaf_config.DEFAULT_SETPOINT
        self._kp_val = hiaf_config.DEFAULT_KP
        self._ki_val = hiaf_config.DEFAULT_KI

        # Failure tracking
        self._fail_count = 0

        # A1 value cache (from OPC UA, shared between sensor poll and PI)
        self._a1_from_opc = 0.0

        # Background task tracking (for graceful shutdown)
        self._tasks: list[asyncio.Task] = []

        # ── Storage (InfluxDB + SQLite) ──
        self._storage = HiafStorage(
            influx_url=hiaf_config.INFLUX_URL,
            influx_token=hiaf_config.INFLUX_TOKEN,
            influx_org=hiaf_config.INFLUX_ORG,
            influx_bucket=hiaf_config.INFLUX_BUCKET,
            db_path=str(hiaf_config.DB_PATH),
            sensor_tags=hiaf_config.ALL_SENSOR_TAGS,
            pump_tags=hiaf_config.PUMP_TAGS,
        )

        # ── Pump OPC UA cache ──
        self._pump_nodes: dict = {}
        self._pump_values: dict = {}
        self._active_pump_tags: list = []  # populated on first connect

        # R4: per-sensor error counter & cool-off
        self._sensor_error_count: dict[str, int] = defaultdict(int)
        self._sensor_max_errors = 3
        self._sensor_cooloff_cycles = 2
        self._dead_nodes: set[str] = set()

        # P1: OPC UA subscription — async queue + heartbeat
        self._subscription = None
        self._sub_handler = None
        self._sub_queue: asyncio.Queue = asyncio.Queue(maxsize=1000)
        self._last_callback_ts: list[float] = [0.0]
        self._data_loss_cnt: int = 0
        self._subscription_healthy: bool = False
        # nodeid → tag lookup (built at subscription time, not __init__)
        self._nodeid_to_tag: dict[str, str] = {}

        # R5: ntfy alert dedup
        self._last_ntfy_disconnect_warn = 0.0
        self._last_ntfy_recovery_warn = 0.0
        self._last_ntfy_failrate_warn = 0.0
        self._ntfy_cooldown = 60.0
        self._disconnect_warned = False

    @property
    def _alarm_pvs(self):
        """PVs that get alarm status updates."""
        return (
            self.Piezo_A1, self.Piezo_ValveSP,
            self.Piezo_Error, self.Piezo_Delta, self.Piezo_Cycle,
            self.Temp_T1, self.Press_P1, self.Vac_A1,
        )

    # ── OPC UA connection management ──────────────
    async def _ensure_connected(self) -> bool:
        """Verify OPC UA connection is alive; reconnect + rebuild nodes if dead.
        Uses exponential backoff with jitter (R1)."""
        if self._opc is not None:
            try:
                test_node = self._opc.get_node("i=2258")
                await asyncio.wait_for(test_node.read_value(), timeout=5.0)
                self._reconnect_backoff = 0.0
                self._sensor_error_count.clear()
                # Repopulate pump tags after dead node clear
                self._active_pump_tags = [
                    k for k in self._pump_nodes
                    if any(s in k for s in ['DP3', 'DP4', '循环泵', '压缩机', '低温循环泵'])
                ]
                # R5: send recovery alert
                if self._disconnect_warned:
                    now_w = time.monotonic()
                    if now_w - self._last_ntfy_recovery_warn > self._ntfy_cooldown:
                        await self._send_ntfy("OPC UA 已恢复")
                        self._last_ntfy_recovery_warn = now_w
                    self._disconnect_warned = False
                return True
            except Exception:
                LOGGER.warning("OPC UA connection lost — reconnecting...")
                try:
                    await self._opc.disconnect()
                except Exception:
                    LOGGER.debug("disconnect ignored during reconnect")
                self._valve_node = None
                self._sensor_nodes.clear()
                self._subscription = None
                self._subscription_healthy = False

        # R5: ntfy alert on prolonged disconnection
        if self._reconnect_backoff >= 30.0 and not self._disconnect_warned:
            self._disconnect_warned = True
            now_w = time.monotonic()
            if now_w - self._last_ntfy_disconnect_warn > self._ntfy_cooldown:
                await self._send_ntfy("OPC UA 断连 >30s")
                self._last_ntfy_disconnect_warn = now_w

        # Backoff: exponential with jitter (R1)
        if self._reconnect_backoff > 0:
            sleep_sec = self._reconnect_backoff + random.random() * self._reconnect_jitter
            await asyncio.sleep(sleep_sec)

        # (Re)connect
        try:
            self._opc = Client(hiaf_config.OPC_URL, timeout=10)
            await self._opc.connect()
            self._valve_node = self._opc.get_node(hiaf_config.VALVE_NODE_ID)
            # Rebuild all sensor node references (old handles are invalid)
            for tag, _ in hiaf_config.ALL_SENSOR_TAGS:
                node_id = f"ns=1;s=t|{tag}"
                try:
                    self._sensor_nodes[tag] = self._opc.get_node(node_id)
                except Exception as e:
                    LOGGER.warning("Dead sensor node on connect — %s: %s", tag, e)
                    self._dead_nodes.add(tag)
            # Also rebuild pump nodes
            self._pump_nodes.clear()
            for opc_tag, meas, ftag in hiaf_config.PUMP_TAGS:
                node_id = f"ns=1;s=t|{opc_tag}"
                self._pump_nodes[opc_tag] = self._opc.get_node(node_id)
            # P4: precompute active pump tags at connect time
            self._active_pump_tags = [
                k for k in self._pump_nodes
                if any(s in k for s in ['DP3', 'DP4', '循环泵', '压缩机', '低温循环泵'])
            ]
            self._reconnect_backoff = 0.0
            # P3.2: clear dead_nodes only after all nodes processed
            self._dead_nodes.clear()
            # P1: re-create OPC UA subscription
            await self._setup_subscription()
            LOGGER.info("OPC UA connected — %d sensor nodes cached (%d dead)",
                        len(self._sensor_nodes), len(self._dead_nodes))
            return True
        except Exception as e:
            self._reconnect_backoff = min(
                self._reconnect_max,
                max(self._reconnect_base, self._reconnect_backoff * 2) if self._reconnect_backoff > 0 else self._reconnect_base,
            )
            LOGGER.error("OPC UA connect failed (backoff=%.1fs): %s", self._reconnect_backoff, e)
            self._opc = None
            return False

    async def _set_connected(self, connected: bool) -> None:
        """Update alarm status on key PVs."""
        if self._connected == connected:
            return
        self._connected = connected
        if connected:
            status = AlarmStatus.NO_ALARM
            severity = AlarmSeverity.NO_ALARM
        else:
            status = AlarmStatus.COMM
            severity = AlarmSeverity.INVALID_ALARM
        for prop in self._alarm_pvs:
            await prop.alarm.write(status=status, severity=severity)

    # ── Health HTTP server (R2) ──
    async def _run_health_server(self) -> None:
        """Minimal HTTP /health endpoint on port 5080."""
        import asyncio as aio

        async def handle(reader: aio.StreamReader, writer: aio.StreamWriter) -> None:
            try:
                request_line = (await reader.readline()).decode("utf-8", errors="replace").strip()
                while True:
                    line = await reader.readline()
                    if line in (b"\r\n", b"\n", b""):
                        break
                if "GET /health" in request_line or "GET /" in request_line:
                    opc_ok = self._opc is not None
                    caproto_alive = True
                    status_code = 200 if (opc_ok and caproto_alive) else 503
                    status_text = "ok" if status_code == 200 else "degraded"
                    body = '{"status":"' + status_text + '","opc_ua":' + str(opc_ok).lower() + ',"caproto":true}'
                    resp_text = "OK" if status_code == 200 else "Service Unavailable"
                    resp = f"HTTP/1.1 {status_code} {resp_text}\r\nContent-Type: application/json\r\nContent-Length: {len(body.encode())}\r\nConnection: close\r\n\r\n{body}"
                else:
                    resp = "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
                writer.write(resp.encode())
            except Exception:
                pass
            finally:
                try:
                    writer.close()
                except Exception:
                    pass

        server = await aio.start_server(handle, "0.0.0.0", 5080)
        LOGGER.info("Health HTTP server started on port 5080")
        async with server:
            await server.serve_forever()

    async def _read_pump_tags(self) -> None:
        if not self._pump_nodes or not self._active_pump_tags:
            return
        try:
            tasks = [asyncio.wait_for(self._pump_nodes[k].read_value(), timeout=1.0) for k in self._active_pump_tags]
            vals = await asyncio.gather(*tasks, return_exceptions=True)
            for k, v in zip(self._active_pump_tags, vals):
                if not isinstance(v, Exception) and v is not None:
                    self._pump_values[k] = float(v)
        except Exception:
            LOGGER.debug("pump read failed")

    # ── Background task 1: Sensor poll loop ~1Hz ──
    async def _sensor_poll_loop(self) -> None:
        """Poll all 27 sensor OPC UA nodes at ~1Hz, update PVs."""
        cooloff: dict[str, int] = {}
        while True:
            loop_start = time.monotonic()
            try:
                if self._opc is None or self._valve_node is None:
                    await self._ensure_connected()
                    if self._opc is None or self._valve_node is None:
                        await asyncio.sleep(hiaf_config.SENSOR_POLL_SEC)
                        continue

                # When subscription is healthy, skip PV writes (only storage + safety)
                sub_healthy = self._subscription_healthy

                # R4: skip dead nodes + cooled-off nodes
                active_nodes = []
                active_tag_map = []
                for tag, _ in hiaf_config.ALL_SENSOR_TAGS:
                    if tag in self._dead_nodes:
                        continue
                    cc = cooloff.get(tag, 0)
                    if cc > 0:
                        cooloff[tag] = cc - 1
                        continue
                    node = self._sensor_nodes.get(tag)
                    if node is not None:
                        active_nodes.append(node)
                        active_tag_map.append(tag)

                # Read active sensors in parallel
                tasks = [
                    asyncio.wait_for(self._safe_read_node(node), timeout=2.0)
                    for node in active_nodes
                ]
                results = await asyncio.gather(*tasks, return_exceptions=True)

                failed_count = 0
                total_count = len(active_tag_map)
                for tag, val_or_err in zip(active_tag_map, results):
                    if isinstance(val_or_err, Exception):
                        self._sensor_values[tag] = float('nan')
                        self._sensor_error_count[tag] += 1
                        failed_count += 1
                        if self._sensor_error_count[tag] >= self._sensor_max_errors:
                            cooloff[tag] = self._sensor_cooloff_cycles
                            LOGGER.warning("Sensor %s: %d consecutive fails — cooling off %d cycles",
                                           tag, self._sensor_error_count[tag], self._sensor_cooloff_cycles)
                            self._sensor_error_count[tag] = 0
                        continue
                    self._sensor_error_count[tag] = 0
                    if val_or_err is not None:
                        self._sensor_values[tag] = float(val_or_err)
                        if not sub_healthy:
                            pv = self._sensor_pvs[tag]
                            try:
                                await pv.write(float(val_or_err))
                            except Exception:
                                LOGGER.debug("sensor PV write failed for tag %s", tag)

                # R5: ntfy alert if >50% sensors failed
                if total_count > 0:
                    fail_rate = failed_count / total_count
                    if fail_rate > 0.5:
                        now_w = time.monotonic()
                        if now_w - self._last_ntfy_failrate_warn > self._ntfy_cooldown:
                            await self._send_ntfy(
                                f"传感器读取失败率 {fail_rate:.0%} ({failed_count}/{total_count})"
                            )
                            self._last_ntfy_failrate_warn = now_w

                # Also update Piezo:A1 with the Vac:A1 reading
                a1_val = self._sensor_values.get("直采数据_A1", 0.0)
                self._a1_from_opc = a1_val
                if not sub_healthy:
                    try:
                        await self.Vac_A1.write(a1_val)
                    except Exception:
                        LOGGER.debug("Vac_A1 write failed")

                await self._set_connected(True)

                # Read pump tags & write to storage (SQLite + InfluxDB)
                await self._read_pump_tags()
                await self._storage.maybe_write_sensors(self._sensor_values)
                await self._storage.maybe_write_influx(
                    self._sensor_values, self._pump_values,
                    float(self.Piezo_ValveSP.value), self._sp_val, self._last_error,
                )

                # ── A5 overpressure safety check ──
                a5_val = self._sensor_values.get("直采数据_A5")
                if a5_val is None or a5_val != a5_val:  # None or NaN
                    if not self._a5_tripped:
                        await self.Safety_A5Trip.write(2)
                        LOGGER.error("A5 SAFETY: sensor data missing/NaN — PI interlock")
                    self._running = False
                    await self.Piezo_Running.write(0)
                elif a5_val > self._a5_max and not self._a5_tripped:
                    self._a5_tripped = True
                    await self.Safety_A5Trip.write(1)
                    await self.Safety_A5TripTime.write(datetime.now().isoformat())
                    await self.Safety_A5TripPV.write(f"{a5_val:.4f}")
                    self._running = False
                    await self.Piezo_Running.write(0)
                    try:
                        if self._opc is not None and self._valve_node is not None:
                            await self._valve_node.write_value(0.0)
                        await self.Piezo_ValveSP.write(0.0)
                    except Exception as e:
                        LOGGER.error("A5 safety: close valve failed: %s", e)
                    LOGGER.warning("A5 TRIP: A5=%.4fPa (limit=%.1f) — valve closed", a5_val, self._a5_max)
                    # Meow notification
                    if hiaf_config.MEOW_NAME:
                        try:
                            msg = f"A5超压 {a5_val:.1f}Pa 阀门已关闭"
                            await asyncio.to_thread(
                                urlopen,
                                f"https://api.chuckfang.com/{hiaf_config.MEOW_NAME}/A5超压/{quote(msg)}",
                                timeout=5,
                            )
                        except Exception:
                            LOGGER.debug("A5 notification failed")

            except Exception as e:
                LOGGER.warning("Sensor poll cycle error: %s", e)
                await self._set_connected(self._opc is not None)

            elapsed = time.monotonic() - loop_start
            remaining = hiaf_config.SENSOR_POLL_SEC - elapsed
            if remaining > 0:
                await asyncio.sleep(remaining)

    # ── Background task 2: PI control loop 10Hz ──
    async def _pi_control_loop(self) -> None:
        """PI fine pressure control at fixed 10Hz, only active when Running=1."""
        while True:
            await asyncio.sleep(0.1)

            if not self._running:
                try:
                    await self.Piezo_A1.write(self._a1_from_opc)
                except Exception:
                    LOGGER.debug("Piezo_A1 write failed during idle")
                await self._set_connected(self._opc is not None)
                continue

            self._cycle += 1
            await self.Piezo_Cycle.write(self._cycle)

            if self._opc is None:
                LOGGER.warning('OPC UA disconnected, skipping cycle')
                continue

            try:
                a1 = self._a1_from_opc
                self._fail_count = 0
                await self.Piezo_A1.write(a1)
                await self._set_connected(True)

                sp_val = self._sp_val
                error = sp_val - a1
                await self._pi_cycle(sp_val, a1, error)

            except Exception as e:
                self._fail_count += 1
                LOGGER.warning(
                    "PI cycle %d failed (%d/%d): %s",
                    self._cycle, self._fail_count,
                    hiaf_config.MAX_CONSECUTIVE_FAILURES, e,
                )
                if self._fail_count >= hiaf_config.MAX_CONSECUTIVE_FAILURES:
                    LOGGER.error("Max consecutive failures — auto-stop")
                    self._running = False
                    await self.Piezo_Running.write(0)
                    await self._set_connected(False)

    # ── PI cycle: pure velocity-form PI ──
    async def _pi_cycle(self, sp_val, a1, error) -> None:
        """Pure velocity-form PI (no deadband, no D, no feedforward, no trim)."""
        kp_val = self._kp_val
        ki_val = self._ki_val

        try:
            current_v = float(await self._valve_node.read_value())
        except Exception:
            current_v = float(self.Piezo_ValveSP.value)

        derror = error - self._last_error
        p_term = kp_val * derror
        i_term = ki_val * error * hiaf_config.PI_POLL_SEC

        # Anti-windup: clamp integration when valve saturated
        if current_v <= hiaf_config.VALVE_MIN and i_term < 0:
            i_term = 0.0
        if current_v >= hiaf_config.VALVE_MAX and i_term > 0:
            i_term = 0.0

        delta = p_term + i_term
        delta = max(-hiaf_config.VALVE_RATE_MAX, min(hiaf_config.VALVE_RATE_MAX, delta))
        self._last_error = error

        await self.Piezo_Error.write(error)
        await self.Piezo_Delta.write(delta)

        new_valve = current_v + delta
        new_valve = max(hiaf_config.VALVE_MIN, min(hiaf_config.VALVE_MAX, new_valve))
        await self.Piezo_ValveSP.write(new_valve)

    # ── P1: OPC UA subscription consumer + heartbeat ──
    def _drop_stale(self) -> None:
        items: list[tuple[str, float]] = []
        drained = 0
        while True:
            try:
                items.append(self._sub_queue.get_nowait())
            except asyncio.QueueEmpty:
                break
            drained += 1
        if drained == 0:
            return
        latest: dict[str, tuple[str, float]] = {}
        for tag, val in items:
            latest[tag] = (tag, val)
        for t, v in latest.values():
            try:
                self._sub_queue.put_nowait((t, v))
            except asyncio.QueueFull:
                pass
        LOGGER.debug("_drop_stale: merged %d items -> %d", drained, len(latest))

    async def _consume_sub_queue(self) -> None:
        while True:
            tag, val = await self._sub_queue.get()
            qsize = self._sub_queue.qsize()
            if qsize > QUEUE_HIGH_WATERMARK:
                self._drop_stale()
            if qsize > QUEUE_CRITICAL_WATERMARK:
                self._data_loss_cnt += 1
                if self._data_loss_cnt % 10 == 1:
                    LOGGER.warning("data_loss_cnt=%d queue_depth=%d", self._data_loss_cnt, qsize)
                    await self._send_ntfy(f"订阅数据丢失 {self._data_loss_cnt} 次")
            self._sensor_values[tag] = val
            pv = self._sensor_pvs.get(tag)
            if pv is not None:
                try:
                    await pv.write(val)
                except Exception:
                    LOGGER.debug("sub consumer PV write failed for tag %s", tag)
            if tag == "直采数据_A1":
                self._a1_from_opc = val
                try:
                    await self.Piezo_A1.write(val)
                except Exception:
                    LOGGER.debug("Piezo_A1 write failed in sub consumer")

    async def _heartbeat_check(self) -> None:
        while True:
            await asyncio.sleep(1)
            now = self._last_callback_ts[0]
            if now > 0 and (time.monotonic() - now) > HEARTBEAT_STALL_SEC and self._subscription_healthy:
                LOGGER.warning("订阅断流30s，触发poll fallback")
                self._subscription_healthy = False
                task = asyncio.create_task(self._maybe_recover_subscription())
                self._tasks.append(task)

    async def _maybe_recover_subscription(self) -> None:
        while not self._subscription_healthy:
            await asyncio.sleep(HEARTBEAT_RETRY_SEC)
            LOGGER.info("尝试恢复OPC UA订阅...")
            await self._setup_subscription()
            if self._subscription_healthy:
                LOGGER.info("OPC UA订阅已恢复")
            else:
                LOGGER.warning("OPC UA订阅恢复失败，60s后重试")

    async def _setup_subscription(self) -> None:
        if self._opc is None or not self._sensor_nodes:
            return
        if self._subscription is not None:
            try:
                await self._subscription.delete()
            except Exception:
                LOGGER.debug("old subscription delete ignored during setup")
        self._subscription_healthy = False
        try:
            self._nodeid_to_tag.clear()
            for tag, node in self._sensor_nodes.items():
                if node is not None:
                    self._nodeid_to_tag[str(node.nodeid)] = tag
            self._sub_handler = _SensorSubHandler(self._nodeid_to_tag, self._sub_queue, self._last_callback_ts)
            sub_obj = await self._opc.create_subscription(100, self._sub_handler)
            node_ids = []
            for tag, node in self._sensor_nodes.items():
                if tag not in self._dead_nodes:
                    node_ids.append(node)
            if node_ids:
                await sub_obj.subscribe_data_change(node_ids, queuesize=1000)
            self._subscription = sub_obj
            self._subscription_healthy = True
            LOGGER.info("OPC UA subscription created — publishing=100ms queuesize=1000 %d nodes", len(node_ids))
        except Exception as e:
            LOGGER.warning("OPC UA subscription failed — falling back to poll: %s", e)
            self._subscription = None
            self._subscription_healthy = False

    # ── R5: ntfy alert helper ──
    async def _send_ntfy(self, message: str) -> None:
        ntfy_url = os.getenv("NTFY_URL", "http://ntfy:80")
        topic = os.getenv("NTFY_TOPIC", "lab-system")
        try:
            import aiohttp
            async with aiohttp.ClientSession() as session:
                await session.post(
                    f"{ntfy_url}/{topic}",
                    data=message.encode(),
                    headers={"Title": "IOC", "Priority": "3"},
                    timeout=aiohttp.ClientTimeout(total=5),
                )
        except ImportError:
            try:
                req = urlopen(f"{ntfy_url}/{topic}", data=message.encode(), timeout=5)
                req.close()
            except Exception as e:
                LOGGER.debug("ntfy send failed: %s", e)
        except Exception as e:
            LOGGER.debug("ntfy send failed: %s", e)

    # ── OPC UA safe reads ──
    async def _safe_read_node(self, node) -> float | None:
        """Read a generic OPC UA node, return value or None on failure."""
        try:
            val = await node.read_value()
            return float(val)
        except Exception:
            return None

    async def _safe_read_a1(self) -> float:
        """Read A1 from OPC UA (vacuum inner chamber). Raises on failure."""
        a1_node = self._sensor_nodes.get("直采数据_A1")
        if a1_node is None:
            raise RuntimeError("A1 node not cached")
        val = await a1_node.read_value()
        return float(val)

    # ── Startup hook (launches both background tasks) ──
    @Piezo_Running.startup
    async def Piezo_Running(self, instance, async_lib):
        """Startup: connect OPC UA, launch all background tasks."""
        LOGGER.info("HiafGasCellIOC starting up...")

        # Initialize SQLite database via storage
        await self._storage.init_db()

        # Launch sensor poll background task
        task = asyncio.create_task(self._sensor_poll_loop())
        self._tasks.append(task)
        LOGGER.info("Sensor poll loop started (~%g Hz)", 1.0 / hiaf_config.SENSOR_POLL_SEC)

        # Launch OPC UA subscription consumer
        task = asyncio.create_task(self._consume_sub_queue())
        self._tasks.append(task)
        LOGGER.info("Subscription consumer started")

        # Launch heartbeat check
        task = asyncio.create_task(self._heartbeat_check())
        self._tasks.append(task)
        LOGGER.info("Heartbeat check started (stall=%ds retry=%ds)", HEARTBEAT_STALL_SEC, HEARTBEAT_RETRY_SEC)

        # Launch health HTTP server (R2)
        task = asyncio.create_task(self._run_health_server())
        self._tasks.append(task)
        LOGGER.info("Health HTTP server started on port 5080")

        # Launch PI control loop as independent task
        task = asyncio.create_task(self._pi_control_loop())
        self._tasks.append(task)
        LOGGER.info("PI control loop started (10Hz)")

        # Keep startup alive
        while True:
            await asyncio.sleep(3600)

    # ── PV putter handlers (cache to instance vars — NO .read() calls) ──

    @Piezo_Running.putter
    async def Piezo_Running(self, instance, value):
        if int(value) == 1 and self._a5_tripped:
            LOGGER.warning("Cannot start PI — A5 safety trip active")
            return 0
        self._running = bool(int(value))
        if self._running:
            self._last_error = 0.0
            self._cycle = 0
            await self.Piezo_Cycle.write(0)
            LOGGER.info("PI control STARTED (target=%dPa)", self._sp_val)
        else:
            LOGGER.info("PI control STOPPED")
        return int(self._running)

    @Piezo_ValveSP.putter
    async def Piezo_ValveSP(self, instance, value):
        """Manual valve setpoint write (bypasses PI). Refused during safety trip."""
        if self._a5_tripped:
            return self.Piezo_ValveSP.value  # refuse, keep current
        val = float(value)
        val = max(0.0, min(hiaf_config.VALVE_MAX, val))
        if self._opc is not None and self._valve_node is not None:
            try:
                await self._valve_node.write_value(val)
                LOGGER.info("Valve manually set to %.2f", val)
            except Exception as e:
                LOGGER.error("Valve write failed: %s", e)
                raise
        return val

    @Safety_A5Max.putter
    async def Safety_A5Max(self, instance, value):
        self._a5_max = float(value)
        return value

    @Safety_A5Clear.putter
    async def Safety_A5Clear(self, instance, value):
        if int(value) == 1:
            a5_val = self._sensor_values.get("直采数据_A5")
            if a5_val is None or a5_val != a5_val or a5_val > self._a5_max:
                LOGGER.warning("A5 safety clear refused — A5=%.4f > limit=%.1f",
                               a5_val or 999, self._a5_max)
                return 0
            self._a5_tripped = False
            await self.Safety_A5Trip.write(0)
            await self.Safety_A5TripTime.write("")
            await self.Safety_A5TripPV.write("0.0")
            LOGGER.warning("A5 safety trip cleared")
        return value

    @Piezo_Setpoint.putter
    async def Piezo_Setpoint(self, instance, value):
        self._sp_val = float(value)
        return self._sp_val

    @Piezo_Kp.putter
    async def Piezo_Kp(self, instance, value):
        self._kp_val = float(value)
        return self._kp_val

    @Piezo_Ki.putter
    async def Piezo_Ki(self, instance, value):
        self._ki_val = float(value)
        return self._ki_val

    # ── Shutdown ──
    @Piezo_Running.shutdown
    async def Piezo_Running(self, instance, async_lib):
        self._running = False
        for t in self._tasks:
            t.cancel()
        if self._tasks:
            _, pending = await asyncio.wait(self._tasks, timeout=5.0)
            for t in pending:
                LOGGER.warning("Task %s did not finish in time", t)
            self._tasks.clear()
        if self._subscription is not None:
            try:
                await self._subscription.delete()
            except Exception:
                LOGGER.debug("subscription delete ignored")
            self._subscription = None
        if self._opc is not None:
            try:
                await asyncio.wait_for(self._opc.disconnect(), timeout=3.0)
                LOGGER.info("OPC UA disconnected")
            except asyncio.TimeoutError:
                LOGGER.warning("OPC UA disconnect timed out")
            except Exception:
                LOGGER.debug("OPC UA disconnect ignored during shutdown")
        await self._storage.close()


# ── Entry point ─────────────────────────────────

def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="caproto IOC for HIAF Gas Cell — sensors + PI control",
    )
    parser.add_argument(
        "--prefix", default="GasCell:",
        help="EPICS PV prefix (default: GasCell:)",
    )
    parser.add_argument(
        "--list-pvs", action="store_true",
        help="List PV names and exit",
    )
    parser.add_argument(
        "--verbose", "-v", action="count", default=0,
        help="Increase logging verbosity",
    )
    return parser


def main() -> None:
    args = build_arg_parser().parse_args()
    if args.verbose:
        logging.getLogger().setLevel(logging.DEBUG)

    ioc = HiafGasCellIOC(prefix=args.prefix)

    if args.list_pvs:
        for pvname in sorted(ioc.pvdb):
            print(pvname)
        return

    run(ioc.pvdb)


if __name__ == "__main__":
    main()
