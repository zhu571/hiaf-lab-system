"""IOC 告警中心接入测试 — 只测 _report_alert/_resolve_alert/_send_ntfy 的 HTTP 行为。

IOC 依赖 caproto/asyncua/aiohttp/aiosqlite/influxdb_client（测试环境未安装），
导入前用 types.ModuleType 打桩 sys.modules，避免真实依赖。
"""

import asyncio
import json
import sys
import time
import types
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

# ── 打桩第三方依赖（仅测试环境，不安装真实库）──
def _stub_third_party():
    if "caproto" in sys.modules:
        return

    class _PVGroup:
        pass

    def _pvproperty(*_args, **_kwargs):
        return MagicMock()

    caproto = types.ModuleType("caproto")
    caproto.AlarmSeverity = MagicMock()
    caproto.AlarmStatus = MagicMock()
    caproto.server = types.ModuleType("caproto.server")
    caproto.server.PVGroup = _PVGroup
    caproto.server.pvproperty = _pvproperty
    caproto.server.run = MagicMock()
    sys.modules["caproto"] = caproto
    sys.modules["caproto.server"] = caproto.server

    asyncua = types.ModuleType("asyncua")
    asyncua.Client = MagicMock()
    asyncua.ua = MagicMock()
    sys.modules["asyncua"] = asyncua

    aiohttp = types.ModuleType("aiohttp")
    aiohttp.ClientSession = MagicMock()
    aiohttp.ClientTimeout = MagicMock()
    sys.modules["aiohttp"] = aiohttp

    sys.modules["aiosqlite"] = types.ModuleType("aiosqlite")

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


_stub_third_party()
sys.path.insert(0, str(Path(__file__).parents[1] / "ioc"))

import hiaf_config  # noqa: E402
import hiaf_ioc_final  # noqa: E402
from hiaf_ioc_final import HiafGasCellIOC, QUEUE_CRITICAL_WATERMARK  # noqa: E402


class FakeResponse:
    """session.post 返回的响应对象（async context manager，带 status）。"""

    def __init__(self, status=200):
        self.status = status

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_exc):
        return False


class FakePost:
    """模拟 aiohttp 的 post 返回值：`await post(...)`（_send_ntfy）与
    `async with post(...) as resp`（_report_alert/_resolve_alert）都支持。"""

    def __init__(self, status=200, raise_error=False):
        self._status = status
        self._raise_error = raise_error
        self._resp = None

    def _go(self):
        if self._raise_error:
            raise ConnectionError("mock network down")
        self._resp = FakeResponse(self._status)
        return self._resp

    def __await__(self):
        async def _inner():
            return self._go()

        return _inner().__await__()

    async def __aenter__(self):
        return self._go()

    async def __aexit__(self, *_exc):
        return False


class FakeSession:
    """伪造 aiohttp.ClientSession，记录 (url, kwargs)。"""

    def __init__(self, calls, status=200, raise_on=None):
        self._calls = calls
        self._status = status
        self._raise_on = raise_on

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_exc):
        return False

    def post(self, url, **kwargs):
        self._calls.append((url, kwargs))
        return FakePost(
            status=self._status,
            raise_error=self._raise_on is not None and url.endswith(self._raise_on),
        )


class FakeSubQueue:
    """仅支持一轮消费的伪订阅队列，qsize 首轮返回 critical 水位。"""

    def __init__(self, qsize):
        self._qsize = qsize
        self._count = 0

    async def get(self):
        self._count += 1
        if self._count > 1:
            raise asyncio.CancelledError
        return ("x", 1.0)

    def qsize(self):
        return self._qsize if self._count == 1 else 0

    def get_nowait(self):
        raise asyncio.QueueEmpty()


def make_ioc():
    """绕过 PVGroup.__init__（caproto 打桩），只准备告警辅助方法所需状态。"""
    ioc = object.__new__(HiafGasCellIOC)
    ioc._ntfy_cooldown = 60.0
    ioc._last_ntfy_disconnect_warn = 0.0
    ioc._last_ntfy_failrate_warn = 0.0
    ioc._last_ntfy_dataloss_warn = 0.0
    ioc._disconnect_warned = False
    ioc._data_loss_cnt = 0
    ioc._sensor_pvs = {}
    ioc._sensor_values = {}
    return ioc


class IocReportResolveTests(unittest.IsolatedAsyncioTestCase):
    """_report_alert / _resolve_alert 的 URL / Authorization / 失败降级。"""

    def setUp(self):
        self.calls = []
        self.token_patch = patch.object(hiaf_config, "SERVICE_TOKEN", "svc-token")
        self.token_patch.start()
        self.alert_patch = patch.object(hiaf_config, "ALERT_API_BASE", "http://server:8000")
        self.alert_patch.start()
        self.ntfy_token_patch = patch.object(hiaf_config, "NTFY_PUBLISH_TOKEN", "pub-token")
        self.ntfy_token_patch.start()
        self.env_patch = patch.dict("os.environ", {"NTFY_URL": "http://ntfy:80"}, clear=False)
        self.env_patch.start()
        self.addCleanup(self.token_patch.stop)
        self.addCleanup(self.alert_patch.stop)
        self.addCleanup(self.ntfy_token_patch.stop)
        self.addCleanup(self.env_patch.stop)

    def _patch_session(self, **kwargs):
        return patch.object(hiaf_ioc_final.aiohttp, "ClientSession",
                            lambda: FakeSession(self.calls, **kwargs))

    async def test_report_posts_to_alert_center_with_token_and_payload(self):
        ioc = make_ioc()
        with self._patch_session():
            await ioc._report_alert("warning", "订阅数据丢失",
                                    "订阅数据已累计丢失 10 次（队列深度 951）")
        self.assertEqual(len(self.calls), 1)
        url, kwargs = self.calls[0]
        self.assertEqual(url, "http://server:8000/api/v1/alerts/report")
        self.assertEqual(kwargs["headers"]["Authorization"], "Bearer svc-token")
        self.assertEqual(kwargs["headers"]["Content-Type"], "application/json")
        payload = json.loads(kwargs["data"].decode("utf-8"))
        self.assertEqual(payload["level"], "warning")
        self.assertEqual(payload["source"], "ioc")
        self.assertEqual(payload["title"], "订阅数据丢失")
        self.assertIn("队列深度 951", payload["detail"])

    async def test_resolve_posts_to_alert_center_with_token(self):
        ioc = make_ioc()
        with self._patch_session():
            await ioc._resolve_alert("OPC UA 断连 >30s")
        self.assertEqual(len(self.calls), 1)
        url, kwargs = self.calls[0]
        self.assertEqual(url, "http://server:8000/api/v1/alerts/resolve")
        self.assertEqual(kwargs["headers"]["Authorization"], "Bearer svc-token")
        payload = json.loads(kwargs["data"].decode("utf-8"))
        self.assertEqual(payload, {"source": "ioc", "title": "OPC UA 断连 >30s"})

    async def test_report_failure_falls_back_to_ntfy_with_detail(self):
        ioc = make_ioc()
        with self._patch_session(raise_on="/api/v1/alerts/report"):
            await ioc._report_alert("error", "OPC UA 断连 >30s", "OPC UA 连接中断超过 30s")
        # 第一发 report 失败 → 回退 _send_ntfy 到 lab-alerts（双保险）
        self.assertEqual(len(self.calls), 2)
        self.assertTrue(self.calls[0][0].endswith("/api/v1/alerts/report"))
        url, kwargs = self.calls[1]
        self.assertEqual(url, "http://ntfy:80/lab-alerts")
        self.assertEqual(kwargs["headers"]["Authorization"], "Bearer pub-token")
        self.assertEqual(kwargs["headers"]["Title"], "IOC")
        body = kwargs["data"].decode("utf-8")
        self.assertIn("OPC UA 断连 >30s", body)
        self.assertIn("OPC UA 连接中断超过 30s", body)

    async def test_report_http_500_falls_back_to_ntfy(self):
        ioc = make_ioc()
        with self._patch_session(status=500):
            await ioc._report_alert("warning", "订阅数据丢失", "detail-500")
        self.assertEqual(len(self.calls), 2)
        self.assertEqual(self.calls[1][0], "http://ntfy:80/lab-alerts")

    async def test_send_ntfy_without_token_omits_authorization(self):
        ioc = make_ioc()
        with patch.object(hiaf_config, "NTFY_PUBLISH_TOKEN", ""), self._patch_session():
            await ioc._send_ntfy("hello")
        self.assertEqual(len(self.calls), 1)
        url, kwargs = self.calls[0]
        self.assertEqual(url, "http://ntfy:80/lab-alerts")
        self.assertNotIn("Authorization", kwargs["headers"])
        self.assertEqual(kwargs["headers"]["Title"], "IOC")
        self.assertEqual(kwargs["headers"]["Priority"], "3")
        self.assertEqual(kwargs["data"], b"hello")


class IocCooldownTests(unittest.IsolatedAsyncioTestCase):
    """修复回归：订阅数据丢失告警 60s 冷却门控。"""

    async def test_dataloss_alert_fires_when_cooldown_expired(self):
        ioc = make_ioc()
        ioc._last_ntfy_dataloss_warn = time.monotonic() - 61.0
        ioc._report_alert = AsyncMock()
        ioc._sub_queue = FakeSubQueue(QUEUE_CRITICAL_WATERMARK + 1)
        with self.assertRaises(asyncio.CancelledError):
            await ioc._consume_sub_queue()
        ioc._report_alert.assert_awaited_once()
        title = ioc._report_alert.await_args.args[1]
        self.assertEqual(title, "订阅数据丢失")

    async def test_dataloss_alert_skipped_within_cooldown(self):
        ioc = make_ioc()
        ioc._last_ntfy_dataloss_warn = time.monotonic()
        ioc._report_alert = AsyncMock()
        ioc._sub_queue = FakeSubQueue(QUEUE_CRITICAL_WATERMARK + 1)
        with self.assertRaises(asyncio.CancelledError):
            await ioc._consume_sub_queue()
        ioc._report_alert.assert_not_awaited()


if __name__ == "__main__":
    unittest.main()
