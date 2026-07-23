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
import time
from datetime import datetime
from typing import Any
from urllib.request import urlopen
from urllib.parse import quote

from caproto import AlarmSeverity, AlarmStatus
from caproto.server import PVGroup, pvproperty, run

from asyncua import Client
from hiaf_storage import HiafStorage
import hiaf_config

LOGGER = logging.getLogger(__name__)


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
    # Group 4: Piezo Valve Control  (9 PVs, r/w + r/o)
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
    Piezo_Kd = pvproperty(
        name="Piezo:Kd", value=hiaf_config.DEFAULT_KD, dtype=float,
        doc="微分增益 (PV微分)",
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
        self._last_error_sign = 0
        self._last_sign_change = 0.0
        self._cycle = 0
        self._running = False
        self._valve_rate_max = hiaf_config.VALVE_RATE_MAX  # adjustable per-instance

        # PI parameter cache (updated by putters)
        self._sp_val = hiaf_config.DEFAULT_SETPOINT
        self._kp_val = hiaf_config.DEFAULT_KP
        self._ki_val = hiaf_config.DEFAULT_KI
        self._kd_val = hiaf_config.DEFAULT_KD
        self._filtered_dpv = 0.0
        self._last_a1_for_d = None

        # Failure tracking
        self._fail_count = 0

        # A1 value cache (from OPC UA, shared between sensor poll and PI)
        self._a1_from_opc = 0.0
        self._last_a1 = None  # for pressure rate suppression

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
        """Verify OPC UA connection is alive; reconnect + rebuild nodes if dead."""
        if self._opc is not None:
            try:
                test_node = self._opc.get_node("i=2258")
                await asyncio.wait_for(test_node.read_value(), timeout=5.0)
                return True
            except Exception:
                LOGGER.warning("OPC UA connection lost — reconnecting...")
                try:
                    await self._opc.disconnect()
                except Exception:
                    LOGGER.debug("disconnect ignored during reconnect")
                self._valve_node = None
                self._sensor_nodes.clear()

        # (Re)connect
        try:
            self._opc = Client(hiaf_config.OPC_URL, timeout=10)
            await self._opc.connect()
            self._valve_node = self._opc.get_node(hiaf_config.VALVE_NODE_ID)
            # Rebuild all sensor node references (old handles are invalid)
            for tag, _ in hiaf_config.ALL_SENSOR_TAGS:
                node_id = f"ns=1;s=t|{tag}"
                self._sensor_nodes[tag] = self._opc.get_node(node_id)
            # Also rebuild pump nodes
            self._pump_nodes.clear()
            for opc_tag, meas, ftag in hiaf_config.PUMP_TAGS:
                node_id = f"ns=1;s=t|{opc_tag}"
                self._pump_nodes[opc_tag] = self._opc.get_node(node_id)
            self._active_pump_tags = [tag for tag in list(self._pump_nodes.keys())[:20]]
            LOGGER.info("OPC UA connected — %d sensor nodes cached", len(self._sensor_nodes))
            return True
        except Exception as e:
            LOGGER.error("OPC UA connect failed: %s", e)
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

    async def _read_pump_tags(self) -> None:
        if not self._pump_nodes:
            return
        active_keys = [
            k for k in self._pump_nodes
            if any(s in k for s in ['DP3', 'DP4', '循环泵', '压缩机', '低温循环泵'])
        ]
        if not active_keys:
            return
        try:
            tasks = [asyncio.wait_for(self._pump_nodes[k].read_value(), timeout=1.0) for k in active_keys]
            vals = await asyncio.gather(*tasks, return_exceptions=True)
            for k, v in zip(active_keys, vals):
                if not isinstance(v, Exception) and v is not None:
                    self._pump_values[k] = float(v)
        except Exception:
            LOGGER.debug("pump read failed")

    # ── Background task 1: Sensor poll loop ~1Hz ──
    async def _sensor_poll_loop(self) -> None:
        """Poll all 27 sensor OPC UA nodes at ~1Hz, update PVs."""
        while True:
            loop_start = time.monotonic()
            try:
                if self._opc is None or self._valve_node is None:
                    await self._ensure_connected()
                    if self._opc is None or self._valve_node is None:
                        await asyncio.sleep(hiaf_config.SENSOR_POLL_SEC)
                        continue

                # Read all 27 sensors in parallel
                tasks = [
                    asyncio.wait_for(self._safe_read_node(node), timeout=2.0)
                    for node in self._sensor_nodes.values()
                ]
                results = await asyncio.gather(*tasks, return_exceptions=True)

                # Update PVs
                for (tag, _), val_or_err in zip(hiaf_config.ALL_SENSOR_TAGS, results):
                    if isinstance(val_or_err, Exception):
                        self._sensor_values[tag] = float('nan')
                        continue
                    if val_or_err is not None:
                        self._sensor_values[tag] = float(val_or_err)
                        pv = self._sensor_pvs[tag]
                        try:
                            await pv.write(float(val_or_err))
                        except Exception:
                            LOGGER.debug("sensor PV write failed for tag %s", tag)

                # Also update Piezo:A1 with the Vac:A1 reading
                a1_val = self._sensor_values.get("直采数据_A1", 0.0)
                self._a1_from_opc = a1_val
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

    # ── Background task 2: PI control loop ~10Hz ──
    async def _pi_control_loop(self, sleep) -> None:
        """PI精细维持，only active when Running=1."""
        while True:
            loop_start = time.monotonic()

            if not self._running:
                try:
                    await self.Piezo_A1.write(self._a1_from_opc)
                except Exception:
                    LOGGER.debug("Piezo_A1 write failed during idle")
                await self._set_connected(self._opc is not None)
                await sleep(hiaf_config.PI_POLL_SEC)
                continue

            self._cycle += 1
            await self.Piezo_Cycle.write(self._cycle)

            if self._opc is None:
                LOGGER.warning('OPC UA disconnected, skipping cycle')
                await sleep(hiaf_config.PI_POLL_SEC)
                continue

            try:
                a1 = await self._safe_read_a1()
                self._fail_count = 0
                self._a1_from_opc = a1
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

            elapsed = time.monotonic() - loop_start
            remaining = hiaf_config.PI_POLL_SEC - elapsed
            if remaining > 0:
                await sleep(remaining)

    # ── PI cycle: existing velocity-form PI ──
    async def _pi_cycle(self, sp_val, a1, error) -> None:
        """Velocity-form PI（保留分级死区、抗积分饱和、压力速率抑制）."""
        kp_val = self._kp_val
        ki_val = self._ki_val
        kd_val = self._kd_val

        try:
            current_v = float(await self._valve_node.read_value())
        except Exception:
            current_v = float(self.Piezo_ValveSP.value)

        ff_valve = hiaf_config.feedforward_valve(sp_val)
        current_trim = current_v - ff_valve
        now = time.monotonic()

        error_sign = 1 if error > 0 else -1 if error < 0 else 0
        if error_sign != 0 and self._last_error_sign != 0 and error_sign != self._last_error_sign:
            self._last_sign_change = now
        if error_sign != 0:
            self._last_error_sign = error_sign

        # 小误差：不调
        if abs(error) < 3:
            self._last_error = error
            self._last_a1 = a1
            self._last_a1_for_d = a1
            await self.Piezo_Error.write(error)
            await self.Piezo_Delta.write(0.0)
            return

        derror = error - self._last_error
        p_term = kp_val * derror
        i_term = ki_val * error * hiaf_config.PI_POLL_SEC

        # D term: PV微分 + 低通滤波
        dpv = a1 - (self._last_a1_for_d if self._last_a1_for_d is not None else a1)
        self._last_a1_for_d = a1
        d_alpha = 0.1
        self._filtered_dpv = d_alpha * dpv + (1 - d_alpha) * self._filtered_dpv
        d_term = -kd_val * self._filtered_dpv

        # 分级积分死区（反向：误差越大死区越短）
        ae = abs(error)
        if ae > 30:
            dz = 5.0
        elif ae > 10:
            dz = 15.0
        else:
            dz = 30.0
        if now - self._last_sign_change < dz:
            i_term = 0.0

        # 抗积分饱和
        blocked_low = current_v <= hiaf_config.VALVE_MIN and i_term < 0
        blocked_high = current_v >= hiaf_config.VALVE_MAX and i_term > 0
        trim_low = current_trim <= -hiaf_config.VALVE_TRIM_MAX and i_term < 0
        trim_high = current_trim >= hiaf_config.VALVE_TRIM_MAX and i_term > 0
        if blocked_low or blocked_high or trim_low or trim_high:
            i_term = 0.0

        delta = p_term + i_term + d_term
        if self._last_a1 is not None:
            a1_rate = (a1 - self._last_a1) / hiaf_config.PI_POLL_SEC
            if (a1_rate > hiaf_config.PRESSURE_RATE_MAX and delta > 0) or \
               (a1_rate < -hiaf_config.PRESSURE_RATE_MAX and delta < 0):
                delta *= hiaf_config.PRESSURE_RATE_DAMP
        self._last_a1 = a1
        delta = max(-hiaf_config.VALVE_RATE_MAX, min(hiaf_config.VALVE_RATE_MAX, delta))
        self._last_error = error

        await self.Piezo_Error.write(error)
        await self.Piezo_Delta.write(delta)

        new_trim = current_trim + delta
        new_trim = max(-hiaf_config.VALVE_TRIM_MAX, min(hiaf_config.VALVE_TRIM_MAX, new_trim))
        new_valve = ff_valve + new_trim
        new_valve = max(hiaf_config.VALVE_MIN, min(hiaf_config.VALVE_MAX, new_valve))
        await self.Piezo_ValveSP.write(new_valve)

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
        """Startup: connect OPC UA, launch sensor poll + PI control loops."""
        sleep = async_lib.library.sleep
        LOGGER.info("HiafGasCellIOC starting up...")

        # Initialize SQLite database via storage
        await self._storage.init_db()

        # Launch sensor poll background task
        asyncio.create_task(self._sensor_poll_loop())
        LOGGER.info("Sensor poll loop started (~%g Hz)", 1.0 / hiaf_config.SENSOR_POLL_SEC)

        # Run PI control loop in the caproto async context
        await self._pi_control_loop(sleep)

    # ── PV putter handlers (cache to instance vars — NO .read() calls) ──

    @Piezo_Running.putter
    async def Piezo_Running(self, instance, value):
        if int(value) == 1 and self._a5_tripped:
            LOGGER.warning("Cannot start PI — A5 safety trip active")
            return 0
        self._running = bool(int(value))
        if self._running:
            self._last_error = 0.0
            self._last_error_sign = 0
            self._last_sign_change = 0.0
            self._cycle = 0
            await self.Piezo_Cycle.write(0)
            ff_valve = hiaf_config.feedforward_valve(self._sp_val)
            await self.Piezo_ValveSP.write(ff_valve)
            LOGGER.info("PI control STARTED (target=%dPa ff_valve=%.1f)", self._sp_val, ff_valve)
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

    @Piezo_Kd.putter
    async def Piezo_Kd(self, instance, value):
        self._kd_val = float(value)
        return self._kd_val

    # ── Shutdown ──
    @Piezo_Running.shutdown
    async def Piezo_Running(self, instance, async_lib):
        self._running = False
        if self._opc is not None:
            try:
                await self._opc.disconnect()
                LOGGER.info("OPC UA disconnected")
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
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )

    ioc = HiafGasCellIOC(prefix=args.prefix)

    if args.list_pvs:
        for pvname in sorted(ioc.pvdb):
            print(pvname)
        return

    run(ioc.pvdb)


if __name__ == "__main__":
    main()
