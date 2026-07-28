实现实验步骤前端。后端已完成（迁移 025 + runs 模块 6 个 API + main.go 路由注册）。

## 改动

### 1. 新建 web-ui/src/api/runs.ts
```typescript
import { request, requestWithMeta } from './client'

export interface RunStep {
  id: string
  run_id: string
  name: string
  description?: string
  depends_on?: string
  status: string
  step_order: number
  started_at?: string
  completed_at?: string
  source_template_id?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export type StepTransition = 'start' | 'pause' | 'resume' | 'complete' | 'skip' | 'cancel'

export function listRunSteps(runId: string) {
  return request<RunStep[]>({ url: `/experiment-runs/${runId}/steps` })
}

export function createRunStep(runId: string, data: { name: string; description?: string; step_order?: number; depends_on?: string }) {
  return requestWithMeta<RunStep>({ url: `/experiment-runs/${runId}/steps`, method: 'POST', data })
}

export function applyRunTemplate(runId: string, payload: { template_id?: string; steps?: { name: string; description?: string; step_order: number; depends_on_order?: number }[]; source_prompt?: string }) {
  return requestWithMeta<RunStep[]>({ url: `/experiment-runs/${runId}/steps/apply-template`, method: 'POST', data: payload })
}

export function updateRunStep(id: string, data: { name?: string; description?: string; transition?: string }) {
  return requestWithMeta<RunStep>({ url: `/run-steps/${id}`, method: 'PATCH', data })
}

export function deleteRunStep(id: string) {
  return requestWithMeta<void>({ url: `/run-steps/${id}`, method: 'DELETE' })
}

export function reorderRunSteps(runId: string, steps: { id: string; step_order: number }[]) {
  return requestWithMeta<void>({ url: `/run-steps/reorder`, method: 'POST', data: { run_id: runId, steps } })
}
```

### 2. RunDetailView.vue
新增「步骤」tab（放在现有 tab 行中），内容：

- 工具行：AI 生成步骤 / 从模板导入 / 手动新建按钮（viewer 隐藏）
- 步骤表格：序号 / 名称 / 描述 / 状态标签 / 依赖步骤 / 操作（状态转移按钮 + 删除）
- AI 生成对话框：照抄 AssemblyView 的三态模式（输入→候选→三按钮：直接应用/存模板/存并应用）
  - 调用 generateSteps('experiment', prompt, {run_type, gas_type, devices})
  - 候选编辑用 <StepItemsEditor v-model="aiItems" />
  - 直接应用调 applyRunTemplate(runId, {steps, source_prompt})
  - 存并应用=先 createTemplate 再 applyRunTemplate
- 从模板导入：listTemplates({kind:'experiment'}) → 选择 → applyRunTemplate
- 步骤变更后刷新步骤列表，不动主 load()

### 3. StepTemplatesView.vue
在「应用到项目」逻辑中，按模板 kind 分流：
- kind='assembly' → 现有 applyAssemblyTemplate
- kind='experiment' → 先选项目 → 再选该项目的实验批次（listRuns）→ applyRunTemplate

### 4. api/stepTemplates.ts
给 generateSteps 加可选的第三个参数 context? 如果现有接口已支持就不改。

验证：npm run build
