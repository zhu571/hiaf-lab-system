"""labctl 命令行入口（click）。

用法示例：
    labctl login zhangsan
    labctl daily-report today
    labctl projects list --status active
    labctl issues create prj_001 --title "..." --severity high
    labctl alerts list --status active
    labctl update status
    labctl update run
"""

import json
import os
import sys

if __package__ in (None, ""):
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import click

from cli import commands
from cli.api_client import DEFAULT_BASE_URL, LabctlAPI, LabctlError
from cli.auth import clear_token, load_token, save_token

EXIT_OK = 0
EXIT_ERROR = 1
EXIT_AUTH = 2

_AUTH_CODES = ("not_logged_in", "auth_expired", "invalid_credentials", "csrf_failed")


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
@click.option("--base-url", default=None,
              help="服务端基础地址（默认 LABCTL_BASE_URL 或 http://127.0.0.1:8000；"
                   "已登录时未显式传入则沿用上次登录的服务器）")
@click.option("--human", is_flag=True, help="人类可读输出（默认 JSON，便于 Agent 解析）")
@click.pass_context
def cli(ctx, base_url, human):
    ctx.ensure_object(dict)
    ctx.obj["base_url"] = base_url
    ctx.obj["human"] = human


def build_api(base_url):
    """构造客户端：LABCTL_SERVICE_TOKEN 优先（无人值守透传），否则读本地 token 文件。"""
    service_token = os.getenv("LABCTL_SERVICE_TOKEN", "")
    if service_token:
        return LabctlAPI(base_url or DEFAULT_BASE_URL, access_token=service_token)
    stored = load_token()
    if stored:
        return LabctlAPI.from_stored(stored, base_url=base_url)
    return LabctlAPI(base_url or DEFAULT_BASE_URL)


def emit_result(result, human):
    if human:
        click.echo(render_human(result))
    else:
        click.echo(json.dumps(result, ensure_ascii=False, indent=2))


def emit_error(exc):
    payload = {"error": exc.to_dict()}
    click.echo(json.dumps(payload, ensure_ascii=False, indent=2), err=True)


def run_command(ctx, fn, **kwargs):
    api = build_api(ctx.obj["base_url"])
    try:
        result = fn(api, **kwargs)
    except LabctlError as exc:
        emit_error(exc)
        code = EXIT_AUTH if (exc.status == 401 or exc.code in _AUTH_CODES) else EXIT_ERROR
        raise click.exceptions.Exit(code)
    emit_result(result, ctx.obj["human"])
    return result


def render_human(result):
    lines = []
    if isinstance(result, list):
        for item in result:
            lines.append(_brief(item))
        return "\n".join(lines) if lines else "(空)"
    if not isinstance(result, dict):
        return str(result)
    items = result.get("items")
    if isinstance(items, list):
        lines.append(f"共 {result.get('total', len(items))} 条")
        for item in items:
            lines.append("  " + _brief(item))
        return "\n".join(lines) if lines else "(空)"
    for key in ("id", "code", "name", "title", "status", "date", "category", "level",
                "source", "username", "message", "success", "total"):
        if key in result:
            lines.append(f"{key}: {result[key]}")
    for key, value in result.items():
        if key not in ("id", "code", "name", "title", "status", "date", "category",
                       "level", "source", "username", "message", "success", "total") \
                and not isinstance(value, (dict, list)):
            lines.append(f"{key}: {value}")
    return "\n".join(lines) if lines else json.dumps(result, ensure_ascii=False)


def _brief(item):
    if not isinstance(item, dict):
        return str(item)
    fields = [(k, item[k]) for k in
              ("id", "code", "name", "title", "status", "date", "category", "data_type",
               "measurement", "level", "source", "severity", "run_type")
              if item.get(k) is not None]
    return ", ".join(f"{k}={v}" for k, v in fields)


# ---------------------------------------------------------------- login
@cli.command("login")
@click.option("--token-stdin", is_flag=True, help="从 stdin 读取两行：用户名、密码（CI/Agent 场景）")
@click.option("--logout", is_flag=True, help="注销：撤销 refresh token 并清除本地凭证")
@click.option("--whoami", is_flag=True, help="显示当前登录用户")
@click.argument("username", required=False)
@click.pass_context
def login(ctx, token_stdin, logout, whoami, username):
    """登录 / 注销 / 查看当前用户。登录后凭证存 ~/.labctl/token（0600）。"""
    base_url = ctx.obj["base_url"]
    if logout:
        # 未显式传 --base-url 时沿用 token 文件记录的服务器，确保在正确端撤销。
        _do_logout(ctx, base_url)
        return
    if whoami:
        run_command(ctx, commands.run_whoami)
        return
    if not username and not token_stdin:
        raise click.UsageError("交互登录需要用户名（labctl login <用户名>）或 --token-stdin")
    if token_stdin and username:
        raise click.UsageError("--token-stdin 与用户名参数互斥（stdin 第一行已含用户名）")
    password = ""
    if token_stdin:
        lines = [line.strip() for line in sys.stdin if line.strip()]
        if len(lines) < 2:
            raise click.UsageError("--token-stdin 需要两行输入：第一行用户名，第二行密码")
        username, password = lines[0], lines[1]
    else:
        password = click.prompt("密码", hide_input=True)
    api = LabctlAPI(base_url or DEFAULT_BASE_URL)
    try:
        data = api.login(username, password)
    except LabctlError as exc:
        emit_error(exc)
        raise click.exceptions.Exit(EXIT_AUTH)
    save_token(api.to_token_payload())
    result = {"username": username, "user": data.get("user"), "success": True}
    emit_result(result, ctx.obj["human"])


def _do_logout(ctx, base_url):
    stored = load_token()
    api = LabctlAPI.from_stored(stored, base_url=base_url) if stored \
        else LabctlAPI(base_url or DEFAULT_BASE_URL)
    try:
        result = commands.run_logout(api)
    except LabctlError as exc:
        emit_error(exc)
        clear_token()
        raise click.exceptions.Exit(EXIT_ERROR)
    clear_token()
    emit_result(result, ctx.obj["human"])


# ---------------------------------------------------------------- daily-report
@cli.group("daily-report")
def daily_report():
    """日报：today / history / entry"""


@daily_report.command("today")
@click.pass_context
def daily_report_today(ctx):
    """获取/创建今日日报"""
    run_command(ctx, commands.run_daily_report_today)


@daily_report.command("history")
@click.option("--status")
@click.option("--keyword")
@click.option("--date", help="YYYY-MM-DD")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def daily_report_history(ctx, status, keyword, date, page, per_page):
    """日报列表（仅本人可见）"""
    run_command(ctx, commands.run_daily_report_history,
                status=status or "", keyword=keyword or "", date=date or "",
                page=page, per_page=per_page)


@daily_report.command("entry")
@click.argument("report_id")
@click.option("--raw-text", help="更新日报正文（不给则只读详情）")
@click.pass_context
def daily_report_entry(ctx, report_id, raw_text):
    """查看/编辑单份日报"""
    run_command(ctx, commands.run_daily_report_entry,
                report_id=report_id, raw_text=raw_text)


# ---------------------------------------------------------------- projects
@cli.group("projects")
def projects():
    """项目：list / get / create / transition"""


@projects.command("list")
@click.option("--status")
@click.pass_context
def projects_list(ctx, status):
    """项目列表（默认只看自己 active 成员的项目）"""
    run_command(ctx, commands.run_projects_list, status=status or "")


@projects.command("get")
@click.argument("project_id")
@click.pass_context
def projects_get(ctx, project_id):
    """项目详情"""
    run_command(ctx, commands.run_projects_get, project_id=project_id)


@projects.command("create")
@click.option("--code", required=True, help="项目编码（唯一）")
@click.option("--name", required=True, help="项目名称")
@click.option("--short-name")
@click.option("--description")
@click.option("--visibility", type=click.Choice(["private", "public"]))
@click.option("--start-date", help="YYYY-MM-DD")
@click.option("--target-end-date", help="YYYY-MM-DD")
@click.option("--default-category")
@click.option("--tags")
@click.pass_context
def projects_create(ctx, code, name, short_name, description, visibility,
                    start_date, target_end_date, default_category, tags):
    """创建项目（需 maintainer/admin 角色）"""
    run_command(ctx, commands.run_projects_create,
                code=code, name=name, short_name=short_name, description=description,
                visibility=visibility, start_date=start_date, target_end_date=target_end_date,
                default_category=default_category, tags=tags)


@projects.command("transition")
@click.argument("project_id")
@click.option("--action", required=True, type=click.Choice(list(commands.PROJECT_TRANSITIONS)),
              help="流转动作：activate/complete/archive/reactivate/deactivate/reopen"
                   "（由服务端按当前状态校验）")
@click.pass_context
def projects_transition(ctx, project_id, action):
    """流转项目状态（draft→active→completed→archived 生命周期）"""
    run_command(ctx, commands.run_projects_transition, project_id=project_id, action=action)


# ---------------------------------------------------------------- issues
@cli.group("issues")
def issues():
    """问题：list / create / transition"""


@issues.command("list")
@click.argument("project_id")
@click.option("--status", type=click.Choice(["open", "in_progress", "resolved", "closed"]))
@click.option("--severity", type=click.Choice(list(commands.ISSUE_SEVERITIES)))
@click.option("--search", "search")
@click.option("--assignee")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def issues_list(ctx, project_id, status, severity, search, assignee, page, per_page):
    """项目问题列表"""
    run_command(ctx, commands.run_issues_list, project_id=project_id,
                status=status or "", severity=severity or "", search=search or "",
                assignee=assignee or "", page=page, per_page=per_page)


@issues.command("create")
@click.argument("project_id")
@click.option("--title", required=True)
@click.option("--description")
@click.option("--severity", type=click.Choice(list(commands.ISSUE_SEVERITIES)), default="medium")
@click.option("--assignee-id")
@click.pass_context
def issues_create(ctx, project_id, title, description, severity, assignee_id):
    """创建问题（需项目 member+）"""
    run_command(ctx, commands.run_issues_create, project_id=project_id, title=title,
                description=description, severity=severity, assignee_id=assignee_id)


@issues.command("transition")
@click.argument("issue_id")
@click.option("--target-status", required=True,
              type=click.Choice(["open", "in_progress", "resolved", "closed"]))
@click.option("--reason")
@click.pass_context
def issues_transition(ctx, issue_id, target_status, reason):
    """流转问题状态"""
    run_command(ctx, commands.run_issues_transition, issue_id=issue_id,
                target_status=target_status, reason=reason)


# ---------------------------------------------------------------- test-data
@cli.group("test-data")
def test_data():
    """测试数据：list / entry"""


@test_data.command("list")
@click.argument("project_id")
@click.option("--run-id")
@click.option("--data-type", type=click.Choice(list(commands.TEST_DATA_TYPES)))
@click.option("--quality", type=click.Choice(["normal", "suspect", "invalid"]))
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def test_data_list(ctx, project_id, run_id, data_type, quality, page, per_page):
    """测试数据列表（默认过滤 invalid）"""
    run_command(ctx, commands.run_test_data_list, project_id=project_id, run_id=run_id,
                data_type=data_type, quality=quality, page=page, per_page=per_page)


@test_data.command("entry")
@click.argument("project_id")
@click.option("--data-type", required=True, type=click.Choice(list(commands.TEST_DATA_TYPES)))
@click.option("--measurement", required=True)
@click.option("--value", required=True, type=float)
@click.option("--unit")
@click.option("--quality", type=click.Choice(["normal", "suspect", "invalid"]), default="normal")
@click.option("--measured-at", help="RFC3339 时间，默认服务端当前时间")
@click.option("--run-id")
@click.option("--notes")
@click.pass_context
def test_data_entry(ctx, project_id, data_type, measurement, value, unit, quality,
                    measured_at, run_id, notes):
    """录入单条测试数据"""
    run_command(ctx, commands.run_test_data_entry, project_id=project_id, data_type=data_type,
                measurement=measurement, value=value, unit=unit, quality=quality,
                measured_at=measured_at, run_id=run_id, notes=notes)


# ---------------------------------------------------------------- runs
@cli.group("runs")
def runs():
    """实验批次：list / get / status"""


@runs.command("list")
@click.argument("project_id")
@click.option("--campaign")
@click.option("--status", type=click.Choice(["planned", "active", "paused", "completed", "aborted"]))
@click.option("--run-type", type=click.Choice(["cooldown", "warmup", "steady_state", "test"]))
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def runs_list(ctx, project_id, campaign, status, run_type, page, per_page):
    """实验批次列表"""
    run_command(ctx, commands.run_runs_list, project_id=project_id, campaign=campaign,
                status=status, run_type=run_type, page=page, per_page=per_page)


@runs.command("get")
@click.argument("run_id")
@click.pass_context
def runs_get(ctx, run_id):
    """批次详情"""
    run_command(ctx, commands.run_runs_get, run_id=run_id)


@runs.command("status")
@click.argument("run_id")
@click.option("--action", required=True, type=click.Choice(list(commands.RUN_TRANSITIONS)),
              help="流转动作：start/abort/pause/complete/resume（由服务端按当前状态校验）")
@click.pass_context
def runs_status(ctx, run_id, action):
    """流转批次状态"""
    run_command(ctx, commands.run_runs_status, run_id=run_id, action=action)


# ---------------------------------------------------------------- alerts
@cli.group("alerts")
def alerts():
    """告警：list / resolve"""


@alerts.command("list")
@click.option("--status", type=click.Choice(["active", "resolved"]), default="active")
@click.option("--limit", type=int, default=50, show_default=True)
@click.option("--offset", type=int, default=0, show_default=True)
@click.pass_context
def alerts_list(ctx, status, limit, offset):
    """告警列表"""
    run_command(ctx, commands.run_alerts_list, status=status, limit=limit, offset=offset)


@alerts.command("resolve")
@click.argument("alert_id")
@click.pass_context
def alerts_resolve(ctx, alert_id):
    """解除告警（需 admin/maintainer 角色）"""
    run_command(ctx, commands.run_alerts_resolve, alert_id=alert_id)


# ---------------------------------------------------------------- logs
@cli.group("logs")
def logs():
    """日志：list / get"""


@logs.command("list")
@click.argument("project_id")
@click.option("--category",
              type=click.Choice(["general", "assembly", "test", "cryo", "rf", "vacuum",
                                 "beam", "data_analysis"]))
@click.option("--date-from")
@click.option("--date-to")
@click.option("--status", type=click.Choice(list(commands.LOG_STATUSES)),
              help="内容状态过滤：draft/confirmed/locked/voided（默认不传，服务端默认只返回 confirmed）")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def logs_list(ctx, project_id, category, date_from, date_to, status, page, per_page):
    """项目日志列表"""
    run_command(ctx, commands.run_logs_list, project_id=project_id, category=category,
                date_from=date_from, date_to=date_to, status=status, page=page, per_page=per_page)


@logs.command("get")
@click.argument("log_id")
@click.pass_context
def logs_get(ctx, log_id):
    """日志详情"""
    run_command(ctx, commands.run_logs_get, log_id=log_id)


# ---------------------------------------------------------------- experiences
@cli.group("experiences")
def experiences():
    """经验库（AI 提取）：extract-candidates / list / publish"""


@experiences.command("extract-candidates")
@click.option("--days", type=int, default=None,
              help="回溯天数（默认 7，限 1-30）")
@click.pass_context
def experiences_extract_candidates(ctx, days):
    """触发 AI 经验候选提取（需 maintainer/admin）：最近 days 天 resolved/closed 的
    issue 提炼经验候选，落库为 candidate 草稿，随后在经验库审核发布"""
    run_command(ctx, commands.run_experiences_extract, days=days)


@experiences.command("list")
@click.option("--status", type=click.Choice(["candidate", "published", "archived"]),
              help="缺省 published")
@click.option("--project-id")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def experiences_list(ctx, status, project_id, page, per_page):
    """经验列表（审核候选用 status=candidate）"""
    run_command(ctx, commands.run_experiences_list, status=status or "",
                project_id=project_id or "", page=page, per_page=per_page)


@experiences.command("publish")
@click.argument("experience_id")
@click.pass_context
def experiences_publish(ctx, experience_id):
    """审核通过并发布候选经验（需项目 maintainer+；全局经验仅 admin）"""
    run_command(ctx, commands.run_experiences_publish, experience_id=experience_id)


# ---------------------------------------------------------------- weekly
@cli.group("weekly")


def weekly():
    """周报（AI 生成）：generate / recent"""

@weekly.command("generate")
@click.option("--week-start", help="周开始日期（周一，YYYY-MM-DD；默认本周一）")
@click.option("--no-notify", is_flag=True, help="不推送 ntfy 通知")
@click.pass_context
def weekly_generate(ctx, week_start, no_notify):
    """生成/复用本周周报（需 maintainer/admin；同一周只生成一次，重复调用返回已存周报）"""
    run_command(ctx, commands.run_weekly_generate,
                week_start=week_start or "", notify=not no_notify)


@weekly.command("recent")
@click.option("--limit", type=int, default=5, show_default=True)
@click.pass_context
def weekly_recent(ctx, limit):
    """查看最近周报（复用 experiences 查询，tags=weekly_summary）"""
    run_command(ctx, commands.run_weekly_recent, limit=limit)


# ---------------------------------------------------------------- update
@cli.group("update")
def update():
    """系统更新（admin）：status / run"""


@update.command("status")
@click.pass_context
def update_status(ctx):
    """查询当前版本与远端差异（behind/can_update）"""
    run_command(ctx, commands.run_update_status)


@update.command("run")
@click.option("--no-wait", is_flag=True, help="只触发更新不跟踪日志流（脚本后台触发场景）")
@click.option("--timeout", "timeout_s", type=int, default=2400, show_default=True,
              help="日志流读超时（秒），默认为服务端 30 分钟看门狗略留余量")
@click.pass_context
def update_run(ctx, no_wait, timeout_s):
    """触发系统更新并实时跟踪执行日志（需 admin 交互登录）

    默认订阅 SSE 日志流直到更新结束：done 且成功退出码 0，error / done 但
    失败退出码 1；--no-wait 只触发并打印 session_id。
    """
    api = build_api(ctx.obj["base_url"])
    human = ctx.obj["human"]
    try:
        result = commands.run_update_trigger(api)
    except LabctlError as exc:
        _emit_update_error(exc)
    session_id = result.get("session_id", "")
    if no_wait:
        payload = dict(result)
        payload["stream"] = f"/api/v1/admin/system/update/stream/{session_id}"
        emit_result(payload, human)
        return
    if human:
        click.echo(f"[UPDATE] 已触发更新 session={session_id}，当前版本 {result.get('current', '')}，开始跟踪日志流…")
    else:
        click.echo(json.dumps({"event": "triggered", "session_id": session_id,
                               "current": result.get("current", "")}, ensure_ascii=False))
    saw_done = saw_error = False
    try:
        for event, payload in commands.run_update_stream(api, session_id, timeout_s=timeout_s):
            if human:
                click.echo(_render_update_event_human(event, payload))
            else:
                click.echo(json.dumps({"event": event, "data": payload}, ensure_ascii=False))
            if event == "error":
                saw_error = True
                break
            if event == "done":
                saw_done = True
                if isinstance(payload, dict) and (payload.get("success") is False
                                                  or payload.get("exit_code")):
                    saw_error = True  # done 但失败（exit_code 非零 / success=false）同判失败
                break
    except LabctlError as exc:
        _emit_update_error(exc)
    if saw_error:
        raise click.exceptions.Exit(EXIT_ERROR)
    if not saw_done:
        emit_error(LabctlError(
            f"更新日志流已结束但未收到 done/error 结果事件（session={session_id}），"
            "请用 labctl update status 或重新订阅确认结果", code="stream_ended_without_result"))
        raise click.exceptions.Exit(EXIT_ERROR)


def _emit_update_error(exc):
    """更新命令统一错误出口：透传错误 + 403 时补 admin 交互登录提示。"""
    emit_error(exc)
    if exc.status == 403 or exc.code in ("csrf_failed", "permission_denied"):
        click.echo("提示：系统更新需要 admin 账号交互登录（labctl login <admin 用户名>）；"
                   "LABCTL_SERVICE_TOKEN 服务账号通道的写操作会被服务端 CSRF 校验拒绝（403）",
                   err=True)
    code = EXIT_AUTH if (exc.status == 401 or exc.code in _AUTH_CODES) else EXIT_ERROR
    raise click.exceptions.Exit(code)


def _render_update_event_human(event, payload):
    if not isinstance(payload, dict):
        return f"[UPDATE] {payload}"
    if event == "step":
        step, total = payload.get("step"), payload.get("step_total")
        label = f"步骤 {step}/{total}" if step and total else (payload.get("title") or "步骤")
        title = payload.get("title", "")
        return f"[UPDATE] {label}：{title}" if title and label != title else f"[UPDATE] {label}"
    if event == "line":
        return payload.get("text", "")
    if event == "done":
        old, new = payload.get("old_sha", ""), payload.get("new_sha", "")
        sha = f"（{old[:8]}..{new[:8]}）" if old or new else ""
        return f"[UPDATE] 完成 exit_code={payload.get('exit_code', 0)}{sha}"
    if event == "error":
        return f"[UPDATE] 失败：{payload.get('message', '')}"
    return f"[UPDATE] {event}: {json.dumps(payload, ensure_ascii=False)}"


def main():
    cli(prog_name="labctl")


if __name__ == "__main__":
    main()
