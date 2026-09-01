"""labctl MCP Server（stdio，FastMCP）。

所有工具复用 cli/commands.py 的命令执行函数（不重复实现），全部经 REST 调用
服务端，权限/审计/限流由服务端兜底。MCP 自身无独立鉴权——安全边界 = 启动
进程所持 token 的权限：仅限内网/受信主机启动，不在公网暴露 stdio 桥。

启动（仓库根目录）：
    py-agent/.venv/bin/python -m cli.mcp_server

会话建立两种方式：
    1. 调用 labctl_login 工具（凭证仅存进程内存，不落盘）；
    2. 启动时设置 LABCTL_SERVICE_TOKEN（服务账号 JWT，只读为主，见 README）。
"""

import json
import os
import sys

if __package__ in (None, ""):
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from mcp.server.fastmcp import FastMCP

from cli import commands
from cli.api_client import DEFAULT_BASE_URL, LabctlAPI, LabctlError

mcp = FastMCP("labctl")

_session = {"api": None}


def _get_api():
    api = _session.get("api")
    if api is not None:
        return api
    if os.getenv("LABCTL_SERVICE_TOKEN"):
        return LabctlAPI.from_env()
    raise LabctlError("未登录：请先调用 labctl_login 建立会话，或设置 LABCTL_SERVICE_TOKEN 环境变量",
                      code="not_logged_in")


def _call(fn, **kwargs):
    try:
        return json.dumps(fn(_get_api(), **kwargs), ensure_ascii=False, indent=2)
    except LabctlError as exc:
        return json.dumps({"error": exc.to_dict()}, ensure_ascii=False, indent=2)


# ---------------------------------------------------------------- 会话
@mcp.tool()
def labctl_login(username: str, password: str, base_url: str = DEFAULT_BASE_URL) -> str:
    """登录并建立会话（凭证仅存于本进程内存，不落盘）。"""
    api = LabctlAPI(base_url=base_url)
    try:
        data = api.login(username, password)
    except LabctlError as exc:
        return json.dumps({"error": exc.to_dict()}, ensure_ascii=False, indent=2)
    _session["api"] = api
    return json.dumps({"username": username, "user": data.get("user"), "success": True},
                      ensure_ascii=False, indent=2)


@mcp.tool()
def labctl_logout() -> str:
    """注销当前会话（撤销 refresh token）。"""
    try:
        return _call(commands.run_logout)
    finally:
        _session["api"] = None


@mcp.tool()
def labctl_whoami() -> str:
    """查看当前会话用户信息。"""
    return _call(commands.run_whoami)


# ---------------------------------------------------------------- 日报
@mcp.tool()
def labctl_daily_report_today() -> str:
    """获取/创建今日日报。"""
    return _call(commands.run_daily_report_today)


@mcp.tool()
def labctl_daily_report_history(status: str = "", keyword: str = "", date: str = "",
                                page: int = 1, per_page: int = 20) -> str:
    """查询本人日报列表。status/date 可选；date 格式 YYYY-MM-DD。"""
    return _call(commands.run_daily_report_history,
                 status=status, keyword=keyword, date=date, page=page, per_page=per_page)


@mcp.tool()
def labctl_daily_report_entry(report_id: str, raw_text: str = None,
                              summary: str = None) -> str:
    """查看单份日报；传入 raw_text/summary 则更新（仅作者本人、draft 态）。"""
    return _call(commands.run_daily_report_entry,
                 report_id=report_id, raw_text=raw_text, summary=summary)


@mcp.tool()
def labctl_daily_report_submit(report_id: str, force: bool = False) -> str:
    """提交日报。返回 {report, warnings, blocked}：blocked=true 表示有警告未强制，加 force 重提。"""
    return _call(commands.run_daily_report_submit, report_id=report_id, force=force)


@mcp.tool()
def labctl_daily_report_ai_parse(report_id: str) -> str:
    """触发日报 AI 解析（仅作者+draft；限流 10 次/min）。返回三态 status=ok/clarify/rejected。"""
    return _call(commands.run_daily_report_ai_parse, report_id=report_id)


# ---------------------------------------------------------------- 项目
@mcp.tool()
def labctl_projects_list(status: str = "") -> str:
    """项目列表（默认只看自己 active 成员的项目）。"""
    return _call(commands.run_projects_list, status=status)


@mcp.tool()
def labctl_projects_get(project_id: str) -> str:
    """项目详情。"""
    return _call(commands.run_projects_get, project_id=project_id)


@mcp.tool()
def labctl_projects_create(code: str, name: str, short_name: str = None,
                           description: str = None, visibility: str = None,
                           start_date: str = None, target_end_date: str = None,
                           default_category: str = None, tags: str = None) -> str:
    """创建项目（需 maintainer/admin 角色）。visibility 可选 restricted/workspace。"""
    return _call(commands.run_projects_create, code=code, name=name,
                 short_name=short_name, description=description, visibility=visibility,
                 start_date=start_date, target_end_date=target_end_date,
                 default_category=default_category, tags=tags)


@mcp.tool()
def labctl_projects_update(project_id: str, name: str = None, short_name: str = None,
                           description: str = None, visibility: str = None,
                           comment_policy: str = None, start_date: str = None,
                           target_end_date: str = None, default_category: str = None,
                           tags: str = None) -> str:
    """更新项目（需 manage_project）。visibility: restricted/workspace；
    comment_policy: everyone/members/disabled。"""
    return _call(commands.run_projects_update, project_id=project_id, name=name,
                 short_name=short_name, description=description, visibility=visibility,
                 comment_policy=comment_policy, start_date=start_date,
                 target_end_date=target_end_date, default_category=default_category,
                 tags=tags)


@mcp.tool()
def labctl_projects_transition(project_id: str, action: str,
                               ignore_warnings: bool = False,
                               reason: str = None) -> str:
    """流转项目状态。action: activate/complete/archive/reactivate/deactivate/reopen；
    有警告时须 ignore_warnings=true。"""
    return _call(commands.run_projects_transition, project_id=project_id, action=action,
                 ignore_warnings=ignore_warnings, reason=reason)


@mcp.tool()
def labctl_projects_members_list(project_id: str) -> str:
    """项目成员列表（viewer+）。"""
    return _call(commands.run_projects_members_list, project_id=project_id)


@mcp.tool()
def labctl_projects_members_add(project_id: str, user_id: str, role: str) -> str:
    """添加项目成员（需 manage_members）。role: owner/maintainer/member/viewer。"""
    return _call(commands.run_projects_members_add,
                 project_id=project_id, user_id=user_id, role=role)


@mcp.tool()
def labctl_projects_members_set_role(project_id: str, user_id: str, role: str) -> str:
    """调整项目成员角色（最后一个 owner 降级会被服务端拒绝）。"""
    return _call(commands.run_projects_members_set_role,
                 project_id=project_id, user_id=user_id, role=role)


@mcp.tool()
def labctl_projects_members_remove(project_id: str, user_id: str) -> str:
    """移除项目成员（移除最后一个 active owner 会被服务端拒绝）。"""
    return _call(commands.run_projects_members_remove,
                 project_id=project_id, user_id=user_id)


# ---------------------------------------------------------------- 问题
@mcp.tool()
def labctl_issues_list(project_id: str, status: str = "", severity: str = "",
                       search: str = "", assignee: str = "", page: int = 1,
                       per_page: int = 20) -> str:
    """项目问题列表。status: open/in_progress/resolved/closed；severity: low/medium/high/critical。"""
    return _call(commands.run_issues_list, project_id=project_id,
                 status=status, severity=severity, search=search, assignee=assignee,
                 page=page, per_page=per_page)


@mcp.tool()
def labctl_issues_create(project_id: str, title: str, description: str = None,
                         severity: str = "medium", assignee_id: str = None) -> str:
    """创建问题（需项目 member+）。"""
    return _call(commands.run_issues_create, project_id=project_id, title=title,
                 description=description, severity=severity, assignee_id=assignee_id)


@mcp.tool()
def labctl_issues_transition(issue_id: str, target_status: str, reason: str = None) -> str:
    """流转问题状态。target_status: open/in_progress/resolved/closed。"""
    return _call(commands.run_issues_transition, issue_id=issue_id,
                 target_status=target_status, reason=reason)


@mcp.tool()
def labctl_issues_get(issue_id: str) -> str:
    """问题详情（viewer+）。"""
    return _call(commands.run_issues_get, issue_id=issue_id)


@mcp.tool()
def labctl_issues_update(issue_id: str, title: str = None, description: str = None,
                         severity: str = None, assignee_id: str = None) -> str:
    """更新问题（member+；closed 后拒绝修改）。severity: low/medium/high/critical。"""
    return _call(commands.run_issues_update, issue_id=issue_id, title=title,
                 description=description, severity=severity, assignee_id=assignee_id)


@mcp.tool()
def labctl_issues_comment(issue_id: str, content: str) -> str:
    """评论问题（按项目 comment_policy）。"""
    return _call(commands.run_issues_comment, issue_id=issue_id, content=content)


# ---------------------------------------------------------------- 测试数据
@mcp.tool()
def labctl_test_data_list(project_id: str, run_id: str = None, data_type: str = None,
                          quality: str = None, page: int = 1, per_page: int = 20) -> str:
    """测试数据列表。data_type: cryo/pressure/voltage/rf_voltage/efficiency。"""
    return _call(commands.run_test_data_list, project_id=project_id,
                 run_id=run_id, data_type=data_type, quality=quality, page=page, per_page=per_page)


@mcp.tool()
def labctl_test_data_entry(project_id: str, data_type: str, measurement: str, value: float,
                           unit: str = None, quality: str = "normal", source: str = None,
                           measured_at: str = None, run_id: str = None,
                           notes: str = None) -> str:
    """录入单条测试数据（需项目 member+）。quality: normal/outlier/suspect/invalid；
    source: manual/instrument/import/backfill。"""
    return _call(commands.run_test_data_entry, project_id=project_id,
                 data_type=data_type, measurement=measurement, value=value, unit=unit,
                 quality=quality, source=source, measured_at=measured_at,
                 run_id=run_id, notes=notes)


@mcp.tool()
def labctl_test_data_batch(project_id: str, rows: list) -> str:
    """批量录入测试数据（1-100 行裸数组）。rows 元素字段：data_type/measurement/value
    （必填）+ unit/quality/source/measured_at/run_id/notes（可选）；任一行失败整批 422。"""
    return _call(commands.run_test_data_batch, project_id=project_id, rows=rows)


@mcp.tool()
def labctl_test_data_get(entry_id: str) -> str:
    """单条测试数据详情（viewer+）。"""
    return _call(commands.run_test_data_get, entry_id=entry_id)


@mcp.tool()
def labctl_test_data_invalidate(entry_id: str) -> str:
    """软失效单条测试数据（记录者本人或项目 owner）。"""
    return _call(commands.run_test_data_invalidate, entry_id=entry_id)


# ---------------------------------------------------------------- 实验批次
@mcp.tool()
def labctl_runs_list(project_id: str, campaign: str = None, status: str = None,
                     run_type: str = None, page: int = 1, per_page: int = 20) -> str:
    """实验批次列表。status: planned/active/paused/completed/aborted。"""
    return _call(commands.run_runs_list, project_id=project_id, campaign=campaign,
                 status=status, run_type=run_type, page=page, per_page=per_page)


@mcp.tool()
def labctl_runs_get(run_id: str) -> str:
    """实验批次详情。"""
    return _call(commands.run_runs_get, run_id=run_id)


@mcp.tool()
def labctl_runs_status(run_id: str, action: str) -> str:
    """流转批次状态。action: start/abort/pause/complete/resume（服务端按当前状态校验；
    需 maintainer+ 或批次创建者本人）。"""
    return _call(commands.run_runs_status, run_id=run_id, action=action)


@mcp.tool()
def labctl_runs_create(project_id: str, name: str, campaign: str = None,
                       run_type: str = None, gas_type: str = None, target_temp: float = None,
                       min_temp: float = None, pressure_min: float = None,
                       pressure_max: float = None, pressure_unit: str = None,
                       has_beam: bool = None, devices: list = None,
                       description: str = None) -> str:
    """创建实验批次（member+）。run_type: cooldown/warmup/steady_state/test；
    gas_type: He/Ar/Xe；devices 元素: rf_carpet/rfq/qpig。"""
    return _call(commands.run_runs_create, project_id=project_id, name=name,
                 campaign=campaign, run_type=run_type, gas_type=gas_type,
                 target_temp=target_temp, min_temp=min_temp, pressure_min=pressure_min,
                 pressure_max=pressure_max, pressure_unit=pressure_unit,
                 has_beam=has_beam, devices=devices, description=description)


@mcp.tool()
def labctl_runs_delete(run_id: str) -> str:
    """删除实验批次（软删；maintainer+ 或创建者本人）。"""
    return _call(commands.run_runs_delete, run_id=run_id)


# ---------------------------------------------------------------- 告警
@mcp.tool()
def labctl_alerts_list(status: str = "active", limit: int = 50, offset: int = 0) -> str:
    """告警列表。status: active/resolved。"""
    return _call(commands.run_alerts_list, status=status, limit=limit, offset=offset)


@mcp.tool()
def labctl_alerts_resolve(alert_id: str) -> str:
    """解除告警（需 admin/maintainer 角色）。"""
    return _call(commands.run_alerts_resolve, alert_id=alert_id)


@mcp.tool()
def labctl_alerts_get(alert_id: str) -> str:
    """告警详情。"""
    return _call(commands.run_alerts_get, alert_id=alert_id)


# ---------------------------------------------------------------- 日志
@mcp.tool()
def labctl_logs_list(project_id: str, category: str = None, date_from: str = None,
                     date_to: str = None, status: str = None, page: int = 1,
                     per_page: int = 20) -> str:
    """项目日志列表。category: general/assembly/test/cryo/rf/vacuum/beam/data_analysis。"""
    return _call(commands.run_logs_list, project_id=project_id, category=category,
                 date_from=date_from, date_to=date_to, status=status, page=page, per_page=per_page)


@mcp.tool()
def labctl_logs_get(log_id: str) -> str:
    """日志详情。"""
    return _call(commands.run_logs_get, log_id=log_id)


@mcp.tool()
def labctl_logs_create(project_id: str, content: str, category: str = "general",
                       occurred_at: str = None, daily_report_id: str = None,
                       raw_snippet: str = None) -> str:
    """录入项目日志（member+；source 固定 manual）。category:
    general/assembly/test/cryo/rf/vacuum/beam/data_analysis；关联日报时
    raw_snippet 须匹配日报原文。"""
    return _call(commands.run_logs_create, project_id=project_id, content=content,
                 category=category, occurred_at=occurred_at,
                 daily_report_id=daily_report_id, raw_snippet=raw_snippet)


@mcp.tool()
def labctl_logs_update(log_id: str, content: str = None, category: str = None,
                       occurred_at: str = None, confirm: bool = False) -> str:
    """更新日志（member+ 改自己的 / maintainer+ 改任意；仅 draft 态）。
    confirm=true 置 content_status=confirmed。"""
    return _call(commands.run_logs_update, log_id=log_id, content=content,
                 category=category, occurred_at=occurred_at, confirm=confirm)


# ---------------------------------------------------------------- 附件
@mcp.tool()
def labctl_attachments_upload(path: str, entity_type: str = None,
                              entity_id: str = None, description: str = None) -> str:
    """上传附件（≤100 MiB）。path 为 MCP 进程所在主机的本地文件路径（MCP 仅限
    内网受信主机启动）；entity_type+entity_id 成对传则直挂实体（需实体 write 权限）。
    同 sha256 文件秒传复用。"""
    return _call(commands.run_attachments_upload, path=path, entity_type=entity_type,
                 entity_id=entity_id, description=description)


@mcp.tool()
def labctl_attachments_list(entity_type: str = None, entity_id: str = None,
                            page: int = 1, per_page: int = 20) -> str:
    """附件列表：都不传=本人未绑定附件（admin 全量）；成对传=某实体的附件。"""
    return _call(commands.run_attachments_list, entity_type=entity_type,
                 entity_id=entity_id, page=page, per_page=per_page)


@mcp.tool()
def labctl_attachments_download(attachment_id: str, output: str) -> str:
    """下载附件到 MCP 主机本地路径 output（返回本地路径/大小/sha256 与服务端比对结果）。"""
    return _call(commands.run_attachments_download, attachment_id=attachment_id,
                 output=output)


@mcp.tool()
def labctl_attachments_link(attachment_id: str, entity_type: str, entity_id: str,
                            description: str = None) -> str:
    """把附件补挂到实体（附件可读 + 目标实体 write）。"""
    return _call(commands.run_attachments_link, attachment_id=attachment_id,
                 entity_type=entity_type, entity_id=entity_id, description=description)


@mcp.tool()
def labctl_attachments_unlink(attachment_id: str, link_id: str) -> str:
    """解除附件与某实体的关联。"""
    return _call(commands.run_attachments_unlink, attachment_id=attachment_id,
                 link_id=link_id)


@mcp.tool()
def labctl_attachments_rm(attachment_id: str) -> str:
    """删除附件（软删；上传者本人或 admin）。"""
    return _call(commands.run_attachments_rm, attachment_id=attachment_id)


# ---------------------------------------------------------------- 待办
@mcp.tool()
def labctl_todos_list(date: str = "", scope: str = "", status: str = "",
                      limit: int = 100) -> str:
    """待办列表。scope: all/mine/shared；status: open/done/cancelled/all。"""
    return _call(commands.run_todos_list, date=date, scope=scope, status=status,
                 limit=limit)


@mcp.tool()
def labctl_todos_add(title: str, priority: str = None, project_id: str = None) -> str:
    """新建待办。priority: high/medium/low；project_id 共享到项目。"""
    return _call(commands.run_todos_add, title=title, priority=priority,
                 project_id=project_id)


@mcp.tool()
def labctl_todos_edit(todo_id: str, updated_at: str = "", title: str = None,
                      priority: str = None, project_id: str = None,
                      clear_project: bool = False) -> str:
    """编辑待办（仅 owner）。updated_at 为乐观锁版本（缺省自动从列表取）。"""
    return _call(commands.run_todos_edit, todo_id=todo_id, updated_at=updated_at,
                 title=title, priority=priority, project_id=project_id,
                 clear_project=clear_project)


@mcp.tool()
def labctl_todos_done(todo_id: str) -> str:
    """完成待办。"""
    return _call(commands.run_todos_done, todo_id=todo_id)


@mcp.tool()
def labctl_todos_defer(todo_id: str) -> str:
    """推迟待办到明天。"""
    return _call(commands.run_todos_defer, todo_id=todo_id)


@mcp.tool()
def labctl_todos_rm(todo_id: str) -> str:
    """删除待办（仅 owner）。"""
    return _call(commands.run_todos_rm, todo_id=todo_id)


# ---------------------------------------------------------------- 审计
@mcp.tool()
def labctl_audit_events(action: str = "", user_id: str = "", actor_type: str = "",
                        from_: str = "", to_: str = "", page: int = 1,
                        per_page: int = 20) -> str:
    """审计事件列表（admin/maintainer）。from_/to_ 为 RFC3339。"""
    return _call(commands.run_audit_events, action=action, user_id=user_id,
                 actor_type=actor_type, from_=from_, to_=to_, page=page,
                 per_page=per_page)


@mcp.tool()
def labctl_audit_verify(from_id: int = 0, to_id: int = 0) -> str:
    """增量校验审计 hash 链（admin/maintainer）。"""
    return _call(commands.run_audit_verify, from_id=from_id, to_id=to_id)


@mcp.tool()
def labctl_audit_get(request_id: str) -> str:
    """按 request_id 查单条审计轨迹（admin/maintainer）。"""
    return _call(commands.run_audit_get, request_id=request_id)


# ---------------------------------------------------------------- 经验库
@mcp.tool()
def labctl_experiences_extract_candidates(days: int = None) -> str:
    """触发 AI 经验候选提取（maintainer/admin）：最近 days 天（默认 7，限 1-30）
    resolved/closed 的 issue 提炼候选草稿。"""
    return _call(commands.run_experiences_extract, days=days)


@mcp.tool()
def labctl_experiences_list(status: str = "", project_id: str = "", page: int = 1,
                            per_page: int = 20) -> str:
    """经验列表（审核候选用 status=candidate）。status: candidate/published/archived。"""
    return _call(commands.run_experiences_list, status=status, project_id=project_id,
                 page=page, per_page=per_page)


@mcp.tool()
def labctl_experiences_publish(experience_id: str) -> str:
    """审核通过并发布候选经验（项目 maintainer+；全局仅 admin）。"""
    return _call(commands.run_experiences_publish, experience_id=experience_id)


@mcp.tool()
def labctl_experiences_create(title: str, content: str, project_id: str = None,
                              tags: list = None, linked_projects: list = None) -> str:
    """创建经验候选（项目 member+；无 project_id=全局经验仅 admin）。
    linked_projects 元素：{project_id, relation?}。"""
    return _call(commands.run_experiences_create, title=title, content=content,
                 project_id=project_id, tags=tags, linked_projects=linked_projects)


@mcp.tool()
def labctl_experiences_update(experience_id: str, title: str = None, content: str = None,
                              tags: list = None, linked_projects: list = None) -> str:
    """编辑经验（仅 candidate 态；作者/admin/项目 maintainer+）。"""
    return _call(commands.run_experiences_update, experience_id=experience_id,
                 title=title, content=content, tags=tags, linked_projects=linked_projects)


@mcp.tool()
def labctl_experiences_archive(experience_id: str) -> str:
    """归档经验（须 published；项目 owner，全局仅 admin）。"""
    return _call(commands.run_experiences_archive, experience_id=experience_id)


# ---------------------------------------------------------------- 周报
@mcp.tool()
def labctl_weekly_generate(week_start: str = "", notify: bool = True) -> str:
    """生成/复用周报（maintainer/admin；week_start 须周一 YYYY-MM-DD，默认本周一；
    同一周重复调用返回已存周报）。"""
    return _call(commands.run_weekly_generate, week_start=week_start, notify=notify)


@mcp.tool()
def labctl_weekly_recent(limit: int = 5) -> str:
    """查看最近周报（tags=weekly_summary 的已发布经验）。"""
    return _call(commands.run_weekly_recent, limit=limit)


# ---------------------------------------------------------------- P2: sensors/ask
@mcp.tool()
def labctl_sensors_latest(tags: str = "") -> str:
    """传感器最新读数（tags 逗号分隔 measurement 名；固定查最近 1h last()）。"""
    return _call(commands.run_sensors_latest, tags=tags)


@mcp.tool()
def labctl_sensors_history(tag: str, from_: str = "-1h", to_: str = "",
                           interval: str = "") -> str:
    """传感器历史序列。from_/to_/interval 是 InfluxDB Flux 字面量（如 -1h/now()/5m）。"""
    return _call(commands.run_sensors_history, tag=tag, from_=from_, to=to_,
                 interval=interval)


@mcp.tool()
def labctl_ask_chat(question: str) -> str:
    """AI 问答（限流 10 次/min；502 upstream_error=AI 降级中）。"""
    return _call(commands.run_ask_chat, question=question)


@mcp.tool()
def labctl_ask_history(page: int = 1, per_page: int = 20) -> str:
    """本人问答历史。"""
    return _call(commands.run_ask_history, page=page, per_page=per_page)


# ---------------------------------------------------------------- P2: 只读杂项
@mcp.tool()
def labctl_experiences_get(experience_id: str) -> str:
    """经验详情（viewer+）。"""
    return _call(commands.run_experiences_get, experience_id=experience_id)


@mcp.tool()
def labctl_daily_report_by_date(date: str = "", latest: bool = False) -> str:
    """按日期查本人日报（date 缺省今天；latest=true 只取当天最新一份）。"""
    return _call(commands.run_daily_report_by_date, date=date, latest=latest)


@mcp.tool()
def labctl_test_data_update(entry_id: str, measurement: str = None, value: float = None,
                            unit: str = None, quality: str = None,
                            measured_at: str = None, notes: str = None) -> str:
    """编辑测试数据（白名单字段；data_type/run_id/source 不可变）。"""
    return _call(commands.run_test_data_update, entry_id=entry_id, measurement=measurement,
                 value=value, unit=unit, quality=quality, measured_at=measured_at,
                 notes=notes)


# ---------------------------------------------------------------- P2: 装配/模板/RF
@mcp.tool()
def labctl_assembly_list(project_id: str, status: str = None, page: int = 1,
                         per_page: int = 20) -> str:
    """项目装配步骤列表。status: planned/in_progress/paused/completed/skipped/cancelled。"""
    return _call(commands.run_assembly_list, project_id=project_id, status=status,
                 page=page, per_page=per_page)


@mcp.tool()
def labctl_assembly_transition(step_id: str, action: str,
                               override_reason: str = None) -> str:
    """流转装配步骤。action: start/pause/resume/complete/skip/cancel；
    依赖步骤 cancelled 时 start 须给 override_reason。"""
    return _call(commands.run_assembly_transition, step_id=step_id, action=action,
                 override_reason=override_reason)


@mcp.tool()
def labctl_step_templates_list(kind: str = "", q: str = "", page: int = 1,
                               per_page: int = 20) -> str:
    """步骤模板列表。kind: assembly/experiment。"""
    return _call(commands.run_step_templates_list, kind=kind, q=q, page=page,
                 per_page=per_page)


@mcp.tool()
def labctl_step_templates_generate(kind: str, prompt: str,
                                   context: dict = None) -> str:
    """AI 生成步骤模板（member+；限流 10 次/min）。kind: assembly/experiment。"""
    return _call(commands.run_step_templates_generate, kind=kind, prompt=prompt,
                 context=context)


_RF_MEASUREMENT_FIELDS = ("s11", "input_freq", "input_voltage", "input_power", "input_desc",
                          "output_freq", "output_voltage", "output_power", "output_desc",
                          "transformer_turns", "capacitance_text", "transformer_material",
                          "shunt_inductance", "series_capacitor")


@mcp.tool()
def labctl_rf_matching_list(project_id: str, device: str = None, status: str = None,
                            page: int = 1, per_page: int = 20) -> str:
    """项目 RF 匹配记录列表。device: rf_carpet/rfq/qpig；status: pass/adjust/fail。"""
    return _call(commands.run_rf_matching_list, project_id=project_id, device=device,
                 status=status, page=page, per_page=per_page)


@mcp.tool()
def labctl_rf_matching_create(project_id: str, device: str, frequency_mhz: float,
                              status: str, measurements: dict = None,
                              notes: str = None, measured_at: str = None) -> str:
    """录入 RF 匹配记录（member+）。measurements 可选键：s11/input_freq/input_voltage/
    input_power/input_desc/output_freq/output_voltage/output_power/output_desc/
    transformer_turns/capacitance_text/transformer_material/shunt_inductance/series_capacitor。"""
    extra = {k: v for k, v in (measurements or {}).items() if k in _RF_MEASUREMENT_FIELDS}
    return _call(commands.run_rf_matching_create, project_id=project_id, device=device,
                 frequency_mhz=frequency_mhz, status=status, notes=notes,
                 measured_at=measured_at, **extra)


# ---------------------------------------------------------------- P2: admin 管理
@mcp.tool()
def labctl_admin_users_list() -> str:
    """用户列表（admin）。"""
    return _call(commands.run_admin_users_list)


@mcp.tool()
def labctl_admin_users_create(username: str, display_name: str = None,
                              role: str = None, password: str = None) -> str:
    """创建用户（admin；不给 password 服务端生成 temporary_password）。
    role: admin/maintainer/member/viewer。"""
    return _call(commands.run_admin_users_create, username=username,
                 display_name=display_name, role=role, password=password)


@mcp.tool()
def labctl_admin_users_set(user_id: str, display_name: str = None, role: str = None,
                           disabled: bool = None) -> str:
    """更新用户（admin）：display_name/role/disabled。"""
    return _call(commands.run_admin_users_set, user_id=user_id, display_name=display_name,
                 role=role, disabled=disabled)


@mcp.tool()
def labctl_admin_users_reset_password(user_id: str, new_password: str = None) -> str:
    """重置用户密码（admin；不给 new_password 服务端生成 temporary_password）。"""
    return _call(commands.run_admin_users_reset_password, user_id=user_id,
                 new_password=new_password)


@mcp.tool()
def labctl_admin_invites_list() -> str:
    """邀请码列表（admin）。"""
    return _call(commands.run_admin_invites_list)


@mcp.tool()
def labctl_admin_invites_create(expires_at: str = None) -> str:
    """创建邀请码（admin）。"""
    return _call(commands.run_admin_invites_create, expires_at=expires_at)


@mcp.tool()
def labctl_admin_invites_revoke(invite_id: str) -> str:
    """吊销邀请码（admin）。"""
    return _call(commands.run_admin_invites_revoke, invite_id=invite_id)


@mcp.tool()
def labctl_automation_rules_list() -> str:
    """自动化规则列表（admin）。"""
    return _call(commands.run_automation_rules_list)


@mcp.tool()
def labctl_automation_rules_create(name: str,
                                   trigger_event: str = "daily_report.submitted") -> str:
    """创建自动化规则（admin；动作固定 enqueue_agent_task）。"""
    return _call(commands.run_automation_rules_create, name=name,
                 trigger_event=trigger_event)


@mcp.tool()
def labctl_automation_rules_enable(rule_id: str, enabled: bool = True) -> str:
    """启停自动化规则（admin）。"""
    return _call(commands.run_automation_rules_enable, rule_id=rule_id, enabled=enabled)


@mcp.tool()
def labctl_automation_rules_rm(rule_id: str) -> str:
    """删除自动化规则（admin）。"""
    return _call(commands.run_automation_rules_rm, rule_id=rule_id)


# ---------------------------------------------------------------- P2: agent candidates
@mcp.tool()
def labctl_agent_candidates_list(status: str = "", page: int = 1,
                                 per_page: int = 20) -> str:
    """Agent 候选动作列表（admin/maintainer）。status:
    pending_review/approved/rejected/executed/execution_failed。"""
    return _call(commands.run_agent_candidates_list, status=status, page=page,
                 per_page=per_page)


@mcp.tool()
def labctl_agent_candidates_trace(candidate_id: str) -> str:
    """候选动作完整 AI 时间线（admin/maintainer）。"""
    return _call(commands.run_agent_candidates_trace, candidate_id=candidate_id)


@mcp.tool()
def labctl_agent_candidates_approve(candidate_id: str) -> str:
    """批准候选动作（admin/maintainer；服务端执行落库）。"""
    return _call(commands.run_agent_candidates_approve, candidate_id=candidate_id)


@mcp.tool()
def labctl_agent_candidates_reject(candidate_id: str, reason: str = "") -> str:
    """拒绝候选动作（admin/maintainer）。"""
    return _call(commands.run_agent_candidates_reject, candidate_id=candidate_id,
                 reason=reason)


# ---------------------------------------------------------------- P2: instruments 只读
@mcp.tool()
def labctl_instruments_list() -> str:
    """仪器列表（含在线状态）。"""
    return _call(commands.run_instruments_list)


@mcp.tool()
def labctl_instruments_status(instrument_id: str) -> str:
    """单台仪器状态。"""
    return _call(commands.run_instruments_status, instrument_id=instrument_id)


@mcp.tool()
def labctl_instruments_whitelist() -> str:
    """仪器命令白名单（参数范围以 docs/仪器白名单.yaml 为准）。"""
    return _call(commands.run_instruments_whitelist)


@mcp.tool()
def labctl_instruments_parse_result(instrument_id: str, command: str,
                                    response: str) -> str:
    """只读解析仪器原始响应（不发命令，viewer+）。"""
    return _call(commands.run_instruments_parse_result, instrument_id=instrument_id,
                 command=command, response=response)


# ---------------------------------------------------------------- P2: run steps / report-links
@mcp.tool()
def labctl_runs_steps_list(run_id: str, page: int = 1, per_page: int = 50) -> str:
    """批次实验步骤列表。"""
    return _call(commands.run_runs_steps_list, run_id=run_id, page=page, per_page=per_page)


@mcp.tool()
def labctl_runs_steps_add(run_id: str, name: str, description: str = None,
                          depends_on: str = None, step_order: int = 0) -> str:
    """添加实验步骤（step_order=0 追加到末尾）。"""
    return _call(commands.run_runs_steps_add, run_id=run_id, name=name,
                 description=description, depends_on=depends_on, step_order=step_order)


@mcp.tool()
def labctl_run_steps_status(step_id: str, action: str) -> str:
    """流转实验步骤。action: start/pause/resume/complete/skip/cancel。"""
    return _call(commands.run_run_steps_status, step_id=step_id, action=action)


@mcp.tool()
def labctl_runs_steps_reorder(run_id: str, steps: list) -> str:
    """重排实验步骤（maintainer+）。steps 元素：{id, step_order}。"""
    return _call(commands.run_runs_steps_reorder, run_id=run_id, steps=steps)


@mcp.tool()
def labctl_runs_report_link(run_id: str, report_id: str) -> str:
    """关联日报到批次（maintainer+ 或创建者）。"""
    return _call(commands.run_runs_report_link, run_id=run_id, report_id=report_id)


@mcp.tool()
def labctl_runs_report_unlink(run_id: str, report_id: str) -> str:
    """取消批次与日报的关联。"""
    return _call(commands.run_runs_report_unlink, run_id=run_id, report_id=report_id)


# ---------------------------------------------------------------- 系统更新
@mcp.tool()
def labctl_update_status() -> str:
    """查询系统版本与远端差异（需 admin；只读，返回 current/latest/behind/can_update）。"""
    return _call(commands.run_update_status)


@mcp.tool()
def labctl_update_trigger() -> str:
    """触发系统更新（需 admin 交互登录；服务账号 token 写操作会被 CSRF 校验拒绝）。

    返回 session_id 与当前版本；日志流跟踪不适合 MCP 轮询模型，由
    Web 端 SSE 或 labctl update run 订阅。
    """
    return _call(commands.run_update_trigger)


def main():
    mcp.run(transport="stdio")


if __name__ == "__main__":
    main()
