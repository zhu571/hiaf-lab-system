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
import os
import time
from bisect import bisect_left
from datetime import datetime
from typing import Any
from urllib.request import urlopen
from urllib.parse import quote

from caproto import AlarmSeverity, AlarmStatus
from caproto.server import PVGroup, pvproperty, run

import aiosqlite
from asyncua import Client
from pathlib import Path
from influxdb_client import InfluxDBClient, Point
from influxdb_client.client.write_api import SYNCHRONOUS

LOGGER = logging.getLogger(__name__)

# ── OPC UA ──
OPC_URL = "opc.tcp://10.51.12.158:4862"

# InfluxDB
import os as _os, pathlib as _pl
def _read_secret(name, default=''):
    sf = _os.getenv(f'{name}_FILE', '')
    if sf:
        try: return _pl.Path(sf).read_text(encoding='utf-8').strip()
        except OSError as e: print(f'WARN: {name}_FILE={sf}: {e}', flush=True)
    return _os.getenv(name, default)
INFLUX_URL = _os.getenv('INFLUX_URL', 'http://localhost:8086')
INFLUX_TOKEN = _read_secret('INFLUX_TOKEN')
INFLUX_ORG = _os.getenv('INFLUX_ORG', 'lab-org')
INFLUX_BUCKET = _os.getenv('INFLUX_BUCKET', 'lab-bucket')
INFLUX_WRITE_SEC = 10.0
MEOW_NAME = os.getenv("MEOW_NAME", "")
VALVE_NODE_ID = "ns=1;s=t|控制及设定_压电阀开度设定"

# ── Feedforward calibration (A1 target → valve) ──
# From reverse calibration 2026-07-12, linear interpolation
_FF_A1 = [300, 400, 500, 600, 700, 800, 900, 1000, 1100, 1200, 1300, 1400, 1500, 1600]
_FF_VALVE = [40.0, 43.5, 47.6, 48.8, 50.3, 51.5, 52.6, 56.0, 55.2, 55.6, 56.0, 56.3, 56.6, 56.9]

def feedforward_valve(a1_target):
    """Linear-interpolated valve opening for target A1 pressure."""
    if a1_target <= _FF_A1[0]: return _FF_VALVE[0]
    if a1_target >= _FF_A1[-1]: return _FF_VALVE[-1]
    i = bisect_left(_FF_A1, a1_target)
    a0, a1 = _FF_A1[i - 1], _FF_A1[i]
    v0, v1 = _FF_VALVE[i - 1], _FF_VALVE[i]
    return v0 + (v1 - v0) * (a1_target - a0) / (a1 - a0)

# ── Tag → PV-name mappings (27 sensor channels) ──
# fmt: (OPC_UA_tag, PV_name_suffix)
TEMP_TAGS: list[tuple[str, str]] = [
    ("218数据_T1", "Temp:T1"),
    ("218数据_T2", "Temp:T2"),
    ("218数据_T3", "Temp:T3"),
    ("218数据_T6", "Temp:T6"),
    ("218数据_T7", "Temp:T7"),
    ("218数据_T8", "Temp:T8"),
    ("218数据_T9", "Temp:T9"),
    ("218数据_T10", "Temp:T10"),
    ("218数据_PT1000-1", "Temp:PT1000_1"),
    ("218数据_PT1000-2", "Temp:PT1000_2"),
    ("218数据_PT1000-3", "Temp:PT1000_3"),
    ("218数据_PT1000-4", "Temp:PT1000_4"),
    ("218数据_空", "Temp:T4"),
    ("218数据_空_1", "Temp:T5"),
]

PRESS_TAGS: list[tuple[str, str]] = [
    ("直采数据_P1", "Press:P1"),
    ("直采数据_P2", "Press:P2"),
    ("直采数据_P3", "Press:P3"),
    ("直采数据_P4", "Press:P4"),
    ("直采数据_P6", "Press:P6"),
    ("直采数据_P8", "Press:P8"),
]

VAC_TAGS: list[tuple[str, str]] = [
    ("直采数据_A1", "Vac:A1"),
    ("直采数据_A2", "Vac:A2"),
    ("直采数据_A4", "Vac:A4"),
    ("直采数据_A5", "Vac:A5"),
    ("直采数据_A6", "Vac:A6"),
    ("直采数据_A7", "Vac:A7"),
    ("直采数据_A8", "Vac:A8"),
]

ALL_SENSOR_TAGS: list[tuple[str, str]] = TEMP_TAGS + PRESS_TAGS + VAC_TAGS  # 27


PUMP_TAGS = [
    # ── 分子泵 DP (4台) ──
    ("分子泵DP1运行",   "pump", "DP1_Run"),
    ("分子泵DP1报警",   "pump", "DP1_Alarm"),
    ("分子泵DP1启动",   "pump", "DP1_Start"),
    ("分子泵DP1复位",   "pump", "DP1_Reset"),
    ("分子泵DP1数据启用", "pump", "DP1_DataEnable"),
    ("分子泵DP2运行",   "pump", "DP2_Run"),
    ("分子泵DP2报警",   "pump", "DP2_Alarm"),
    ("分子泵DP2启动",   "pump", "DP2_Start"),
    ("分子泵DP2复位",   "pump", "DP2_Reset"),
    ("分子泵DP2数据启用", "pump", "DP2_DataEnable"),
    ("分子泵DP3运行",   "pump", "DP3_Run"),
    ("分子泵DP3报警",   "pump", "DP3_Alarm"),
    ("分子泵DP3启动",   "pump", "DP3_Start"),
    ("分子泵DP3复位",   "pump", "DP3_Reset"),
    ("分子泵DP3数据启用", "pump", "DP3_DataEnable"),
    ("分子泵DP4运行",   "pump", "DP4_Run"),
    ("分子泵DP4报警",   "pump", "DP4_Alarm"),
    ("分子泵DP4启动",   "pump", "DP4_Start"),
    ("分子泵DP4复位",   "pump", "DP4_Reset"),
    ("分子泵DP4数据启用", "pump", "DP4_DataEnable"),
    # ── 分子泵DP数据 (4台) ──
    ("分子泵DP数据_1#报警", "pump", "DP_Data_1_Alarm"),
    ("分子泵DP数据_1#运行", "pump", "DP_Data_1_Run"),
    ("分子泵DP数据_1#开关", "pump", "DP_Data_1_Switch"),
    ("分子泵DP数据_1#复位", "pump", "DP_Data_1_Reset"),
    ("分子泵DP数据_2#报警", "pump", "DP_Data_2_Alarm"),
    ("分子泵DP数据_2#运行", "pump", "DP_Data_2_Run"),
    ("分子泵DP数据_2#开关", "pump", "DP_Data_2_Switch"),
    ("分子泵DP数据_2#复位", "pump", "DP_Data_2_Reset"),
    ("分子泵DP数据_3#报警", "pump", "DP_Data_3_Alarm"),
    ("分子泵DP数据_3#运行", "pump", "DP_Data_3_Run"),
    ("分子泵DP数据_3#开关", "pump", "DP_Data_3_Switch"),
    ("分子泵DP数据_3#复位", "pump", "DP_Data_3_Reset"),
    ("分子泵DP数据_4#报警", "pump", "DP_Data_4_Alarm"),
    ("分子泵DP数据_4#运行", "pump", "DP_Data_4_Run"),
    ("分子泵DP数据_4#开关", "pump", "DP_Data_4_Switch"),
    ("分子泵DP数据_4#复位", "pump", "DP_Data_4_Reset"),
    # ── 850i 分子泵 (2台) ──
    ("分子泵自由口数据_850i_1运行",   "pump", "DP850i_1_Run"),
    ("分子泵自由口数据_850i_1报警",   "pump", "DP850i_1_Alarm"),
    ("分子泵自由口数据_850i_1开关机",  "pump", "DP850i_1_Switch"),
    ("分子泵自由口数据_850i_1复位",   "pump", "DP850i_1_Reset"),
    ("分子泵自由口数据_850i_1工作状态", "pump", "DP850i_1_Status"),
    ("分子泵自由口数据_850i_2运行",   "pump", "DP850i_2_Run"),
    ("分子泵自由口数据_850i_2报警",   "pump", "DP850i_2_Alarm"),
    ("分子泵自由口数据_850i_2开关机",  "pump", "DP850i_2_Switch"),
    ("分子泵自由口数据_850i_2复位",   "pump", "DP850i_2_Reset"),
    ("分子泵自由口数据_850i_2工作状态", "pump", "DP850i_2_Status"),
    # 850i 分子泵前缀变体
    ("分子泵_分子泵自由口数据_850i_1运行", "pump", "DP850i_1_Run_v2"),
    ("分子泵_分子泵自由口数据_850i_1报警", "pump", "DP850i_1_Alarm_v2"),
    ("分子泵_分子泵自由口数据_850i_2运行", "pump", "DP850i_2_Run_v2"),
    ("分子泵_分子泵自由口数据_850i_2报警", "pump", "DP850i_2_Alarm_v2"),
    # ── 循环泵 (2台) ──
    ("循环泵变频器运行中", "pump", "CircPump1_Running"),
    ("循环泵变频器报警",  "pump", "CircPump1_Alarm"),
    ("循环泵变频器复位",  "pump", "CircPump1_Reset"),
    ("循环泵变频器启动",  "pump", "CircPump1_Start"),
    ("循环泵2变频器报警",  "pump", "CircPump2_Alarm"),
    ("循环泵2频率读取",   "pump", "CircPump2_FreqRead"),
    ("循环泵2频率设定",   "pump", "CircPump2_FreqSet"),
    ("循环泵2复位",      "pump", "CircPump2_Reset"),
    ("循环泵2启动",      "pump", "CircPump2_Start"),
    # ── 干泵 (5台) ──
    ("干泵1启停", "pump", "DryPump1_StartStop"),
    ("干泵1使能", "pump", "DryPump1_Enable"),
    ("干泵2启停", "pump", "DryPump2_StartStop"),
    ("干泵2使能", "pump", "DryPump2_Enable"),
    ("干泵3启停", "pump", "DryPump3_StartStop"),
    ("干泵3使能", "pump", "DryPump3_Enable"),
    ("干泵4启停", "pump", "DryPump4_StartStop"),
    ("干泵4使能", "pump", "DryPump4_Enable"),
    ("干泵5启停", "pump", "DryPump5_StartStop"),
    ("干泵5使能", "pump", "DryPump5_Enable"),
    # ── 离子泵 ──
    ("离子泵数据_离子泵运行状态",     "pump", "IonPump_RunStatus"),
    ("离子泵数据_离子泵故障状态",     "pump", "IonPump_FaultStatus"),
    ("离子泵数据_离子泵烘烤状态",     "pump", "IonPump_BakeStatus"),
    ("离子泵数据_离子泵遥控状态",     "pump", "IonPump_RemoteStatus"),
    ("离子泵数据_离子泵6KV高压状态",  "pump", "IonPump_HV6KV_Status"),
    ("离子泵数据_离子泵4KV高压状态",  "pump", "IonPump_HV4KV_Status"),
    # ── 压缩机 (8台) ──
    ("万瑞压缩机1温度报警",       "pump", "WR_Comp1_TempAlarm"),
    ("万瑞压缩机2温度报警",       "pump", "WR_Comp2_TempAlarm"),
    ("万瑞压缩机3温度报警",       "pump", "WR_Comp3_TempAlarm"),
    ("万瑞压缩机4温度报警",       "pump", "WR_Comp4_TempAlarm"),
    ("住友压缩机1气体温度错误",    "pump", "ZYK_Comp1_GasTempErr"),
    ("住友压缩机1电机温度报警",    "pump", "ZYK_Comp1_MotorTempAlarm"),
    ("住友压缩机2气体温度错误",    "pump", "ZYK_Comp2_GasTempErr"),
    ("住友压缩机2电机温度报警",    "pump", "ZYK_Comp2_MotorTempAlarm"),
    ("住友压缩机3气体温度错误",    "pump", "ZYK_Comp3_GasTempErr"),
    ("住友压缩机3电机温度报警",    "pump", "ZYK_Comp3_MotorTempAlarm"),
    ("住友压缩机4气体温度错误",    "pump", "ZYK_Comp4_GasTempErr"),
    ("住友压缩机4电机温度报警",    "pump", "ZYK_Comp4_MotorTempAlarm"),
    # ── 低温循环泵压力 ──
    ("低温循环泵出口压力", "pump", "CryoCircPump_OutletPress"),
    ("低温循环泵入口压力", "pump", "CryoCircPump_InletPress"),
]

# ── PI control parameters ──
SENSOR_POLL_SEC = 1.0
PI_POLL_SEC = 0.1
DEFAULT_SETPOINT = 1500.0
DEFAULT_KP = 0.006
DEFAULT_KI = 0.00018
VALVE_MIN = 45.0
VALVE_MAX = 100.0
VALVE_RATE_MAX = 3.0          # max valve change per cycle
VALVE_TRIM_MAX = 30.0          # max PI trim around feedforward valve
PRESSURE_RATE_MAX = 10.0     # Pa/s - damp delta if A1 moves faster than this
PRESSURE_RATE_DAMP = 0.15   # damping factor when pressure moves too fast
MAX_CONSECUTIVE_FAILURES = 5

# ── SQLite logging ──
DB_PATH = Path.home() / "work" / "hiaf-plc-agent" / "sensor_history.db"
SQLITE_FLUSH_SEC = 30.0          # batch-write every 30s regardless
SENSOR_CHANGE_REL = 0.005        # 0.5% relative change threshold
SENSOR_CHANGE_ABS = 0.1          # minimum absolute change threshold (for near-zero)


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
        name="Piezo:Setpoint", value=DEFAULT_SETPOINT, dtype=float,
        doc="目标气压 (Pa)",
    )
    Piezo_Kp = pvproperty(
        name="Piezo:Kp", value=DEFAULT_KP, dtype=float,
        doc="比例增益",
    )
    Piezo_Ki = pvproperty(
        name="Piezo:Ki", value=DEFAULT_KI, dtype=float,
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

        # Tag → OPC UA node mapping (built after connect)
        self._sensor_nodes: dict[str, Any] = {}

        # Tag → pvproperty mapping for sensor poll
        self._sensor_pvs: dict[str, Any] = {}
        for tag, pv_name in ALL_SENSOR_TAGS:
            attr_name = pv_name.replace(":", "_")      # "Temp:T1" → "Temp_T1"
            self._sensor_pvs[tag] = getattr(self, attr_name)

        # Sensor poll cache (for fast readout)
        self._sensor_values: dict[str, float] = {tag: 0.0 for tag, _ in ALL_SENSOR_TAGS}

        # Subscription health (not used in poll-only mode; False = always write)
        self._subscription_healthy: bool = False

        # Safety state
        self._a5_tripped = False
        self._a5_max = 10.0

        # PI control state
        self._last_error = 0.0
        self._last_error_sign = 0
        self._last_sign_change = 0.0
        self._cycle = 0
        self._running = False
        self._valve_rate_max = VALVE_RATE_MAX  # adjustable per-instance

        # PI parameter cache (updated by putters)
        self._sp_val = DEFAULT_SETPOINT
        self._kp_val = DEFAULT_KP
        self._ki_val = DEFAULT_KI

        # Failure tracking
        self._fail_count = 0

        # A1 value cache (from OPC UA, shared between sensor poll and PI)
        self._a1_from_opc = 0.0
        self._last_a1 = None  # for pressure rate suppression

        # ── InfluxDB client ──
        self._influx_client = None
        self._influx_write_api = None
        self._last_influx_write = 0.0
        try:
            self._influx_client = InfluxDBClient(
                url=INFLUX_URL, token=INFLUX_TOKEN, org=INFLUX_ORG)
            self._influx_write_api = self._influx_client.write_api(
                write_options=SYNCHRONOUS)
            LOGGER.info('InfluxDB connected — %s', INFLUX_URL)
        except Exception as e:
            LOGGER.warning('InfluxDB init failed: %s', e)

        # ── Pump OPC UA cache ──
        self._pump_nodes: dict = {}
        self._pump_values: dict = {}
        self._active_pump_tags: list = []  # populated on first connect

        # ── SQLite logging state ──
        self._db: aiosqlite.Connection | None = None
        self._last_db_write: float = 0.0
        self._last_written_values: dict[str, float] = {tag: 0.0 for tag, _ in ALL_SENSOR_TAGS}

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
            # Actually verify the connection is alive by reading a standard
            # OPC UA server node (ServerStatus/CurrentTime: i=2258).
            try:
                test_node = self._opc.get_node("i=2258")
                await asyncio.wait_for(test_node.read_value(), timeout=5.0)
                return True
            except Exception:
                LOGGER.warning("OPC UA connection lost — reconnecting...")
                # Connection is dead; close old client
                try:
                    await self._opc.disconnect()
                except Exception:
                    pass
                self._opc = None
                self._valve_node = None
                self._sensor_nodes.clear()

        # (Re)connect
        try:
            self._opc = Client(OPC_URL, timeout=10)
            await self._opc.connect()
            self._valve_node = self._opc.get_node(VALVE_NODE_ID)
            # Rebuild all sensor node references (old handles are invalid)
            for tag, _ in ALL_SENSOR_TAGS:
                node_id = f"ns=1;s=t|{tag}"
                self._sensor_nodes[tag] = self._opc.get_node(node_id)
            # Also rebuild pump nodes
            self._pump_nodes.clear()
            for opc_tag, meas, ftag in PUMP_TAGS:
                node_id = f"ns=1;s=t|{opc_tag}"
                self._pump_nodes[opc_tag] = self._opc.get_node(node_id)
                # Only add to active list — verified on first read
            self._active_pump_tags = [tag for tag in list(self._pump_nodes.keys())[:20]]  # start with 20 most common
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

    # ── SQLite initialization ──────────────────────
    async def _init_db(self) -> None:
        """Open aiosqlite connection and create sensor_data table if needed."""
        db_path = str(DB_PATH)
        try:
            self._db = await aiosqlite.connect(db_path)
            await self._db.execute("""
                CREATE TABLE IF NOT EXISTS sensor_data (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    timestamp TEXT NOT NULL,
                    tag_name TEXT NOT NULL,
                    value REAL,
                    UNIQUE(timestamp, tag_name)
                )
            """)
            await self._db.execute(
                "CREATE INDEX IF NOT EXISTS idx_ts ON sensor_data(timestamp)"
            )
            await self._db.execute(
                "CREATE INDEX IF NOT EXISTS idx_tag ON sensor_data(tag_name)"
            )
            await self._db.commit()
            LOGGER.info("SQLite DB ready — %s", db_path)
        except Exception as e:
            LOGGER.error("SQLite init failed: %s", e)
            self._db = None

    # ── SQLite threshold-based write ───────────────
    def _sensor_value_changed(self, tag: str) -> bool:
        """Check if a sensor value changed significantly since last DB write."""
        current = self._sensor_values.get(tag, 0.0)
        last = self._last_written_values.get(tag, 0.0)
        abs_diff = abs(current - last)
        # Use relative threshold, with absolute minimum for near-zero values
        rel_thresh = max(abs(last), 1.0) * SENSOR_CHANGE_REL
        return abs_diff > max(rel_thresh, SENSOR_CHANGE_ABS)

    async def _maybe_write_sensors(self) -> None:
        """Batch-write all 27 tags to SQLite when threshold exceeded or 60s elapsed."""
        if self._db is None:
            return

        now = time.monotonic()
        time_since_last = now - self._last_db_write

        # First write always happens immediately
        if self._last_db_write == 0.0:
            should_write = True
        else:
            any_changed = any(
                self._sensor_value_changed(tag) for tag, _ in ALL_SENSOR_TAGS
            )
            should_write = any_changed or time_since_last >= SQLITE_FLUSH_SEC

        if not should_write:
            return

        ts = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime())
        rows = [
            (ts, tag, self._sensor_values.get(tag, 0.0))
            for tag, _ in ALL_SENSOR_TAGS
        ]

        try:
            await self._db.executemany(
                "INSERT OR IGNORE INTO sensor_data (timestamp, tag_name, value) VALUES (?, ?, ?)",
                rows,
            )
            await self._db.commit()
            self._last_db_write = now
            # Track last written values for change detection
            for tag, _ in ALL_SENSOR_TAGS:
                self._last_written_values[tag] = self._sensor_values.get(tag, 0.0)
        except Exception as e:
            LOGGER.warning("SQLite write failed: %s", e)

    # ── Background task 1: Sensor poll loop ~1Hz ──
    # ── InfluxDB write (sync, runs in executor) ──
    def _flush_influx(self, points: list) -> bool:
        if self._influx_write_api is None:
            return False
        try:
            self._influx_write_api.write(
                bucket=INFLUX_BUCKET, org=INFLUX_ORG, record=points)
            return True
        except Exception as e:
            LOGGER.warning(f'InfluxDB write fail: {e}')
            return False

    async def _maybe_write_influx(self) -> None:
        if self._influx_write_api is None:
            return
        now = time.monotonic()
        if now - self._last_influx_write < INFLUX_WRITE_SEC:
            return
        loop = asyncio.get_event_loop()
        points = []
        # Sensor points
        for tag, pv_name in ALL_SENSOR_TAGS:
            val = self._sensor_values.get(tag, 0)
            if val != val:  # NaN check — NaN != NaN is True
                continue
            grp = pv_name.split(':')[0].lower()
            meas = {'temp': 'temperature', 'press': 'pressure', 'vac': 'vacuum'}.get(grp, 'unknown')
            points.append(Point(meas).tag('tag', pv_name.split(':')[-1]).tag('location', 'lab').field('value', float(val)))
        # PI control
        points.append(Point('control').tag('tag', 'ValveSP').tag('location', 'lab').field('value', float(self.Piezo_ValveSP.value)))
        points.append(Point('control').tag('tag', 'Setpoint').tag('location', 'lab').field('value', float(self._sp_val)))
        points.append(Point('control').tag('tag', 'Error').tag('location', 'lab').field('value', float(self._last_error)))
        # Pump
        for opc_tag, meas, ftag in PUMP_TAGS:
            val = self._pump_values.get(opc_tag)
            if val is not None and val == val:  # skip None and NaN
                points.append(Point(meas).tag('tag', ftag).tag('location', 'lab').field('value', float(val)))
        try:
            ok = await loop.run_in_executor(None, self._flush_influx, points)
            if ok:
                self._last_influx_write = now
        except Exception as e:
            LOGGER.warning(f'InfluxDB write failed: {e}')

    async def _read_pump_tags(self) -> None:
        if not self._pump_nodes:
            return
        # Only read pumps likely to be active (DP3, DP4, 循环泵, 压缩机)
        active_keys = [k for k in self._pump_nodes if any(s in k for s in ['DP3','DP4','循环泵','压缩机','低温循环泵'])]
        if not active_keys:
            return
        try:
            tasks = [asyncio.wait_for(self._pump_nodes[k].read_value(), timeout=1.0) for k in active_keys]
            vals = await asyncio.gather(*tasks, return_exceptions=True)
            for k, v in zip(active_keys, vals):
                if not isinstance(v, Exception) and v is not None:
                    self._pump_values[k] = float(v)
        except Exception:
            pass

    async def _sensor_poll_loop(self) -> None:
        """Poll all 27 sensor OPC UA nodes at ~1Hz, update PVs."""
        while True:
            loop_start = time.monotonic()
            try:
                if self._opc is None or self._valve_node is None:
                    await self._ensure_connected()
                    if self._opc is None:
                        await asyncio.sleep(SENSOR_POLL_SEC)
                        continue

                # When subscription is healthy, skip PV writes (only storage + safety)
                sub_healthy = self._subscription_healthy

                # Read all 27 sensors in parallel
                tasks = [
                    asyncio.wait_for(self._safe_read_node(node), timeout=2.0)
                    for node in self._sensor_nodes.values()
                ]
                results = await asyncio.gather(*tasks, return_exceptions=True)

                # Update PVs
                for (tag, _), val_or_err in zip(ALL_SENSOR_TAGS, results):
                    if isinstance(val_or_err, Exception):
                        self._sensor_values[tag] = float('nan')
                        continue
                    if val_or_err is not None:
                        self._sensor_values[tag] = float(val_or_err)
                        if not sub_healthy:
                            pv = self._sensor_pvs[tag]
                            try:
                                await pv.write(float(val_or_err))
                            except Exception:
                                pass

                # Also update Piezo:A1 with the Vac:A1 reading
                a1_val = self._sensor_values.get("直采数据_A1", 0.0)
                self._a1_from_opc = a1_val
                if not sub_healthy:
                    try:
                        await self.Vac_A1.write(a1_val)
                    except Exception:
                        pass

                # SQLite batch-write (threshold-based or every 60s)
                await self._maybe_write_sensors()

                await self._set_connected(True)

                # Read pump tags & write to InfluxDB
                await self._read_pump_tags()
                await self._maybe_write_influx()

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
                    if MEOW_NAME:
                        try:
                            msg = f"A5超压 {a5_val:.1f}Pa 阀门已关闭"
                            await asyncio.to_thread(
                                urlopen,
                                f"https://api.chuckfang.com/{MEOW_NAME}/A5超压/{quote(msg)}",
                                timeout=5,
                            )
                        except Exception:
                            pass

            except Exception as e:
                LOGGER.warning("Sensor poll cycle error: %s", e)
                await self._set_connected(self._opc is not None)

            elapsed = time.monotonic() - loop_start
            remaining = SENSOR_POLL_SEC - elapsed
            if remaining > 0:
                await asyncio.sleep(remaining)

    # ── Background task 2: PI control loop ~10Hz ──
    async def _pi_control_loop(self, sleep) -> None:
        """PI velocity-form control loop at ~10Hz, only active when Running=1."""
        while True:
            loop_start = time.monotonic()

            if not self._running:
                # Idle: just update Piezo:A1 from cached sensor value
                try:
                    await self.Piezo_A1.write(self._a1_from_opc)
                except Exception:
                    pass
                await self._set_connected(self._opc is not None)
                await sleep(PI_POLL_SEC)
                continue

            # ── Running: execute PI control cycle ──
            self._cycle += 1
            await self.Piezo_Cycle.write(self._cycle)

            # Ensure OPC UA connection is alive before using any nodes
            if self._opc is None:
                LOGGER.warning('PI: OPC UA disconnected, attempting reconnect')
                await self._ensure_connected()
                if self._opc is None:
                    await sleep(PI_POLL_SEC)
                    continue

            try:
                # 1. Read A1 from OPC UA (direct, fast)
                a1 = await self._safe_read_a1()
                self._fail_count = 0
                self._a1_from_opc = a1
                await self.Piezo_A1.write(a1)
                await self._set_connected(True)

                # 2. PI calculation (velocity form) — use cached putter values
                sp_val = self._sp_val
                kp_val = self._kp_val
                ki_val = self._ki_val

                now = time.monotonic()

                # 设定值大跨度切换软启：前60s弱比例控制，关闭积分
                last_sp = getattr(self, '_last_pi_sp', sp_val)
                if abs(sp_val - last_sp) > 50:
                    self._soft_until = now + 60.0
                self._last_pi_sp = sp_val
                if now < getattr(self, '_soft_until', 0):
                    kp_val *= 0.3
                    ki_val = 0.0

                try:
                    current_v = float(await self._valve_node.read_value())
                except Exception:
                    current_v = float(self.Piezo_ValveSP.value)

                ff_valve = feedforward_valve(sp_val)
                current_trim = current_v - ff_valve

                error = sp_val - a1
                error_sign = 1 if error > 0 else -1 if error < 0 else 0
                if (
                    error_sign != 0
                    and self._last_error_sign != 0
                    and error_sign != self._last_error_sign
                ):
                    self._last_sign_change = now
                if error_sign != 0:
                    self._last_error_sign = error_sign

                # 小误差死区：稳态|error|<3Pa跳过，不频繁微调阀门
                if abs(error) < 3:
                    self._last_error = error
                    self._last_a1 = a1
                    await self.Piezo_Error.write(error)
                    await self.Piezo_Delta.write(0.0)
                    elapsed = time.monotonic() - loop_start
                    remaining = PI_POLL_SEC - elapsed
                    if remaining > 0:
                        await sleep(remaining)
                    continue

                derror = error - self._last_error
                p_term = kp_val * derror
                i_term = ki_val * error * PI_POLL_SEC
                # 死区按|error|分级：小偏差快响应，大偏差保留阻尼
                ae = abs(error)
                if ae < 10:
                    dz = 20.0
                elif ae < 30:
                    dz = 60.0
                else:
                    dz = 120.0
                if now - self._last_sign_change < dz:
                    i_term = 0.0

                blocked_low = current_v <= VALVE_MIN and i_term < 0
                blocked_high = current_v >= VALVE_MAX and i_term > 0
                trim_low = current_trim <= -VALVE_TRIM_MAX and i_term < 0
                trim_high = current_trim >= VALVE_TRIM_MAX and i_term > 0
                if blocked_low or blocked_high or trim_low or trim_high:
                    i_term = 0.0

                delta = p_term + i_term
                # ── Pressure rate suppression: damp if A1 rising too fast ──
                if self._last_a1 is not None:
                    a1_rate = (a1 - self._last_a1) / PI_POLL_SEC
                    if (
                        (a1_rate > PRESSURE_RATE_MAX and delta > 0)
                        or (a1_rate < -PRESSURE_RATE_MAX and delta < 0)
                    ):
                        delta *= PRESSURE_RATE_DAMP
                self._last_a1 = a1
                # ── Slew rate limiter ──
                delta = max(-VALVE_RATE_MAX, min(VALVE_RATE_MAX, delta))
                self._last_error = error

                await self.Piezo_Error.write(error)
                await self.Piezo_Delta.write(delta)
                # 3. Compute feedforward + bounded PI trim setpoint
                new_trim = current_trim + delta
                new_trim = max(-VALVE_TRIM_MAX, min(VALVE_TRIM_MAX, new_trim))
                new_valve = ff_valve + new_trim
                new_valve = max(VALVE_MIN, min(VALVE_MAX, new_valve))

                # 4. Write valve setpoint to OPC UA
                await self.Piezo_ValveSP.write(new_valve)

            except Exception as e:
                self._fail_count += 1
                LOGGER.warning(
                    "PI cycle %d failed (%d/%d): %s",
                    self._cycle, self._fail_count,
                    MAX_CONSECUTIVE_FAILURES, e,
                )
                if self._fail_count >= MAX_CONSECUTIVE_FAILURES:
                    LOGGER.error("Max consecutive PI failures reached — auto-stop")
                    self._running = False
                    await self.Piezo_Running.write(0)
                    await self._set_connected(False)

            # Maintain 10Hz period
            elapsed = time.monotonic() - loop_start
            remaining = PI_POLL_SEC - elapsed
            if remaining > 0:
                await sleep(remaining)

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

        if self._opc is None or self._valve_node is None:
            await self._set_connected(False)
            LOGGER.error("OPC UA connection failed at startup — sensor poll disabled")
        else:
            await self._set_connected(True)

        # Initialize SQLite database
        await self._init_db()

        # Launch sensor poll background task
        asyncio.create_task(self._sensor_poll_loop())
        LOGGER.info("Sensor poll loop started (~%g Hz)", 1.0 / SENSOR_POLL_SEC)

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
            # Feedforward: set valve to calibrated starting position
            ff_valve = feedforward_valve(self._sp_val)
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
        val = max(0.0, min(VALVE_MAX, val))
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
        if self._opc is not None:
            try:
                await self._opc.disconnect()
                LOGGER.info("OPC UA disconnected")
            except Exception:
                pass
        if self._db is not None:
            try:
                await self._db.close()
                LOGGER.info("SQLite DB closed")
            except Exception:
                pass


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

    async def _startup(_server):
        # Only connect OPC UA here — loops are started by Piezo_Running.startup
        await ioc._ensure_connected()

    run(ioc.pvdb, startup_hook=_startup)


if __name__ == "__main__":
    main()
