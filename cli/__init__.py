"""labctl — hiaf-lab-system 命令行客户端与 MCP Server。

设计参照 py-agent/tools/api.py 的 GoAPI REST 客户端模式；
MCP 工具复用 cli/commands.py 的命令执行函数，不重复实现。
"""

__version__ = "1.0.0"
