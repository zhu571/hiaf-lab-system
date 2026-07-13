# AI 智能问答助手 — 设计方案 (Hermes)

## 定位

Web 前端内的对话助手，能根据数据库内容回答实验室相关问题。不是通用 chatbot——只能查实验数据，不闲聊。

## 交互形态

```
Web UI 右下角悬浮球 → 点开 → 对话面板

┌─────────────────────────┐
│ 💬 实验室助手            │
│                         │
│ ┌─────────────────────┐ │
│ │ 上个月真空异常出现过  │ │
│ │ 几次？当时怎么解决的？ │ │
│ └─────────────────────┘ │
│                         │
│ 💡 7月共3次真空异常:     │
│   • 7/3 密封圈老化→更换  │
│   • 7/8 阀门故障→手动复位│
│   • 7/11 传感器漂移→校准 │
│   [查看详情]             │
│                         │
│ ┌─────────────────────┐ │
│ │ 气体靶装配到哪一步了？ │ │  ← 输入框
│ └─────────────────────┘ │
└─────────────────────────┘
```

**入口:** PWA 底部导航栏 + 桌面端右下角悬浮球。

## 能回答什么

| 问题类型 | 示例 | 查什么表 |
|----------|------|----------|
| 项目进展 | "气体靶装到哪了" | plans, assembly |
| 历史问题 | "这个问题以前出过吗" | issues, experiences |
| 测试数据 | "7月RF匹配成功率" | test_data, rf_matching |
| 设备状态 | "33210A上次校准什么时候" | equipment_log |
| 传感器趋势 | "这两天T1温度有异常吗" | sensor_readings |
| 经验检索 | "真空问题怎么排查" | experiences |
| 人员活动 | "浩钒这周做了哪些实验" | daily_report_items |

## 不能做什么

- ❌ 不控制仪器（那是仪器对话模块的事）
- ❌ 不修改任何数据（只读）
- ❌ 不回答实验室以外的问题
- ❌ 不执行用户直接写的 SQL

## 技术方案

### 架构

```
用户问题
  │
  ▼
LightAgent (问答模式)
  │
  ├── 1. 解析意图: "查历史问题" / "项目进展" / "测试数据"
  │
  ├── 2. 映射到预定义查询函数（只读 SQL 模板）
  │     ├ search_issues(keyword, date_range)
  │     ├ get_project_progress(project_id)
  │     ├ get_test_stats(device, date_range)
  │     ├ search_experiences(keyword)
  │     └ get_sensor_trend(tag, date_range)
  │
  ├── 3. Go API 执行查询（只读权限，禁止 DELETE/UPDATE/INSERT）
  │
  └── 4. 查询结果 + 原始问题 → LLM 生成自然语言回答
```

### 为什么不用 Text-to-SQL

LLM 直接生成 SQL 有幻觉风险——可能写出 `DROP TABLE`、慢查询打垮数据库、或者拼错字段名返回错误结果。**预定义只读查询函数**是安全边界：LLM 只能选函数、填参数，不能写任意 SQL。

### 核心工具函数（定义给 LightAgent）

```python
@tool
def search_issues(keyword: str, date_from: str = None, date_to: str = None, project: str = None) -> list:
    """搜索历史问题。keyword 匹配标题和描述，返回问题列表+解决状态+方案。"""

@tool
def get_project_progress(project_name: str) -> dict:
    """查询项目进度：装配步骤完成率、最近活动、计划vs实际。"""

@tool
def get_test_stats(device: str, metric: str, date_from: str, date_to: str) -> dict:
    """查询设备测试数据统计：成功率、均值、趋势。"""

@tool
def search_experiences(keyword: str) -> list:
    """搜索经验库：匹配关键词，返回问题+方案+相关设备。"""

@tool  
def get_sensor_trend(tag: str, hours: int = 24) -> dict:
    """查询传感器最近N小时趋势：当前值、均值、异常点。"""

@tool
def get_daily_reports(date_from: str, date_to: str, project: str = None, author: str = None) -> list:
    """查询日报摘要，可按项目/作者筛选。"""
```

**Go 端的实现：** 每个函数对应一个只读 SQL 模板，写在 Go 代码里，不拼接用户输入。

```go
func SearchIssues(keyword string, dateFrom, dateTo string) ([]Issue, error) {
    rows, err := db.Query(`
        SELECT id, issue_desc, status, resolution, report_date
        FROM issues
        WHERE issue_desc ILIKE '%' || $1 || '%'
          AND report_date BETWEEN $2 AND $3
        ORDER BY report_date DESC
        LIMIT 20
    `, keyword, dateFrom, dateTo)
    // ...
}
```

### 问题意图路由

LLM 不需要选择函数名，只需输出意图类型 + 参数。Go 后端按意图查对应的表：

```
用户: "上个月真空异常出现过几次？"

Agent → 意图: search_issues
        参数: keyword="真空异常", date_from="2026-06-01", date_to="2026-06-30"

Go API → SELECT ... WHERE issue_desc ILIKE '%真空异常%' AND report_date BETWEEN ...

结果 → [{issue #8: 密封圈老化, 已解决}, {issue #12: 阀门故障, 已解决}]

LLM 生成回答 → "上个月共2次真空异常：7/3 密封圈老化（已更换），7/8 阀门故障（已手动复位）..."
```

## 权限

- 问答助手**只看用户有权看的数据**——用户只能看 MNT 项目，助手回答也只涉及 MNT
- 所有查询走 Go API 权限中间件
- 查询记录写审计日志（谁、什么时候、问了什么）

## 前端实现

Vue 组件：`ChatAssistant.vue`

- 悬浮按钮（桌面端右下角）/ 底部导航项（移动端）
- 对话气泡界面
- 支持引用跳转（点击「查看详情」跳到对应页面）
- 无状态——不存聊天历史，每次打开都是新对话
- 加载态：查询中显示「正在查找...」

## 与「快速录入」的区别

| | 快速录入 | AI 问答助手 |
|---|---------|------------|
| 方向 | 用户 → 系统（写入） | 系统 → 用户（读取） |
| Agent | LightAgent 解析文本 → 入库 | LightAgent 解析问题 → 查库 → 回答 |
| 权限 | 按用户角色写入 | 只读用户有权限的数据 |
| 界位 | Web 录入页 | 悬浮球/独立对话页 |

## 实施时机

Phase 3 后期——等日志/问题/经验库都跑起来有数据了再上线。Phase 1-2 先铺数据管道，没数据问了也没意义。
