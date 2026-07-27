"""Test B: 端到端复现 hiaf_ioc_final.py 的结构 ——
dictConfig（有 bug 的配置）+ @pv.startup/@pv.putter/@pv.shutdown 链式装饰 + run(pvdb)。

用 print 和 marker 文件绕过 logging，证明 startup 钩子本身是否被调用。
"""
import asyncio
import logging
import logging.config
import pathlib
import sys

from caproto.server import PVGroup, pvproperty, run

LOGGER = logging.getLogger(__name__)

# 与 hiaf_ioc_final.py 完全相同的配置
_LOGGING_CONFIG = {
    "version": 1,
    "formatters": {"default": {"format": "%(asctime)s %(levelname)s %(name)s: %(message)s"}},
    "handlers": {
        "stdout": {"class": "logging.StreamHandler", "stream": "ext://sys.stdout",
                   "level": "INFO", "formatter": "default"},
        "stderr": {"class": "logging.StreamHandler", "stream": "ext://sys.stderr",
                   "level": "WARNING", "formatter": "default"},
    },
    "root": {"level": "INFO", "handlers": ["stdout", "stderr"]},
}
logging.config.dictConfig(_LOGGING_CONFIG)

MARKER = pathlib.Path(__file__).parent / "startup_fired.marker"


class MiniIOC(PVGroup):
    Piezo_Running = pvproperty(name="Piezo:Running", value=0, dtype=int, doc="0=STOP, 1=RUN")

    @Piezo_Running.startup
    async def Piezo_Running(self, instance, async_lib):
        # 与生产代码相同：第一行是 LOGGER.info —— 预期被吞
        LOGGER.info("HiafGasCellIOC starting up...")
        # print / marker 证明钩子真的被执行了
        print("STARTUP-HOOK-ACTUALLY-FIRED", flush=True)
        MARKER.write_text("fired")
        while True:
            await asyncio.sleep(3600)

    @Piezo_Running.putter
    async def Piezo_Running(self, instance, value):
        return value

    @Piezo_Running.shutdown
    async def Piezo_Running(self, instance, async_lib):
        pass


if __name__ == "__main__":
    ioc = MiniIOC(prefix="TEST:")
    print("PVDB:", list(ioc.pvdb), flush=True)
    print(f"module LOGGER.disabled = {LOGGER.disabled}", flush=True)
    run(ioc.pvdb)
