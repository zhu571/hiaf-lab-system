import logging
import os

from dotenv import load_dotenv

from tools.api import GoAPI
from tools.parse import LLM_TIMEOUT_SECONDS, Parser
from worker import Worker, default_renew_interval


def main():
    load_dotenv()
    logging.basicConfig(
        level=os.getenv("LOG_LEVEL", "INFO"),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    lease_seconds = int(os.getenv("AGENT_LEASE_SECONDS", "300"))
    renew_interval = default_renew_interval(lease_seconds)
    # 硬不变量（D7，R8 修订为全链路时间预算）：租约靠周期续约维持，安全条件是
    # 「续约间隔 + 单次续约调用最坏耗时（3 次重试 × 请求超时 + 退避 1+2s）」
    # 落在租约内——否则续约来不及在过期前落地，任务仍会被重领双跑。
    # 旧校验只比 LLM 超时，漏算了前置 HTTP 链（get_report + 2N 次 list_issues）。
    request_timeout = float(os.getenv("REQUEST_TIMEOUT_SECONDS", "20"))
    worst_renew_call = 3 * request_timeout + 3
    if renew_interval + worst_renew_call >= lease_seconds:
        raise SystemExit(
            f"租约预算不足：renew_interval ({renew_interval}s) + 单次续约最坏耗时 "
            f"({worst_renew_call}s) 必须小于 AGENT_LEASE_SECONDS ({lease_seconds}s)"
        )
    api = GoAPI.from_env()
    api.login()
    parser = Parser(os.environ["DEEPSEEK_API_KEY"])
    Worker(
        api, parser,
        poll_interval=float(os.getenv("POLL_INTERVAL_SECONDS", "5")),
        lease_seconds=lease_seconds,
        llm_timeout=LLM_TIMEOUT_SECONDS,
        renew_interval=renew_interval,
    ).run_forever()


if __name__ == "__main__":
    main()
