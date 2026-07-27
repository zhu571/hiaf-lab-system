"""Test A: 精确复刻 hiaf_ioc_final.py 第 38-67 行的 logging 配置时序。

LOGGER 在 dictConfig 之前创建；config 中没有 "loggers" 键、
也没有 "disable_existing_loggers" 键（默认 True）。
"""
import logging
import logging.config

LOGGER = logging.getLogger(__name__)   # 与 hiaf_ioc_final.py:38 相同的时序

# 与 hiaf_ioc_final.py:41-66 完全一致
_LOGGING_CONFIG = {
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

print("--- after dictConfig ---")
LOGGER.info("SHOULD-BE-VISIBLE-INFO")
LOGGER.warning("SHOULD-BE-VISIBLE-WARNING")
LOGGER.error("SHOULD-BE-VISIBLE-ERROR")
print(f"LOGGER.disabled = {LOGGER.disabled}")
print(f"LOGGER.isEnabledFor(INFO) = {LOGGER.isEnabledFor(logging.INFO)}")
print(f"LOGGER.isEnabledFor(WARNING) = {LOGGER.isEnabledFor(logging.WARNING)}")

# 对照：dictConfig 之后新建的子 logger
NEW = logging.getLogger("created_after")
print(f"created_after.disabled = {NEW.disabled}")
NEW.info("NEW-LOGGER-INFO")

# 模拟 caproto：dictConfig 之前 import 时已创建的模块 logger
CAP = logging.getLogger("caproto.asyncio.server")
print(f"caproto-like.disabled = {CAP.disabled}")
CAP.info("CAPROTO-LIKE-INFO")
