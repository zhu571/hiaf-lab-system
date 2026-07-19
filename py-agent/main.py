import logging
import os

from dotenv import load_dotenv

from tools.api import GoAPI
from tools.parse import Parser
from worker import Worker


def main():
    load_dotenv()
    logging.basicConfig(
        level=os.getenv("LOG_LEVEL", "INFO"),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    api = GoAPI.from_env()
    api.login()
    parser = Parser(os.environ["DEEPSEEK_API_KEY"])
    Worker(
        api, parser,
        poll_interval=float(os.getenv("POLL_INTERVAL_SECONDS", "5")),
        lease_seconds=int(os.getenv("AGENT_LEASE_SECONDS", "300")),
    ).run_forever()


if __name__ == "__main__":
    main()
