import json
import os
import threading
from pathlib import Path

import httpx

from tools.api import APIError
from tools.parse import MODEL, BASE_URL, ParseError, ensure_safe, _json_object


class _ExecuteRejected(Exception):
    """Go 执行端点返回 SQL 被拒类 4xx（400/422 等，401/403 除外）：可重试——错误信息回填 prompt 让 LLM 改 SQL。"""


def read_service_token():
    """读取 service_token（非 agent_password）：SERVICE_TOKEN_FILE 优先，env 兜底。

    与 serve.py read_token 同模式（file 优先）；测试可注入，不强制在构造期存在。
    """
    path = os.getenv("SERVICE_TOKEN_FILE")
    if path:
        try:
            return Path(path).read_text().strip()
        except FileNotFoundError:
            pass
    return os.getenv("SERVICE_TOKEN", "")


class AskEngine:
    """AI 智能查询引擎（方案 §2）：规划（自然语言→只读单表 SELECT）→ Go 只读执行 → 整合回答。

    - LLM 封装复用 tools/parse.py 的 MODEL/BASE_URL/api_key 模式（LightAgent 调用方式，
      NoToolHook + tools=[] + registry 重置），不新建 LLM 客户端。
    - 执行用 httpx 直调 Go 内部端点 POST {GO_API_BASE}/api/v1/ask/execute，
      鉴权 Authorization: Bearer {SERVICE_TOKEN}（service_token，与 agent_password 是两套凭证）。
      401/403（凭据问题）与 5xx/网络错误 → APIError（502 provider_unavailable）；
      仅 SQL 被拒类 4xx（400/422 等）回填 prompt 重试一次。
    - 总超时 60s 由 serve 层 asyncio.wait_for 兜底（规划 25s + 执行 5s + 整合 25s + 余量）。
    """

    TOTAL_TIMEOUT = 60           # 总预算（秒）
    EXECUTE_TIMEOUT = 5          # 执行调用超时（秒）
    ROWS_TEXT_BUDGET = 20 * 1024  # 整合阶段行集文本截断上限（~20KB）
    # 项目类表：规划 prompt 硬约束 SELECT 必须包含 project_id 列（方案 §4）。
    # 注：run_steps 表无 project_id 列（仅 run_id 关联 experiment_runs，迁移 025），
    # 故不在此清单中（run_steps 仍可单表只读查询，只是不强制 project_id 列）。
    PROJECT_TABLES = frozenset({
        "issues", "test_data", "rf_matching_records", "assembly_steps", "logs",
        "experiment_runs", "experiences",
    })

    def __init__(self, api_key, go_api_base=None, service_token=None,
                 plan_prompt_path=None, integrate_prompt_path=None):
        from LightAgent import HookDecision, LightAgent, ToolRegistry

        class NoToolHook:
            def __call__(self, context):
                if context.phase == "before_model_request":
                    params = context.payload["params"]
                    if params.get("tools") or params.get("extra_body", {}).get("thinking", {}).get("type") != "disabled":
                        return HookDecision.block("model request escaped the no-tool non-thinking boundary")
                return HookDecision.continue_()

        prompts = Path(__file__).parents[1] / "prompts"
        self.plan_agent = self._build_agent(
            api_key, "ask-planner",
            plan_prompt_path or prompts / "ask_plan.txt",
            HookDecision, LightAgent, ToolRegistry, NoToolHook,
        )
        self.integrate_agent = self._build_agent(
            api_key, "ask-integrator",
            integrate_prompt_path or prompts / "ask_integrate.txt",
            HookDecision, LightAgent, ToolRegistry, NoToolHook,
        )
        self.go_api_base = (go_api_base or os.environ["GO_API_BASE"]).rstrip("/")
        self.service_token = service_token if service_token is not None else read_service_token()
        # 共享 httpx.Client：serve 层 asyncio.to_thread 会并发调用 ask，
        # _execute 用锁串行化出站请求，避免跨线程复用连接（httpx.Client 非线程安全）。
        self._client_lock = threading.Lock()
        self.client = httpx.Client(timeout=self.EXECUTE_TIMEOUT)

    @staticmethod
    def _build_agent(api_key, name, prompt_path, HookDecision, LightAgent, ToolRegistry, NoToolHook):
        agent = LightAgent(
            name=name, model=MODEL, base_url=BASE_URL, api_key=api_key,
            instructions=Path(prompt_path).read_text(), tools=[], filter_tools=True, tree_of_thought=False,
            memory=None, self_learning=False, auto_discover_skills=False,
            hooks=[NoToolHook()], debug=False,
        )
        agent.tool_registry = ToolRegistry()
        agent.loaded_tools = {}
        return agent

    def ask(self, question, schema, history=None):
        """规划→执行→整合。同步方法（serve 层 asyncio.to_thread + wait_for 总 60s 预算）。

        返回 {answer, sql, rows, columns, table, row_count, truncated}（对齐 Go 侧
        agentAskResponse，见 go-server/ask/model.go）。
        """
        # 用户输入视同不可信数据：与 interpret/step-plan 同源注入防护。
        ensure_safe(question)
        history = list(history or [])
        for item in history:
            ensure_safe(str(item.get("content", "")))
        plan = self._plan(question, schema, history)
        result = self._execute_with_retry(plan, question, schema, history)
        answer = self._integrate(question, result["sql"], result["table_name"],
                                 result["columns"], result["rows"], history)
        return {
            "answer": answer,
            "sql": result["sql"],
            "rows": result["rows"],
            "columns": result["columns"],
            "table": result["table_name"],
            "row_count": result["row_count"],
            "truncated": bool(result.get("truncated", False)),
        }

    def _plan(self, question, schema, history, error=None):
        """规划：系统说明（instructions）+ 注入 schema + 用户问题 → JSON {"sql", "reason"}。

        schema 属 trusted_context（Go 注入）；question/history 属 untrusted_inputs（不可信）。
        JSON 解析失败 / 缺少 sql → ParseError（422 ask_failed）。
        """
        payload = {
            "trusted_context": {
                "schema": schema,
                "project_tables_with_project_id": sorted(self.PROJECT_TABLES),
            },
            "untrusted_inputs": {"question": question, "history": history},
        }
        if error:
            payload["untrusted_inputs"]["previous_error"] = error
        result = self._call_llm(self.plan_agent, "plan", json.dumps(payload, ensure_ascii=False))
        item = _json_object(result)
        sql = str(item.get("sql", "")).strip()
        if not sql:
            raise ParseError("plan output is missing sql")
        return {"sql": sql, "reason": str(item.get("reason", "")).strip()}

    def _execute_with_retry(self, plan, question, schema, history):
        """执行 + SQL 被拒（4xx）重试一次：失败信息回填 prompt 让 LLM 改 SQL，再执行一次，仍失败 → ParseError。

        Go 不可达 / 5xx / 401 / 403 → APIError（502 provider_unavailable，不重试）。
        """
        error = ""
        for attempt in range(2):
            try:
                return self._execute(plan["sql"])
            except _ExecuteRejected as exc:
                error = str(exc)
                if attempt == 1:
                    break  # 已重试一次，不再触发第三次规划
                plan = self._plan(question, schema, history, error=error)
        raise ParseError(f"SQL 执行被拒绝（重试后仍失败）: {error}")

    def _integrate(self, question, sql, table, columns, rows, history):
        """整合：问题 + SQL + 行集（截断 ~20KB）→ 最终回答（markdown，含关键数字和结论）。"""
        rows_text = json.dumps(rows, ensure_ascii=False)
        if len(rows_text) > self.ROWS_TEXT_BUDGET:
            rows_text = rows_text[:self.ROWS_TEXT_BUDGET] + "\n...(rows truncated)"
        payload = {
            "trusted_context": {
                "sql": sql, "table": table, "columns": columns, "rows": rows_text,
            },
            "untrusted_inputs": {"question": question, "history": history},
        }
        result = self._call_llm(self.integrate_agent, "integrate", json.dumps(payload, ensure_ascii=False))
        answer = str(result).strip()
        if not answer:
            raise ParseError("integration produced empty answer")
        return answer

    @staticmethod
    def _call_llm(agent, stage, query):
        """每次 LLM 调用一次重试（max_retry=1）；LLM 错误 → ParseError（422 ask_failed）。"""
        try:
            return str(agent.run(
                query, tools=[], use_skills=False, max_retry=1, result_format="str",
                metadata={"extra_body": {"thinking": {"type": "disabled"}}},
            ))
        except ParseError:
            raise
        except Exception as exc:
            raise ParseError(f"{stage} model request failed: {type(exc).__name__}") from exc

    def _execute(self, sql):
        if not self.service_token:
            raise APIError("service token is not configured")
        try:
            with self._client_lock:
                response = self.client.post(
                    self.go_api_base + "/api/v1/ask/execute",
                    json={"sql": sql},
                    headers={"Authorization": "Bearer " + self.service_token},
                )
        except (httpx.TimeoutException, httpx.TransportError) as exc:
            raise APIError("Go API unavailable") from exc
        if response.status_code >= 500:
            raise APIError(f"Go API unavailable ({response.status_code})")
        if response.status_code in (401, 403):
            # 鉴权失败是环境/凭据问题，不是 SQL 被拒：回填 prompt 重试无意义，直接 502。
            raise APIError(f"Go API rejected credentials ({response.status_code})")
        if 400 <= response.status_code < 500:
            raise _ExecuteRejected(self._rejection_message(response))
        try:
            payload = response.json()
        except ValueError as exc:
            raise APIError(f"Go API returned invalid JSON ({response.status_code})") from exc
        data = payload.get("data")
        if not isinstance(data, dict):
            raise APIError("Go API returned unexpected payload")
        return data

    @staticmethod
    def _rejection_message(response):
        """提取 Go 4xx 错误信息（表名非法/语法错/超列数等）用于回填 prompt。"""
        try:
            error = response.json().get("error") or {}
            return f"{error.get('code', 'sql_rejected')}: {error.get('message', 'SQL rejected')}"
        except ValueError:
            return f"sql_rejected: Go API returned {response.status_code}"
