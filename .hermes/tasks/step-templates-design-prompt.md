设计 AI 辅助生成装配/实验步骤模板的方案。

## 背景

HIAF 实验室系统中已有：
- `AssemblyStep`（装配步骤）：项目级别，每步有 name/description/status/step_order
- `ExperimentRun`（实验流程）：项目级别，有 name/type/gas/devices 等字段，但没有拆分成步骤
- 两者目前都是手动创建的

用户想要：
1. 输入自然语言 → AI 拆解成结构化步骤列表
2. 生成的步骤可以保存为模板
3. 模板可以在新项目中复用（一键导入）

## 现有代码位置

- 装配模块：/home/zhuhaofan/hiaf-lab-system/go-server/assembly/
- 实验模块：/home/zhuhaofan/hiaf-lab-system/go-server/runs/
- AI 能力：已有 py-agent 的 interpret 机制（/home/zhuhaofan/hiaf-lab-system/py-agent/）
- 仪器 AI 会话模式可参考：/home/zhuhaofan/hiaf-lab-system/go-server/instruments/session.go

## 设计任务

请产出完整设计方案，包括：

1. **数据模型**：需要什么新表/字段
2. **API 设计**：生成、模板 CRUD、应用模板的端点
3. **AI 生成逻辑**：怎么调 LLM，prompt 设计
4. **前端交互**：用户在什么界面输入自然语言、确认、保存、引用
5. **与现有系统的集成**：怎么和 assembly/runs 模块对接
6. **阶段划分**：MVP 做什么，后续扩展做什么

不需要真的改代码，只需要方案。写到 .hermes/plans/step-templates-plan.md。
