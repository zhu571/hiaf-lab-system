"""R9：InfluxDB 断写 backlog 上限测试 — 只测 _buffer_backlog 截断与恢复清空。

依赖打桩同 test_ioc_alert（influxdb_client/aiosqlite 测试环境未安装）；
Point 为 MagicMock，只验数量与新旧保留关系，不序列化真实行协议。
"""

import asyncio
import sys
import types
import unittest
from pathlib import Path
from unittest.mock import MagicMock


def _stub_third_party():
    if "influxdb_client" in sys.modules:
        return
    influx = types.ModuleType("influxdb_client")
    influx.InfluxDBClient = MagicMock()
    influx.Point = MagicMock()
    influx.client = types.ModuleType("influxdb_client.client")
    influx.client.write_api = types.ModuleType("influxdb_client.client.write_api")
    influx.client.write_api.SYNCHRONOUS = "synchronous"
    influx.write_api = influx.client.write_api
    sys.modules["influxdb_client"] = influx
    sys.modules["influxdb_client.client"] = influx.client
    sys.modules["influxdb_client.client.write_api"] = influx.client.write_api
    sys.modules["aiosqlite"] = types.ModuleType("aiosqlite")


_stub_third_party()
sys.path.insert(0, str(Path(__file__).parents[1] / "ioc"))

from hiaf_storage import BACKLOG_MAX, HiafStorage  # noqa: E402


# 2 个传感器点 + 3 个控制点（ValveSP/Setpoint/Error）= 每轮 5 Point。
SENSOR_TAGS = [("t1", "temp:PV1"), ("t2", "press:PV2")]
POINTS_PER_FLUSH = len(SENSOR_TAGS) + 3


def make_storage(flush=None):
    storage = HiafStorage(
        influx_url="http://influxdb:8086", influx_token="t", influx_org="o",
        influx_bucket="b", db_path="/nonexistent/sensor.db",
        sensor_tags=SENSOR_TAGS, pump_tags=[],
    )
    if flush is not None:
        storage._flush_influx = flush
    return storage


def flush_ok(points):
    return True


def flush_fail(points):
    return False


def flush_raise(points):
    raise RuntimeError("influx down")


async def write_once(storage):
    await storage.maybe_write_influx({"t1": 1.0, "t2": 2.0}, {}, 0.0, 0.0, 0.0)


class BacklogCapTests(unittest.TestCase):
    def test_failed_writes_cap_backlog_at_limit(self):
        # 断写远超 10 分钟的量：backlog 必须停在 BACKLOG_MAX，不再无上限增长。
        storage = make_storage(flush=flush_fail)
        rounds = (BACKLOG_MAX // POINTS_PER_FLUSH) + 10
        for _ in range(rounds):
            asyncio.run(write_once(storage))
        self.assertEqual(len(storage._write_backlog), BACKLOG_MAX)

    def test_flush_exception_also_caps_backlog(self):
        # run_in_executor 抛异常分支与返回 False 分支同走截断。
        storage = make_storage(flush=flush_raise)
        rounds = (BACKLOG_MAX // POINTS_PER_FLUSH) + 10
        for _ in range(rounds):
            asyncio.run(write_once(storage))
        self.assertEqual(len(storage._write_backlog), BACKLOG_MAX)

    def test_buffer_backlog_drops_oldest_keeps_newest(self):
        storage = make_storage()
        points = [f"p{i}" for i in range(BACKLOG_MAX + 40)]
        storage._buffer_backlog(list(points))
        self.assertEqual(len(storage._write_backlog), BACKLOG_MAX)
        self.assertEqual(storage._write_backlog, points[-BACKLOG_MAX:])
        self.assertNotIn("p0", storage._write_backlog)
        self.assertIn(f"p{BACKLOG_MAX + 39}", storage._write_backlog)

    def test_buffer_backlog_under_limit_kept_intact(self):
        storage = make_storage()
        points = ["a", "b", "c"]
        storage._buffer_backlog(list(points))
        self.assertEqual(storage._write_backlog, points)

    def test_recovery_flushes_backlog(self):
        # 恢复写入后积压随下一批一起冲掉，backlog 清空。
        storage = make_storage(flush=flush_fail)
        for _ in range(5):
            asyncio.run(write_once(storage))
        self.assertEqual(len(storage._write_backlog), 5 * POINTS_PER_FLUSH)
        storage._flush_influx = flush_ok
        asyncio.run(write_once(storage))
        self.assertEqual(storage._write_backlog, [])


if __name__ == "__main__":
    unittest.main()
