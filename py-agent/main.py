import logging
import os

from dotenv import load_dotenv

from tools.api import GoAPI
from tools.parse import LLM_TIMEOUT_SECONDS, Parser
from worker import Worker


def main():
    load_dotenv()
    logging.basicConfig(
        level=os.getenv("LOG_LEVEL", "INFO"),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    lease_seconds = int(os.getenv("AGENT_LEASE_SECONDS", "300"))
    # 硬不变量（D7）：LLM 硬超时 + fail 开销必须落在租约内，
    # 否则 fail 迟到触发 ErrInvalidLease，任务被重领后双处理。
    if LLM_TIMEOUT_SECONDS >= lease_seconds:
        raise SystemExit(
            f"LLM_TIMEOUT_SECONDS ({LLM_TIMEOUT_SECONDS}) 必须小于 "
            f"AGENT_LEASE_SECONDS ({lease_seconds})"
        )
    api = GoAPI.from_env()
    api.login()
    parser = Parser(os.environ["DEEPSEEK_API_KEY"])
    Worker(
        api, parser,
        poll_interval=float(os.getenv("POLL_INTERVAL_SECONDS", "5")),
        lease_seconds=lease_seconds,
        llm_timeout=LLM_TIMEOUT_SECONDS,
    ).run_forever()


if __name__ == "__main__":
    main()
