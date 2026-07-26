实现步骤模板 MVP 前端。后端已完成（steptemplates 模块 + assembly apply-template）。

## 改动

### 1. 新建 web-ui/src/api/stepTemplates.ts

```typescript
import { request } from './client'

export interface StepTemplate {
  id: string
  name: string
  kind: 'assembly' | 'experiment'
  description?: string
  source_prompt?: string
  ai_generated: boolean
  created_by?: string
  created_at: string
  updated_at: string
  items?: StepTemplateItem[]
  _item_count?: number
}

export interface StepTemplateItem {
  id?: string
  name: string
  description?: string
  step_order: number
  depends_on_order?: number | null
}

export interface GenerateCandidate {
  status: 'ok' | 'clarify' | 'rejected'
  name_suggestion?: string
  steps?: StepTemplateItem[]
  question?: string | null
  reason?: string | null
}

export function generateSteps(kind: string, prompt: string): Promise<GenerateCandidate> {
  return request({ method: 'POST', url: '/step-templates/generate', data: { kind, prompt } })
}

export function listTemplates(params?: { kind?: string; q?: string; page?: number; per_page?: number }): Promise<{ items: StepTemplate[]; total: number }> {
  return request({ url: '/step-templates', params })
}

export function getTemplate(id: string): Promise<StepTemplate> {
  return request({ url: `/step-templates/${id}` })
}

export function createTemplate(data: { name: string; kind: string; description?: string; items: StepTemplateItem[]; source_prompt?: string; ai_generated?: boolean; apply_to_project_id?: string }): Promise<StepTemplate> {
  return request({ method: 'POST', url: '/step-templates', data })
}

export function updateTemplate(id: string, data: { name?: string; description?: string }): Promise<StepTemplate> {
  return request({ method: 'PATCH', url: `/step-templates/${id}`, data })
}

export function replaceTemplateItems(id: string, items: StepTemplateItem[]): Promise<StepTemplate> {
  return request({ method: 'PATCH', url: `/step-templates/${id}/items`, data: { items } })
}

export function deleteTemplate(id: string): Promise<void> {
  return request({ method: 'DELETE', url: `/step-templates/${id}` })
}
```

### 2. 新建 web-ui/src/views/StepTemplatesView.vue
路由 /step-templates。

功能：
- 表格列表：名称、kind 标签、步骤数、AI 标记、创建人、创建时间
- 搜索 + kind 筛选（el-select + el-input）
- 操作：查看详情（对话框展示步骤）、编辑（对话框编辑 name/description/items）、删除
- "应用到项目"按钮：弹窗选择项目（查询自己有 member+ 权限的项目），调 apply-template 端点
- 新建模板按钮：手动输入 name/kind/description + 步骤编辑器，不经过 AI

### 3. 修改 web-ui/src/views/AssemblyView.vue
在步骤列表上方或工具栏加按钮。

- "AI 生成步骤"按钮，打开对话框
- 对话框三态：输入自然语言 → AI 生成候选表格（可编辑 name/description/order/依赖） → 三按钮：直接应用 / 存模板 / 存并应用
- 步骤表格行内可编辑（name, description），可上下移动、删除行、手动加行
- 待执行步骤的步数旁显示编号
- 确认后调 apply-template（template_id 模式或 inline steps 模式）

### 4. 修改 web-ui/src/router/index.ts
添加 StepTemplatesView 路由：{ path: '/step-templates', component: ..., meta: { requiresAuth: true } }

### 文件位置
- 新建 web-ui/src/api/stepTemplates.ts
- 新建 web-ui/src/views/StepTemplatesView.vue
- /home/zhuhaofan/hiaf-lab-system/web-ui/src/views/AssemblyView.vue
- /home/zhuhaofan/hiaf-lab-system/web-ui/src/router/index.ts

### 验证
```bash
cd /home/zhuhaofan/hiaf-lab-system/web-ui && npm run build
```
