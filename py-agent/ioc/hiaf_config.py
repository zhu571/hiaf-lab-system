import os
from bisect import bisect_left
from pathlib import Path

# ── OPC UA ──
OPC_URL = "opc.tcp://10.51.12.158:4862"
VALVE_NODE_ID = "ns=1;s=t|控制及设定_压电阀开度设定"


# InfluxDB
def _read_secret(name, default=''):
    sf = os.getenv(f'{name}_FILE', '')
    if sf:
        try:
            return Path(sf).read_text(encoding='utf-8').strip()
        except OSError as e:
            print(f'WARN: {name}_FILE={sf}: {e}', flush=True)
    return os.getenv(name, default)


INFLUX_URL = os.getenv('INFLUX_URL', 'http://localhost:8086')
INFLUX_TOKEN = _read_secret('INFLUX_TOKEN')
INFLUX_ORG = os.getenv('INFLUX_ORG', 'lab-org')
INFLUX_BUCKET = os.getenv('INFLUX_BUCKET', 'lab-bucket')
INFLUX_WRITE_SEC = 10.0
MEOW_NAME = os.getenv("MEOW_NAME", "")

# ── Feedforward calibration (A1 target → valve) ──
# From reverse calibration 2026-07-12, linear interpolation
FF_A1 = [300, 400, 500, 600, 700, 800, 900, 1000, 1100, 1200, 1300, 1400, 1500, 1600]
FF_VALVES = [40.0, 43.5, 47.6, 48.8, 50.3, 51.5, 52.6, 56.0, 55.2, 55.6, 56.0, 56.3, 56.6, 56.9]


def feedforward_valve(a1_target):
    """Linear-interpolated valve opening for target A1 pressure."""
    if a1_target <= FF_A1[0]:
        return FF_VALVES[0]
    if a1_target >= FF_A1[-1]:
        return FF_VALVES[-1]
    i = bisect_left(FF_A1, a1_target)
    a0, a1 = FF_A1[i - 1], FF_A1[i]
    v0, v1 = FF_VALVES[i - 1], FF_VALVES[i]
    return v0 + (v1 - v0) * (a1_target - a0) / (a1 - a0)


# ── Tag → PV-name mappings ──
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
    ("分子泵DP1运行", "pump", "DP1_Run"),
    ("分子泵DP1报警", "pump", "DP1_Alarm"),
    ("分子泵DP1启动", "pump", "DP1_Start"),
    ("分子泵DP1复位", "pump", "DP1_Reset"),
    ("分子泵DP1数据启用", "pump", "DP1_DataEnable"),
    ("分子泵DP2运行", "pump", "DP2_Run"),
    ("分子泵DP2报警", "pump", "DP2_Alarm"),
    ("分子泵DP2启动", "pump", "DP2_Start"),
    ("分子泵DP2复位", "pump", "DP2_Reset"),
    ("分子泵DP2数据启用", "pump", "DP2_DataEnable"),
    ("分子泵DP3运行", "pump", "DP3_Run"),
    ("分子泵DP3报警", "pump", "DP3_Alarm"),
    ("分子泵DP3启动", "pump", "DP3_Start"),
    ("分子泵DP3复位", "pump", "DP3_Reset"),
    ("分子泵DP3数据启用", "pump", "DP3_DataEnable"),
    ("分子泵DP4运行", "pump", "DP4_Run"),
    ("分子泵DP4报警", "pump", "DP4_Alarm"),
    ("分子泵DP4启动", "pump", "DP4_Start"),
    ("分子泵DP4复位", "pump", "DP4_Reset"),
    ("分子泵DP4数据启用", "pump", "DP4_DataEnable"),
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
    ("分子泵自由口数据_850i_1运行", "pump", "DP850i_1_Run"),
    ("分子泵自由口数据_850i_1报警", "pump", "DP850i_1_Alarm"),
    ("分子泵自由口数据_850i_1开关机", "pump", "DP850i_1_Switch"),
    ("分子泵自由口数据_850i_1复位", "pump", "DP850i_1_Reset"),
    ("分子泵自由口数据_850i_1工作状态", "pump", "DP850i_1_Status"),
    ("分子泵自由口数据_850i_2运行", "pump", "DP850i_2_Run"),
    ("分子泵自由口数据_850i_2报警", "pump", "DP850i_2_Alarm"),
    ("分子泵自由口数据_850i_2开关机", "pump", "DP850i_2_Switch"),
    ("分子泵自由口数据_850i_2复位", "pump", "DP850i_2_Reset"),
    ("分子泵自由口数据_850i_2工作状态", "pump", "DP850i_2_Status"),
    ("分子泵_分子泵自由口数据_850i_1运行", "pump", "DP850i_1_Run_v2"),
    ("分子泵_分子泵自由口数据_850i_1报警", "pump", "DP850i_1_Alarm_v2"),
    ("分子泵_分子泵自由口数据_850i_2运行", "pump", "DP850i_2_Run_v2"),
    ("分子泵_分子泵自由口数据_850i_2报警", "pump", "DP850i_2_Alarm_v2"),
    ("循环泵变频器运行中", "pump", "CircPump1_Running"),
    ("循环泵变频器报警", "pump", "CircPump1_Alarm"),
    ("循环泵变频器复位", "pump", "CircPump1_Reset"),
    ("循环泵变频器启动", "pump", "CircPump1_Start"),
    ("循环泵2变频器报警", "pump", "CircPump2_Alarm"),
    ("循环泵2频率读取", "pump", "CircPump2_FreqRead"),
    ("循环泵2频率设定", "pump", "CircPump2_FreqSet"),
    ("循环泵2复位", "pump", "CircPump2_Reset"),
    ("循环泵2启动", "pump", "CircPump2_Start"),
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
    ("离子泵数据_离子泵运行状态", "pump", "IonPump_RunStatus"),
    ("离子泵数据_离子泵故障状态", "pump", "IonPump_FaultStatus"),
    ("离子泵数据_离子泵烘烤状态", "pump", "IonPump_BakeStatus"),
    ("离子泵数据_离子泵遥控状态", "pump", "IonPump_RemoteStatus"),
    ("离子泵数据_离子泵6KV高压状态", "pump", "IonPump_HV6KV_Status"),
    ("离子泵数据_离子泵4KV高压状态", "pump", "IonPump_HV4KV_Status"),
    ("万瑞压缩机1温度报警", "pump", "WR_Comp1_TempAlarm"),
    ("万瑞压缩机2温度报警", "pump", "WR_Comp2_TempAlarm"),
    ("万瑞压缩机3温度报警", "pump", "WR_Comp3_TempAlarm"),
    ("万瑞压缩机4温度报警", "pump", "WR_Comp4_TempAlarm"),
    ("住友压缩机1气体温度错误", "pump", "ZYK_Comp1_GasTempErr"),
    ("住友压缩机1电机温度报警", "pump", "ZYK_Comp1_MotorTempAlarm"),
    ("住友压缩机2气体温度错误", "pump", "ZYK_Comp2_GasTempErr"),
    ("住友压缩机2电机温度报警", "pump", "ZYK_Comp2_MotorTempAlarm"),
    ("住友压缩机3气体温度错误", "pump", "ZYK_Comp3_GasTempErr"),
    ("住友压缩机3电机温度报警", "pump", "ZYK_Comp3_MotorTempAlarm"),
    ("住友压缩机4气体温度错误", "pump", "ZYK_Comp4_GasTempErr"),
    ("住友压缩机4电机温度报警", "pump", "ZYK_Comp4_MotorTempAlarm"),
    ("低温循环泵出口压力", "pump", "CryoCircPump_OutletPress"),
    ("低温循环泵入口压力", "pump", "CryoCircPump_InletPress"),
]

# ── PI control parameters ──
SENSOR_POLL_SEC = 1.0
PI_POLL_SEC = 0.1
DEFAULT_SETPOINT = 1500.0
DEFAULT_KP = 0.006
DEFAULT_KI = 0.0005
DEFAULT_KD = 0.0001
VALVE_MIN = 45.0
VALVE_MAX = 100.0
VALVE_RATE_MAX = 3.0
VALVE_TRIM_MAX = 30.0
PRESSURE_RATE_MAX = 10.0
PRESSURE_RATE_DAMP = 0.15
MAX_CONSECUTIVE_FAILURES = 5

# ── HYST mode parameters ──
HYST_OUT_BAND = 15.0
HYST_TARGET_BAND = 10.0
HYST_STEP_SMALL = 0.25
HYST_STEP_BIG = 0.5
HYST_SWITCH_TIME = 600.0

# ── SQLite logging ──
DB_PATH = Path(os.getenv("SENSOR_DB_PATH", "/data/sensor_history.db"))
SQLITE_FLUSH_SEC = 30.0
SENSOR_CHANGE_REL = 0.005
SENSOR_CHANGE_ABS = 0.1
