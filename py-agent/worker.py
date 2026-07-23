import logging
import re
import time

from tools.api import sanitize_error


LOG = logging.getLogger("py-agent")


class Worker:
    def __init__(self, api, parser, poll_interval=5, lease_seconds=300):
        self.api = api
        self.parser = parser
        self.poll_interval = poll_interval
        self.lease_seconds = lease_seconds

    def run_once(self):
        task = self.api.claim(self.lease_seconds)
        if task is None:
            return False
        task_id = task["id"]
        try:
            report = self.api.get_report(task["report_id"], task["acting_user_id"], task_id)
            project_ids = list(dict.fromkeys(
                item["project_id"] for item in report.get("logs", []) if item.get("project_id")
            ))
            if not project_ids:
                raise RuntimeError("submitted report has no project logs")
            keyword = search_keyword(report)
            issues = []
            for project_id in project_ids:
                for status in ("open", "in_progress"):
                    issues.extend(self.api.list_issues(
                        project_id, status, keyword, task["acting_user_id"], task_id,
                    ))
            issues = list({item["id"]: item for item in issues}.values())[:10]
            parsed = self.parser.parse(report.get("raw_text", ""), issues, project_ids)
            candidates = [to_candidate(item) for item in parsed]
            confidence = sum(item["confidence"] for item in parsed) / len(parsed) if parsed else None
            self.api.complete(task_id, candidates, confidence)
            LOG.info("task completed", extra={"task_id": task_id, "candidate_count": len(candidates)})
        except Exception as exc:
            import traceback
            LOG.exception("task failed", extra={"task_id": task_id, "trace": traceback.format_exc()[:500]})
            detail = sanitize_error(exc)
            LOG.warning("task failed", extra={"task_id": task_id, "error": detail})
            try:
                self.api.fail(task_id, detail)
            except Exception:
                LOG.exception("could not mark task failed", extra={"task_id": task_id})
                try:
                    from urllib.parse import quote
                    from urllib.request import Request, urlopen
                    title = quote("Agent 死信告警", safe="")
                    body = f"任务 {task_id}: {detail}".encode("utf-8")
                    req = Request("http://ntfy:80/lab-alerts", data=body, method="POST")
                    req.add_header("Title", title)
                    req.add_header("Priority", "high")
                    req.add_header("Tags", "robot_face,warning")
                    req.add_header("Click", "http://10.144.144.12:8000/agent-candidates")
                    urlopen(req, timeout=5)
                except Exception:
                    LOG.exception("dead letter ntfy alert failed")
        return True

    def run_forever(self):
        while True:
            try:
                worked = self.run_once()
            except Exception:
                LOG.exception("claim failed")
                worked = False
            if not worked:
                time.sleep(self.poll_interval)


def search_keyword(report):
    for item in report.get("logs", []):
        text = re.split(r"[，。；;：:\n]", item.get("content", ""), maxsplit=1)[0].strip()
        if text:
            return text[:64]
    return ""


def to_candidate(item):
    if item["is_duplicate"]:
        return {
            "action_type": "add_comment", "project_id": item["project_id"],
            "payload": {"issue_id": item["duplicate_issue_id"], "content": item["description"]},
            "agent_confidence": item["confidence"],
        }
    return {
        "action_type": "create_issue", "project_id": item["project_id"],
        "payload": {
            "title": item["title"], "description": item["description"],
            "severity": item["severity"],
        },
        "agent_confidence": item["confidence"],
    }
