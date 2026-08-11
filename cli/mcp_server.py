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
    return _call(commands.run_logout)


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
def labctl_daily_report_entry(report_id: str, raw_text: str = None) -> str:
    """查看单份日报；传入 raw_text 则更新日报正文（仅作者本人）。"""
    return _call(commands.run_daily_report_entry,
                 report_id=report_id, raw_text=raw_text)


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
    """创建项目（需 maintainer/admin 角色）。visibility 可选 private/public。"""
    return _call(commands.run_projects_create, code=code, name=name,
                 short_name=short_name, description=description, visibility=visibility,
                 start_date=start_date, target_end_date=target_end_date,
                 default_category=default_category, tags=tags)


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


# ---------------------------------------------------------------- 测试数据
@mcp.tool()
def labctl_test_data_list(project_id: str, run_id: str = None, data_type: str = None,
                          quality: str = None, page: int = 1, per_page: int = 20) -> str:
    """测试数据列表。data_type: cryo/pressure/voltage/rf_voltage/efficiency。"""
    return _call(commands.run_test_data_list, project_id=project_id,
                 run_id=run_id, data_type=data_type, quality=quality, page=page, per_page=per_page)


@mcp.tool()
def labctl_test_data_entry(project_id: str, data_type: str, measurement: str, value: float,
                           unit: str = None, quality: str = "normal", measured_at: str = None,
                           run_id: str = None, notes: str = None) -> str:
    """录入单条测试数据（需项目 member+）。"""
    return _call(commands.run_test_data_entry, project_id=project_id,
                 data_type=data_type, measurement=measurement, value=value, unit=unit,
                 quality=quality, measured_at=measured_at, run_id=run_id, notes=notes)


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
    """流转批次状态。action: start/abort/pause/complete/resume（服务端按当前状态校验）。"""
    return _call(commands.run_runs_status, run_id=run_id, action=action)


# ---------------------------------------------------------------- 告警
@mcp.tool()
def labctl_alerts_list(status: str = "active", limit: int = 50, offset: int = 0) -> str:
    """告警列表。status: active/resolved。"""
    return _call(commands.run_alerts_list, status=status, limit=limit, offset=offset)


@mcp.tool()
def labctl_alerts_resolve(alert_id: str) -> str:
    """解除告警（需 admin/maintainer 角色）。"""
    return _call(commands.run_alerts_resolve, alert_id=alert_id)


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


def main():
    mcp.run(transport="stdio")


if __name__ == "__main__":
    main()
