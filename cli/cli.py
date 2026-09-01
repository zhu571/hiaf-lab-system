"""labctl 命令行入口（click）。

用法示例：
    labctl login zhangsan
    labctl daily-report today
    labctl daily-report submit rep_1            # blocked 时加 --force
    labctl logs create prj_001 --content "..." --category rf
    labctl attachments upload 曲线.png --entity-type log --entity-id log_1
    labctl test-data batch prj_001 --file data.csv
    labctl todos add 换气瓶 --priority high
    labctl update status / update run
"""

import json
import os
import sys
from datetime import datetime

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
        ctx.obj["_last_error"] = exc  # 供个别命令附加场景提示（如附件上传幂等冲突）
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
        if items and all(isinstance(item, dict) and "occurred_at" in item
                         and "content" in item for item in items):
            heading = (f"第 {result['page']} 页 / 共 {result.get('total', len(items))} 条"
                       if "page" in result else f"共 {result.get('total', len(items))} 条")
            return "\n".join([heading] + [_render_log_line(item) for item in items])
        if items and all(isinstance(item, dict) and "summary" in item
                         and ("report_date" in item or "date" in item) for item in items):
            return "\n".join(
                [f"{item.get('report_date', item.get('date', ''))} | "
                 f"{item.get('content_status', item.get('status', '-'))} | "
                 f"{_preview(item.get('summary', ''))}" for item in items])
        lines.append(f"共 {result.get('total', len(items))} 条")
        for item in items:
            lines.append("  " + _brief(item))
        return "\n".join(lines) if lines else "(空)"
    if "occurred_at" in result and "content" in result:
        return _render_log_detail(result)
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


def _preview(value, limit=60):
    text = " ".join(str(value or "").split())
    return text if len(text) <= limit else text[:limit] + "…"


def _local_time(value):
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone().strftime(
            "%Y-%m-%d %H:%M:%S %z")
    except (AttributeError, ValueError):
        return str(value or "-")


def _render_log_line(item):
    return (f"{str(item.get('id', '-'))[:8]} | {_local_time(item.get('occurred_at'))} | "
            f"{item.get('category', '-')} | {item.get('content_status', item.get('status', '-'))} | "
            f"{_preview(item.get('content', ''))}")


def _render_log_detail(item):
    metadata = (
        ("id", item.get("id")), ("project", item.get("project_id")),
        ("category", item.get("category")),
        ("status", item.get("content_status", item.get("status"))),
        ("occurred_at", _local_time(item.get("occurred_at"))),
        ("source", item.get("source")),
        ("author", item.get("author_name", item.get("author_id"))),
    )
    lines = ["元信息", *[f"{key}: {value or '-'}" for key, value in metadata], "", "正文",
             str(item.get("content") or "")]
    if item.get("raw_snippet"):
        lines.extend(("", "原文引用", *[f"> {line}" for line in
                                         str(item["raw_snippet"]).splitlines()]))
    return "\n".join(lines)


class LoginGroup(click.Group):
    """login 既是普通命令（labctl login <用户名>）又是组（login password / set-language）。

    click 原生组的位置参数会吞掉子命令名（login set-language en 会把 set-language
    当用户名、把 en 当未知子命令）；这里在解析前检测子命令名并让位。
    代价：用户名恰好叫 password/set-language 时只能走 --token-stdin。
    """

    def parse_args(self, ctx, args):
        for i, token in enumerate(args):
            if token in self.commands:
                click.Group.parse_args(self, ctx, args[:i])  # 只解析组级选项
                # click 8.4：protected_args 公开属性只读，实际存储在 _protected_args
                ctx._protected_args = [token]
                ctx.args = args[i + 1:]
                return ctx.args
        return click.Group.parse_args(self, ctx, args)


# ---------------------------------------------------------------- login
@cli.group("login", cls=LoginGroup, invoke_without_command=True)
@click.option("--token-stdin", is_flag=True, help="从 stdin 读取两行：用户名、密码（CI/Agent 场景）")
@click.option("--logout", is_flag=True, help="注销：撤销 refresh token 并清除本地凭证")
@click.option("--whoami", is_flag=True, help="显示当前登录用户")
@click.argument("username", required=False)
@click.pass_context
def login(ctx, token_stdin, logout, whoami, username):
    """登录 / 注销 / 查看当前用户 / 自助（password、set-language）。

    登录后凭证存 ~/.labctl/token（0600）。
    """
    if ctx.invoked_subcommand is not None:
        return  # 子命令（password / set-language）自带逻辑，不走登录
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


@login.command("password")
@click.option("--old-password", help="当前密码（不给则交互提示输入）")
@click.option("--new-password", help="新密码（不给则交互提示输入，需符合服务端强密码策略）")
@click.pass_context
def login_password(ctx, old_password, new_password):
    """修改本人密码（改完建议重新 labctl login）"""
    old_password = old_password or click.prompt("当前密码", hide_input=True)
    new_password = new_password or click.prompt("新密码", hide_input=True,
                                                confirmation_prompt=True)
    run_command(ctx, commands.run_login_password,
                old_password=old_password, new_password=new_password)


@login.command("set-language")
@click.argument("language", type=click.Choice(list(commands.LANGUAGES)))
@click.pass_context
def login_set_language(ctx, language):
    """切换本人界面语言（zh/en，持久化到服务端）"""
    run_command(ctx, commands.run_login_set_language, language=language)


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
@click.option("--summary", help="更新日报摘要（≤1000 字符，可与 --raw-text 同用）")
@click.pass_context
def daily_report_entry(ctx, report_id, raw_text, summary):
    """查看/编辑单份日报（仅作者本人、draft 态可改）"""
    run_command(ctx, commands.run_daily_report_entry,
                report_id=report_id, raw_text=raw_text, summary=summary)


@daily_report.command("submit")
@click.argument("report_id")
@click.option("--force", is_flag=True, help="有警告仍强制提交；不带时 blocked=true 只返回警告不落库")
@click.pass_context
def daily_report_submit(ctx, report_id, force):
    """提交日报（触发质量校验；日志闭环的最后一步）"""
    api = build_api(ctx.obj["base_url"])
    try:
        result = commands.run_daily_report_submit(api, report_id, force=force)
    except LabctlError as exc:
        emit_error(exc)
        code = EXIT_AUTH if (exc.status == 401 or exc.code in _AUTH_CODES) else EXIT_ERROR
        raise click.exceptions.Exit(code)
    emit_result(result, ctx.obj["human"])
    if isinstance(result, dict) and result.get("blocked"):
        click.echo("日报未提交：存在上述警告，确认无误后加 --force 重新提交", err=True)
        raise click.exceptions.Exit(EXIT_ERROR)


@daily_report.command("ai-parse")
@click.argument("report_id")
@click.pass_context
def daily_report_ai_parse(ctx, report_id):
    """触发日报 AI 解析（仅作者 + draft；限流 10 次/min，返回三态 ok/clarify/rejected）"""
    run_command(ctx, commands.run_daily_report_ai_parse, report_id=report_id)


@daily_report.command("by-date")
@click.option("--date", help="YYYY-MM-DD（默认今天）")
@click.option("--latest", is_flag=True, help="只取当天最新一份")
@click.pass_context
def daily_report_by_date(ctx, date, latest):
    """按日期查本人日报"""
    run_command(ctx, commands.run_daily_report_by_date, date=date or "", latest=latest)


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
@click.option("--visibility", type=click.Choice(list(commands.PROJECT_VISIBILITIES)),
              help="可见性：restricted（默认，仅成员）/ workspace（工作区共享）")
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
@click.option("--ignore-warnings", is_flag=True,
              help="存在警告（如 complete 时有 open issue）仍强制执行；不带该参数时"
                   "服务端返回 400 transition_warning")
@click.option("--reason", help="流转原因（随审计记录）")
@click.pass_context
def projects_transition(ctx, project_id, action, ignore_warnings, reason):
    """流转项目状态（draft→active→completed→archived 生命周期）"""
    run_command(ctx, commands.run_projects_transition, project_id=project_id, action=action,
                ignore_warnings=ignore_warnings, reason=reason)


@projects.command("update")
@click.argument("project_id")
@click.option("--name")
@click.option("--short-name")
@click.option("--description")
@click.option("--visibility", type=click.Choice(list(commands.PROJECT_VISIBILITIES)))
@click.option("--comment-policy", type=click.Choice(list(commands.COMMENT_POLICIES)))
@click.option("--start-date", help="YYYY-MM-DD")
@click.option("--target-end-date", help="YYYY-MM-DD")
@click.option("--default-category")
@click.option("--tags")
@click.pass_context
def projects_update(ctx, project_id, name, short_name, description, visibility,
                    comment_policy, start_date, target_end_date, default_category, tags):
    """更新项目（需 manage_project：项目 maintainer/owner/admin）"""
    run_command(ctx, commands.run_projects_update, project_id=project_id, name=name,
                short_name=short_name, description=description, visibility=visibility,
                comment_policy=comment_policy, start_date=start_date,
                target_end_date=target_end_date, default_category=default_category,
                tags=tags)


@projects.group("members")
def projects_members():
    """项目成员：list / add / set-role / remove"""


@projects_members.command("list")
@click.argument("project_id")
@click.pass_context
def projects_members_list(ctx, project_id):
    """成员列表（viewer+）"""
    run_command(ctx, commands.run_projects_members_list, project_id=project_id)


@projects_members.command("add")
@click.argument("project_id")
@click.argument("user_id")
@click.option("--role", required=True, type=click.Choice(list(commands.PROJECT_MEMBER_ROLES)))
@click.pass_context
def projects_members_add(ctx, project_id, user_id, role):
    """添加成员（需 manage_members）"""
    run_command(ctx, commands.run_projects_members_add,
                project_id=project_id, user_id=user_id, role=role)


@projects_members.command("set-role")
@click.argument("project_id")
@click.argument("user_id")
@click.option("--role", required=True, type=click.Choice(list(commands.PROJECT_MEMBER_ROLES)))
@click.pass_context
def projects_members_set_role(ctx, project_id, user_id, role):
    """调整成员角色（最后一个 owner 降级会被服务端 400 拒绝）"""
    run_command(ctx, commands.run_projects_members_set_role,
                project_id=project_id, user_id=user_id, role=role)


@projects_members.command("remove")
@click.argument("project_id")
@click.argument("user_id")
@click.pass_context
def projects_members_remove(ctx, project_id, user_id):
    """移除成员（移除最后一个 active owner 会被服务端 400 拒绝）"""
    run_command(ctx, commands.run_projects_members_remove,
                project_id=project_id, user_id=user_id)


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


@issues.command("get")
@click.argument("issue_id")
@click.pass_context
def issues_get(ctx, issue_id):
    """问题详情（viewer+）"""
    run_command(ctx, commands.run_issues_get, issue_id=issue_id)


@issues.command("update")
@click.argument("issue_id")
@click.option("--title")
@click.option("--description")
@click.option("--severity", type=click.Choice(list(commands.ISSUE_SEVERITIES)))
@click.option("--assignee-id")
@click.pass_context
def issues_update(ctx, issue_id, title, description, severity, assignee_id):
    """更新问题（member+；closed 后服务端拒绝修改）"""
    run_command(ctx, commands.run_issues_update, issue_id=issue_id, title=title,
                description=description, severity=severity, assignee_id=assignee_id)


@issues.command("comment")
@click.argument("issue_id")
@click.option("--content", required=True, help="评论内容（非空）")
@click.pass_context
def issues_comment(ctx, issue_id, content):
    """评论问题（按项目 comment_policy：everyone=任意登录 / members=项目读 / disabled=403）"""
    run_command(ctx, commands.run_issues_comment, issue_id=issue_id, content=content)


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
@click.option("--quality", type=click.Choice(list(commands.TEST_DATA_QUALITIES)), default="normal",
              show_default=True)
@click.option("--source", type=click.Choice(list(commands.TEST_DATA_SOURCES)),
              help="数据来源（默认 manual；agent 通道不开放给 CLI）")
@click.option("--measured-at", help="RFC3339 时间，默认服务端当前时间")
@click.option("--run-id")
@click.option("--notes")
@click.pass_context
def test_data_entry(ctx, project_id, data_type, measurement, value, unit, quality,
                    source, measured_at, run_id, notes):
    """录入单条测试数据"""
    run_command(ctx, commands.run_test_data_entry, project_id=project_id, data_type=data_type,
                measurement=measurement, value=value, unit=unit, quality=quality,
                source=source, measured_at=measured_at, run_id=run_id, notes=notes)


@test_data.command("batch")
@click.argument("project_id")
@click.option("--file", "batch_file", type=click.Path(exists=True, dir_okay=False),
              help="批量文件：JSON 数组或表头 CSV（列 data_type,measurement,value,unit,"
                   "quality,measured_at,run_id,notes），与 --json 二选一")
@click.option("--json", "batch_json", help="内联 JSON 数组（单引号包裹便于 shell 转义）")
@click.pass_context
def test_data_batch(ctx, project_id, batch_file, batch_json):
    """批量录入测试数据（1-100 行；任一行失败整批 422，行级 errors[] 原样透传）"""
    if bool(batch_file) == bool(batch_json):
        raise click.UsageError("需要且只需要一种数据来源：--file 或 --json")
    if batch_file:
        with open(batch_file, encoding="utf-8") as fh:
            text = fh.read()
    else:
        text = batch_json
    rows = commands.parse_test_data_batch(text)
    run_command(ctx, commands.run_test_data_batch, project_id=project_id, rows=rows)


@test_data.command("get")
@click.argument("entry_id")
@click.pass_context
def test_data_get(ctx, entry_id):
    """单条测试数据详情（viewer+）"""
    run_command(ctx, commands.run_test_data_get, entry_id=entry_id)


@test_data.command("invalidate")
@click.argument("entry_id")
@click.pass_context
def test_data_invalidate(ctx, entry_id):
    """软失效单条数据（记录者本人或项目 owner）"""
    run_command(ctx, commands.run_test_data_invalidate, entry_id=entry_id)


@test_data.command("update")
@click.argument("entry_id")
@click.option("--measurement")
@click.option("--value", type=float)
@click.option("--unit")
@click.option("--quality", type=click.Choice(list(commands.TEST_DATA_QUALITIES)))
@click.option("--measured-at", help="RFC3339 时间")
@click.option("--notes")
@click.pass_context
def test_data_update(ctx, entry_id, measurement, value, unit, quality, measured_at, notes):
    """编辑测试数据（data_type/run_id/source 不可变，member+）"""
    run_command(ctx, commands.run_test_data_update, entry_id=entry_id, measurement=measurement,
                value=value, unit=unit, quality=quality, measured_at=measured_at, notes=notes)


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
    """流转批次状态（需 maintainer+ 或批次创建者本人）"""
    run_command(ctx, commands.run_runs_status, run_id=run_id, action=action)


@runs.command("create")
@click.argument("project_id")
@click.option("--name", required=True)
@click.option("--campaign")
@click.option("--run-type", type=click.Choice(list(commands.RUN_TYPES)))
@click.option("--gas-type", type=click.Choice(list(commands.GAS_TYPES)))
@click.option("--target-temp", type=float)
@click.option("--min-temp", type=float)
@click.option("--pressure-min", type=float)
@click.option("--pressure-max", type=float)
@click.option("--pressure-unit", help="默认 mbar")
@click.option("--has-beam", is_flag=True, default=None, help="有束流（缺省 false）")
@click.option("--device", "devices", multiple=True,
              type=click.Choice(list(commands.RUN_DEVICES)))
@click.option("--description")
@click.pass_context
def runs_create(ctx, project_id, name, campaign, run_type, gas_type, target_temp, min_temp,
                pressure_min, pressure_max, pressure_unit, has_beam, devices, description):
    """创建实验批次（member+）"""
    run_command(ctx, commands.run_runs_create, project_id=project_id, name=name,
                campaign=campaign, run_type=run_type, gas_type=gas_type,
                target_temp=target_temp, min_temp=min_temp, pressure_min=pressure_min,
                pressure_max=pressure_max, pressure_unit=pressure_unit,
                has_beam=has_beam, devices=list(devices) or None, description=description)


@runs.command("delete")
@click.argument("run_id")
@click.pass_context
def runs_delete(ctx, run_id):
    """删除批次（软删；maintainer+ 或创建者本人）"""
    run_command(ctx, commands.run_runs_delete, run_id=run_id)


@runs.group("steps")
def runs_steps():
    """实验步骤：list / add / status / reorder"""


@runs_steps.command("list")
@click.argument("run_id")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=50, show_default=True)
@click.pass_context
def runs_steps_list(ctx, run_id, page, per_page):
    """批次步骤列表"""
    run_command(ctx, commands.run_runs_steps_list, run_id=run_id, page=page, per_page=per_page)


@runs_steps.command("add")
@click.argument("run_id")
@click.option("--name", required=True)
@click.option("--description")
@click.option("--depends-on", help="前置步骤 ID")
@click.option("--step-order", type=int, default=0, help="0=追加到末尾")
@click.pass_context
def runs_steps_add(ctx, run_id, name, description, depends_on, step_order):
    """添加实验步骤（member+）"""
    run_command(ctx, commands.run_runs_steps_add, run_id=run_id, name=name,
                description=description, depends_on=depends_on, step_order=step_order)


@runs_steps.command("status")
@click.argument("step_id")
@click.option("--action", required=True, type=click.Choice(list(commands.STEP_TRANSITIONS)),
              help="start/pause/resume/complete/skip/cancel（由服务端按当前状态校验）")
@click.pass_context
def runs_steps_status(ctx, step_id, action):
    """流转实验步骤状态（member+）"""
    run_command(ctx, commands.run_run_steps_status, step_id=step_id, action=action)


@runs_steps.command("reorder")
@click.argument("run_id")
@click.option("--steps", required=True,
              help='步骤顺序 JSON 数组，如 \'[{"id":"st_1","step_order":1}]\'（maintainer+）')
@click.pass_context
def runs_steps_reorder(ctx, run_id, steps):
    """重排实验步骤"""
    run_command(ctx, commands.run_runs_steps_reorder, run_id=run_id, steps=steps)


@runs.command("report-link")
@click.argument("run_id")
@click.argument("report_id")
@click.pass_context
def runs_report_link(ctx, run_id, report_id):
    """关联日报到批次（maintainer+ 或创建者）"""
    run_command(ctx, commands.run_runs_report_link, run_id=run_id, report_id=report_id)


@runs.command("report-unlink")
@click.argument("run_id")
@click.argument("report_id")
@click.pass_context
def runs_report_unlink(ctx, run_id, report_id):
    """取消批次与日报的关联"""
    run_command(ctx, commands.run_runs_report_unlink, run_id=run_id, report_id=report_id)


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


@alerts.command("get")
@click.argument("alert_id")
@click.pass_context
def alerts_get(ctx, alert_id):
    """告警详情"""
    run_command(ctx, commands.run_alerts_get, alert_id=alert_id)


# ---------------------------------------------------------------- logs
@cli.group("logs")
def logs():
    """日志：list / get"""


@logs.command("list")
@click.argument("project_id")
@click.option("--category",
              type=click.Choice(["general", "assembly", "test", "cryo", "rf", "vacuum",
                                 "beam", "data_analysis"]))
@click.option("--date", "date_value", help="自然日 YYYY-MM-DD（按 +08:00 展开）")
@click.option("--date-from", help="YYYY-MM-DD 或 RFC3339")
@click.option("--date-to", help="YYYY-MM-DD 或 RFC3339")
@click.option("--status", type=click.Choice(list(commands.LOG_STATUSES)),
              help="内容状态过滤：draft/confirmed/locked/voided（默认不传，服务端默认只返回 confirmed）")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.option("--all", "all_pages", is_flag=True, help="拉取全部匹配日志（最多 1000 条）")
@click.pass_context
def logs_list(ctx, project_id, category, date_value, date_from, date_to, status, page, per_page,
              all_pages):
    """项目日志列表"""
    run_command(ctx, commands.run_logs_list, project_id=project_id, category=category,
                date_value=date_value, date_from=date_from, date_to=date_to, status=status,
                page=page, per_page=per_page, all_pages=all_pages,
                resolve_project=ctx.obj["human"])


@logs.command("get")
@click.argument("log_id")
@click.pass_context
def logs_get(ctx, log_id):
    """日志详情"""
    run_command(ctx, commands.run_logs_get, log_id=log_id)


@logs.command("create")
@click.argument("project_id")
@click.option("--content", help="日志正文（与 --file 二选一）")
@click.option("--file", "content_file", type=click.Path(exists=True, dir_okay=False),
              help="从文件读正文（'-' 表示 stdin；长文本从编辑器/管道来更顺手）")
@click.option("--category", type=click.Choice(list(commands.LOG_CATEGORIES)),
              default="general", show_default=True)
@click.option("--occurred-at", help="RFC3339 时间，缺省=服务端当前时间")
@click.option("--daily-report-id", help="关联日报 ID（须为自己的日报）")
@click.option("--raw-snippet", help="日报原文片段（必须同时给 --daily-report-id，"
                                    "且片段须能模糊匹配日报原文，服务端校验）")
@click.pass_context
def logs_create(ctx, project_id, content, content_file, category, occurred_at,
                daily_report_id, raw_snippet):
    """录入项目日志（需项目 member+）。

    典型闭环：logs create →（可选）logs update --confirm → daily-report submit。
    """
    text = _read_content(content, content_file, "日志正文（--content 或 --file）")
    run_command(ctx, commands.run_logs_create, project_id=project_id, content=text,
                category=category, occurred_at=occurred_at,
                daily_report_id=daily_report_id, raw_snippet=raw_snippet)


@logs.command("update")
@click.argument("log_id")
@click.option("--content")
@click.option("--category", type=click.Choice(list(commands.LOG_CATEGORIES)))
@click.option("--occurred-at", help="RFC3339 时间")
@click.option("--confirm", is_flag=True,
              help="置 content_status=confirmed（后端唯一允许的显式状态值；日报提交后日志会被锁定）")
@click.pass_context
def logs_update(ctx, log_id, content, category, occurred_at, confirm):
    """更新日志（member+ 改自己的 / maintainer+ 改任意；仅 draft 态可改，项目须 active）"""
    run_command(ctx, commands.run_logs_update, log_id=log_id, content=content,
                category=category, occurred_at=occurred_at, confirm=confirm)


def _read_content(content, content_file, what):
    """--content 与 --file 二选一：返回正文文本（--file 支持 '-' 表示 stdin）。"""
    if bool(content) == bool(content_file):
        raise click.UsageError(f"需要且只需要一种来源：{what}")
    if content_file:
        if content_file == "-":
            return sys.stdin.read()
        with open(content_file, encoding="utf-8") as fh:
            return fh.read()
    return content


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


@experiences.command("get")
@click.argument("experience_id")
@click.pass_context
def experiences_get(ctx, experience_id):
    """经验详情（viewer+）"""
    run_command(ctx, commands.run_experiences_get, experience_id=experience_id)


@experiences.command("create")
@click.option("--title", required=True)
@click.option("--content", required=True)
@click.option("--project-id", help="归属项目（缺省=全局经验，仅 admin 可建）")
@click.option("--tag", "tags", multiple=True, help="标签（可多次）")
@click.option("--linked-project", "linked_projects", multiple=True,
              help="关联项目，格式 ID[:relation]（relation ∈ primary/applicable/derived_from，可多次）")
@click.pass_context
def experiences_create(ctx, title, content, project_id, tags, linked_projects):
    """创建经验候选（member+；发布走 experiences publish）"""
    run_command(ctx, commands.run_experiences_create, title=title, content=content,
                project_id=project_id, tags=list(tags) or None,
                linked_projects=commands.parse_linked_projects(list(linked_projects)))


@experiences.command("update")
@click.argument("experience_id")
@click.option("--title")
@click.option("--content")
@click.option("--tag", "tags", multiple=True, help="标签（可多次；整体替换）")
@click.option("--linked-project", "linked_projects", multiple=True,
              help="关联项目 ID[:relation]（可多次；整体替换）")
@click.pass_context
def experiences_update(ctx, experience_id, title, content, tags, linked_projects):
    """编辑经验（仅 candidate 态；作者/admin/项目 maintainer+）"""
    run_command(ctx, commands.run_experiences_update, experience_id=experience_id,
                title=title, content=content, tags=list(tags) or None,
                linked_projects=commands.parse_linked_projects(list(linked_projects)))


@experiences.command("archive")
@click.argument("experience_id")
@click.pass_context
def experiences_archive(ctx, experience_id):
    """归档经验（须 published；项目 owner，全局仅 admin）"""
    run_command(ctx, commands.run_experiences_archive, experience_id=experience_id)


# ---------------------------------------------------------------- attachments
@cli.group("attachments")
def attachments():
    """附件：upload / list / download（link/unlink/rm 见对应命令）"""


@attachments.command("upload")
@click.argument("file", type=click.Path(exists=True, dir_okay=False))
@click.option("--entity-type", type=click.Choice(list(commands.ATTACHMENT_ENTITY_TYPES)),
              help="绑定实体类型（与 --entity-id 必须成对；需目标实体 write 权限）")
@click.option("--entity-id", help="绑定实体 ID（与 --entity-type 成对）")
@click.option("--description")
@click.pass_context
def attachments_upload(ctx, file, entity_type, entity_id, description):
    """上传附件（≤100 MiB；同 sha256 文件秒传复用，返回 {attachment, links}）"""
    try:
        run_command(ctx, commands.run_attachments_upload, path=file,
                    entity_type=entity_type, entity_id=entity_id, description=description)
    except click.exceptions.Exit as exc:
        # 上传 5xx 后同幂等键重试会撞服务端幂等唯一索引（409 duplicate_idempotency_key，
        # 属服务端防重放语义）：提示先核实再重试，避免盲目重发造成重复附件。
        last = ctx.obj.get("_last_error")
        if last is not None and last.code == "duplicate_idempotency_key":
            click.echo("提示：该幂等键已被服务端记录（上次尝试可能已写入）。"
                       "请先用 labctl attachments list 核实是否上传成功，不要盲目重试。",
                       err=True)
        raise


@attachments.command("list")
@click.option("--entity-type", type=click.Choice(list(commands.ATTACHMENT_ENTITY_TYPES)),
              help="按实体过滤（必须与 --entity-id 成对；都不传=本人未绑定附件）")
@click.option("--entity-id")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def attachments_list(ctx, entity_type, entity_id, page, per_page):
    """附件列表（默认本人未绑定附件，admin 为全量）"""
    run_command(ctx, commands.run_attachments_list, entity_type=entity_type,
                entity_id=entity_id, page=page, per_page=per_page)


@attachments.command("download")
@click.argument("attachment_id")
@click.option("--output", type=click.Path(dir_okay=False),
              help="保存路径（缺省用附件 original_name 存当前目录）")
@click.pass_context
def attachments_download(ctx, attachment_id, output):
    """下载附件（含服务端 sha256 一致性校验）"""
    run_command(ctx, commands.run_attachments_download,
                attachment_id=attachment_id, output=output)


@attachments.command("link")
@click.argument("attachment_id")
@click.option("--entity-type", required=True,
              type=click.Choice(list(commands.ATTACHMENT_ENTITY_TYPES)))
@click.option("--entity-id", required=True)
@click.option("--description")
@click.pass_context
def attachments_link(ctx, attachment_id, entity_type, entity_id, description):
    """把附件补挂到实体（附件可读 + 目标实体 write）"""
    run_command(ctx, commands.run_attachments_link, attachment_id=attachment_id,
                entity_type=entity_type, entity_id=entity_id, description=description)


@attachments.command("unlink")
@click.argument("attachment_id")
@click.argument("link_id")
@click.pass_context
def attachments_unlink(ctx, attachment_id, link_id):
    """解除附件与某实体的关联"""
    run_command(ctx, commands.run_attachments_unlink,
                attachment_id=attachment_id, link_id=link_id)


@attachments.command("rm")
@click.argument("attachment_id")
@click.confirmation_option(prompt="确认删除该附件？（软删，上传者本人或 admin）")
@click.pass_context
def attachments_rm(ctx, attachment_id):
    """删除附件（软删）"""
    run_command(ctx, commands.run_attachments_rm, attachment_id=attachment_id)


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


# ---------------------------------------------------------------- todos
@cli.group("todos")
def todos():
    """待办：list / add / edit / done / defer / rm"""


@todos.command("list")
@click.option("--date", help="YYYY-MM-DD（默认今天）")
@click.option("--scope", type=click.Choice(list(commands.TODO_SCOPES)))
@click.option("--status", type=click.Choice(list(commands.TODO_STATUSES)),
              help="open（默认）/done/cancelled/all")
@click.option("--limit", type=int, default=100, show_default=True)
@click.pass_context
def todos_list(ctx, date, scope, status, limit):
    """待办列表（仅本人/自己参与的共享项）"""
    run_command(ctx, commands.run_todos_list, date=date or "", scope=scope or "",
                status=status or "", limit=limit)


@todos.command("add")
@click.argument("title")
@click.option("--priority", type=click.Choice(list(commands.TODO_PRIORITIES)))
@click.option("--project-id", help="共享到项目（须为该项目成员）")
@click.pass_context
def todos_add(ctx, title, priority, project_id):
    """新建待办"""
    run_command(ctx, commands.run_todos_add, title=title, priority=priority,
                project_id=project_id)


@todos.command("edit")
@click.argument("todo_id")
@click.option("--updated-at", help="乐观锁版本（RFC3339，取自 todos list 输出；缺省自动从列表取）")
@click.option("--title")
@click.option("--priority", type=click.Choice(list(commands.TODO_PRIORITIES)))
@click.option("--project-id", help="改挂到其他项目（共享）")
@click.option("--clear-project", is_flag=True, help="取消共享（project_id 置空）")
@click.pass_context
def todos_edit(ctx, todo_id, updated_at, title, priority, project_id, clear_project):
    """编辑待办（仅 owner；版本冲突返回 409 时重新 list 取新 updated_at）"""
    run_command(ctx, commands.run_todos_edit, todo_id=todo_id, updated_at=updated_at or "",
                title=title, priority=priority, project_id=project_id,
                clear_project=clear_project)


@todos.command("done")
@click.argument("todo_id")
@click.pass_context
def todos_done(ctx, todo_id):
    """完成待办（owner 或共享项目 active 非 viewer 成员）"""
    run_command(ctx, commands.run_todos_done, todo_id=todo_id)


@todos.command("defer")
@click.argument("todo_id")
@click.pass_context
def todos_defer(ctx, todo_id):
    """推迟待办到明天（仅 owner）"""
    run_command(ctx, commands.run_todos_defer, todo_id=todo_id)


@todos.command("rm")
@click.argument("todo_id")
@click.pass_context
def todos_rm(ctx, todo_id):
    """删除待办（仅 owner）"""
    run_command(ctx, commands.run_todos_rm, todo_id=todo_id)


# ---------------------------------------------------------------- audit
@cli.group("audit")
def audit():
    """审计（admin/maintainer）：events / verify / get"""


@audit.command("events")
@click.option("--action", help="按审计动作过滤（如 logs.create）")
@click.option("--user-id", help="按用户 UUID 过滤")
@click.option("--actor-type", help="按执行者类型过滤")
@click.option("--from", "from_", help="起始时间 RFC3339")
@click.option("--to", "to_", help="结束时间 RFC3339")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def audit_events(ctx, action, user_id, actor_type, from_, to_, page, per_page):
    """审计事件列表"""
    run_command(ctx, commands.run_audit_events, action=action or "", user_id=user_id or "",
                actor_type=actor_type or "", from_=from_ or "", to_=to_ or "",
                page=page, per_page=per_page)


@audit.command("verify")
@click.option("--from-id", type=int, default=0, help="起始审计事件 id（缺省从头）")
@click.option("--to-id", type=int, default=0, help="结束审计事件 id（缺省到最新）")
@click.pass_context
def audit_verify(ctx, from_id, to_id):
    """增量校验审计 hash 链（SHA-256 防篡改）"""
    run_command(ctx, commands.run_audit_verify, from_id=from_id, to_id=to_id)


@audit.command("get")
@click.argument("request_id")
@click.pass_context
def audit_get(ctx, request_id):
    """按 request_id 查单条审计轨迹"""
    run_command(ctx, commands.run_audit_get, request_id=request_id)


# ---------------------------------------------------------------- sensors
@cli.group("sensors")
def sensors():
    """传感器：latest / history（InfluxDB 时序）"""


@sensors.command("latest")
@click.option("--tags", help="measurement 列表（逗号分隔，须 ∈ 服务端配置；缺省全部默认项）")
@click.pass_context
def sensors_latest(ctx, tags):
    """最新读数（固定查最近 1h 的 last()）"""
    run_command(ctx, commands.run_sensors_latest, tags=tags or "")


@sensors.command("history")
@click.option("--tag", required=True, help="measurement 名（单个）")
@click.option("--from", "from_", default="-1h", show_default=True,
              help="Flux range 表达式（如 -1h / now()-24h / RFC3339），不是普通时间参数")
@click.option("--to", "to_", default="", help="Flux range 表达式（如 now()），缺省不传")
@click.option("--interval", default="", help="Flux duration 降采样窗口（如 5m），缺省不降采样")
@click.pass_context
def sensors_history(ctx, tag, from_, to_, interval):
    """历史序列（from/to/interval 均为 Flux 字面量）"""
    run_command(ctx, commands.run_sensors_history, tag=tag, from_=from_, to=to_,
                interval=interval)


# ---------------------------------------------------------------- ask
@cli.group("ask")
def ask():
    """AI 问答：chat / history（JWT 通道）"""


@ask.command("chat")
@click.argument("question")
@click.pass_context
def ask_chat(ctx, question):
    """AI 问答（限流 10 次/min；502=AI 降级中）"""
    run_command(ctx, commands.run_ask_chat, question=question)


@ask.command("history")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def ask_history(ctx, page, per_page):
    """本人问答历史"""
    run_command(ctx, commands.run_ask_history, page=page, per_page=per_page)


# ---------------------------------------------------------------- assembly
@cli.group("assembly")
def assembly():
    """装配步骤：list / transition"""


@assembly.command("list")
@click.argument("project_id")
@click.option("--status", type=click.Choice(["planned", "in_progress", "paused",
                                             "completed", "skipped", "cancelled"]))
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def assembly_list(ctx, project_id, status, page, per_page):
    """项目装配步骤列表（viewer+）"""
    run_command(ctx, commands.run_assembly_list, project_id=project_id, status=status,
                page=page, per_page=per_page)


@assembly.command("transition")
@click.argument("step_id")
@click.option("--action", required=True, type=click.Choice(list(commands.ASSEMBLY_TRANSITIONS)),
              help="start/pause/resume/complete/skip/cancel（由服务端按当前状态校验）")
@click.option("--override-reason", help="依赖步骤 cancelled 时 start 越过依赖的理由（随审计）")
@click.pass_context
def assembly_transition(ctx, step_id, action, override_reason):
    """流转装配步骤状态（member+）"""
    run_command(ctx, commands.run_assembly_transition, step_id=step_id, action=action,
                override_reason=override_reason)


# ---------------------------------------------------------------- step-templates
@cli.group("step-templates")
def step_templates():
    """步骤模板：list / generate（AI 生成）"""


@step_templates.command("list")
@click.option("--kind", type=click.Choice(list(commands.STEP_TEMPLATE_KINDS)))
@click.option("--q", help="名称模糊搜索")
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def step_templates_list(ctx, kind, q, page, per_page):
    """步骤模板列表（viewer+）"""
    run_command(ctx, commands.run_step_templates_list, kind=kind or "", q=q or "",
                page=page, per_page=per_page)


@step_templates.command("generate")
@click.option("--kind", required=True, type=click.Choice(list(commands.STEP_TEMPLATE_KINDS)))
@click.option("--prompt", required=True, help="生成要求（1-4000 字符）")
@click.option("--context", help="补充上下文 JSON 对象（可选）")
@click.pass_context
def step_templates_generate(ctx, kind, prompt, context):
    """AI 生成步骤模板（member+；限流 10 次/min）"""
    parsed_context = None
    if context:
        try:
            parsed_context = json.loads(context)
        except ValueError:
            raise click.UsageError("--context 须为合法 JSON 对象")
    run_command(ctx, commands.run_step_templates_generate, kind=kind, prompt=prompt,
                context=parsed_context)


# ---------------------------------------------------------------- rf-matching
@cli.group("rf-matching")
def rf_matching():
    """RF 匹配记录：list / create"""


@rf_matching.command("list")
@click.argument("project_id")
@click.option("--device", type=click.Choice(list(commands.RF_DEVICES)))
@click.option("--status", type=click.Choice(list(commands.RF_STATUSES)))
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def rf_matching_list(ctx, project_id, device, status, page, per_page):
    """项目 RF 匹配记录列表（viewer+）"""
    run_command(ctx, commands.run_rf_matching_list, project_id=project_id, device=device,
                status=status, page=page, per_page=per_page)


@rf_matching.command("create")
@click.argument("project_id")
@click.option("--device", required=True, type=click.Choice(list(commands.RF_DEVICES)))
@click.option("--frequency-mhz", required=True, type=float)
@click.option("--status", required=True, type=click.Choice(list(commands.RF_STATUSES)),
              help="pass/adjust/fail")
@click.option("--s11", type=float)
@click.option("--input-freq", type=float)
@click.option("--input-voltage", type=float)
@click.option("--input-power", type=float)
@click.option("--input-desc")
@click.option("--output-freq", type=float)
@click.option("--output-voltage", type=float)
@click.option("--output-power", type=float)
@click.option("--output-desc")
@click.option("--transformer-turns")
@click.option("--capacitance-text")
@click.option("--transformer-material")
@click.option("--shunt-inductance")
@click.option("--series-capacitor")
@click.option("--notes")
@click.option("--measured-at", help="RFC3339 时间")
@click.pass_context
def rf_matching_create(ctx, project_id, device, frequency_mhz, status, s11, input_freq,
                       input_voltage, input_power, input_desc, output_freq, output_voltage,
                       output_power, output_desc, transformer_turns, capacitance_text,
                       transformer_material, shunt_inductance, series_capacitor, notes,
                       measured_at):
    """录入 RF 匹配记录（member+）"""
    run_command(ctx, commands.run_rf_matching_create, project_id=project_id, device=device,
                frequency_mhz=frequency_mhz, status=status, s11=s11, input_freq=input_freq,
                input_voltage=input_voltage, input_power=input_power, input_desc=input_desc,
                output_freq=output_freq, output_voltage=output_voltage,
                output_power=output_power, output_desc=output_desc,
                transformer_turns=transformer_turns, capacitance_text=capacitance_text,
                transformer_material=transformer_material,
                shunt_inductance=shunt_inductance, series_capacitor=series_capacitor,
                notes=notes, measured_at=measured_at)


# ---------------------------------------------------------------- admin
@cli.group("admin")
def admin():
    """管理（admin）：users / invites"""


@admin.group("users")
def admin_users():
    """用户管理：list / create / set / reset-password"""


@admin_users.command("list")
@click.pass_context
def admin_users_list(ctx):
    """用户列表"""
    run_command(ctx, commands.run_admin_users_list)


@admin_users.command("create")
@click.option("--username", required=True)
@click.option("--display-name")
@click.option("--role", type=click.Choice(list(commands.USER_ROLES)))
@click.option("--password", help="初始密码（缺省服务端生成 temporary_password）")
@click.pass_context
def admin_users_create(ctx, username, display_name, role, password):
    """创建用户"""
    run_command(ctx, commands.run_admin_users_create, username=username,
                display_name=display_name, role=role, password=password)


@admin_users.command("set")
@click.argument("user_id")
@click.option("--display-name")
@click.option("--role", type=click.Choice(list(commands.USER_ROLES)))
@click.option("--disable", "disabled", flag_value=True, default=None,
              help="禁用账户（与 --enable 互斥）")
@click.option("--enable", "disabled", flag_value=False,
              help="启用账户")
@click.pass_context
def admin_users_set(ctx, user_id, display_name, role, disabled):
    """更新用户（display_name/role/启停）"""
    run_command(ctx, commands.run_admin_users_set, user_id=user_id,
                display_name=display_name, role=role, disabled=disabled)


@admin_users.command("reset-password")
@click.argument("user_id")
@click.option("--new-password", help="新密码（缺省服务端生成 temporary_password）")
@click.pass_context
def admin_users_reset_password(ctx, user_id, new_password):
    """重置用户密码"""
    run_command(ctx, commands.run_admin_users_reset_password, user_id=user_id,
                new_password=new_password)


@admin.group("invites")
def admin_invites():
    """邀请码：list / create / revoke"""


@admin_invites.command("list")
@click.pass_context
def admin_invites_list(ctx):
    """邀请码列表"""
    run_command(ctx, commands.run_admin_invites_list)


@admin_invites.command("create")
@click.option("--expires-at", help="过期时间 RFC3339（可选）")
@click.pass_context
def admin_invites_create(ctx, expires_at):
    """创建邀请码"""
    run_command(ctx, commands.run_admin_invites_create, expires_at=expires_at)


@admin_invites.command("revoke")
@click.argument("invite_id")
@click.pass_context
def admin_invites_revoke(ctx, invite_id):
    """吊销邀请码"""
    run_command(ctx, commands.run_admin_invites_revoke, invite_id=invite_id)


# ---------------------------------------------------------------- automation
@cli.group("automation")
def automation():
    """自动化规则（admin）：rules"""


@automation.group("rules")
def automation_rules():
    """规则管理：list / create / enable / rm"""


@automation_rules.command("list")
@click.pass_context
def automation_rules_list(ctx):
    """规则列表"""
    run_command(ctx, commands.run_automation_rules_list)


@automation_rules.command("create")
@click.option("--name", required=True)
@click.option("--trigger-event", default="daily_report.submitted", show_default=True,
              help="触发事件（一期仅 daily_report.submitted）")
@click.pass_context
def automation_rules_create(ctx, name, trigger_event):
    """创建规则（动作固定为 enqueue_agent_task）"""
    run_command(ctx, commands.run_automation_rules_create, name=name,
                trigger_event=trigger_event)


@automation_rules.command("enable")
@click.argument("rule_id")
@click.pass_context
def automation_rules_enable(ctx, rule_id):
    """启用规则"""
    run_command(ctx, commands.run_automation_rules_enable, rule_id=rule_id, enabled=True)


@automation_rules.command("disable")
@click.argument("rule_id")
@click.pass_context
def automation_rules_disable(ctx, rule_id):
    """停用规则"""
    run_command(ctx, commands.run_automation_rules_enable, rule_id=rule_id, enabled=False)


@automation_rules.command("rm")
@click.argument("rule_id")
@click.pass_context
def automation_rules_rm(ctx, rule_id):
    """删除规则"""
    run_command(ctx, commands.run_automation_rules_rm, rule_id=rule_id)


# ---------------------------------------------------------------- agent candidates
@cli.group("agent")
def agent():
    """Agent 候选审核（admin/maintainer）：candidates"""


@agent.group("candidates")
def agent_candidates():
    """候选队列：list / trace / approve / reject"""


@agent_candidates.command("list")
@click.option("--status", type=click.Choice(list(commands.CANDIDATE_STATUSES)))
@click.option("--page", type=int, default=1, show_default=True)
@click.option("--per-page", type=int, default=20, show_default=True)
@click.pass_context
def agent_candidates_list(ctx, status, page, per_page):
    """候选动作列表（默认 pending_review）"""
    run_command(ctx, commands.run_agent_candidates_list, status=status or "",
                page=page, per_page=per_page)


@agent_candidates.command("trace")
@click.argument("candidate_id")
@click.pass_context
def agent_candidates_trace(ctx, candidate_id):
    """候选动作完整 AI 时间线（回溯）"""
    run_command(ctx, commands.run_agent_candidates_trace, candidate_id=candidate_id)


@agent_candidates.command("approve")
@click.argument("candidate_id")
@click.pass_context
def agent_candidates_approve(ctx, candidate_id):
    """批准候选（服务端执行落库）"""
    run_command(ctx, commands.run_agent_candidates_approve, candidate_id=candidate_id)


@agent_candidates.command("reject")
@click.argument("candidate_id")
@click.option("--reason", default="", help="拒绝原因（随审计）")
@click.pass_context
def agent_candidates_reject(ctx, candidate_id, reason):
    """拒绝候选"""
    run_command(ctx, commands.run_agent_candidates_reject, candidate_id=candidate_id,
                reason=reason)


# ---------------------------------------------------------------- instruments
@cli.group("instruments")
def instruments():
    """仪器（只读）：list / status / whitelist / parse-result

    写命令（commands/leases/approvals/gascell/piezo）刻意不进 CLI——仪器安全链路
    （租约+审批+白名单分级）在 Web 有完整 UX 支撑，CLI 略过审批链容易误操作。
    """


@instruments.command("list")
@click.pass_context
def instruments_list(ctx):
    """仪器列表（含在线状态）"""
    run_command(ctx, commands.run_instruments_list)


@instruments.command("status")
@click.argument("instrument_id")
@click.pass_context
def instruments_status(ctx, instrument_id):
    """单台仪器状态"""
    run_command(ctx, commands.run_instruments_status, instrument_id=instrument_id)


@instruments.command("whitelist")
@click.pass_context
def instruments_whitelist(ctx):
    """命令白名单（参数范围以 docs/仪器白名单.yaml 为准）"""
    run_command(ctx, commands.run_instruments_whitelist)


@instruments.command("parse-result")
@click.argument("instrument_id")
@click.option("--command", required=True, help="仪器命令名（白名单内）")
@click.option("--response", required=True, help="仪器原始响应文本")
@click.pass_context
def instruments_parse_result(ctx, instrument_id, command, response):
    """只读解析仪器响应（不发命令，viewer+）"""
    run_command(ctx, commands.run_instruments_parse_result, instrument_id=instrument_id,
                command=command, response=response)


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
