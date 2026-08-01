# 排查：RunDetailView 看不到 tab 和「AI 生成步骤」按钮

> 用户 admin，缓存已清空；AssemblyView 能正常显示「AI 生成步骤」「模板库」，但 `/experiment-runs/:id` 详情页看不到 概览/步骤/关联日报/测试数据 4 个 tab，也看不到 AI 按钮。

**结论先行：前端代码、构建产物、线上部署、后端路由全部正确且为最新；浏览器无运行时 JS 错误。真正原因是——线上数据库里当前没有任何实验批次（experiment_runs 表为空）。`getRun()` 拿到 404/500 后 `run` 为 null，而 tab 和 AI 按钮都包在 `v-else-if="run"` 里，所以整个详情（含 tab、AI 按钮）都不渲染。** 详细证据如下。

---

## 1. 源码层面 —— 组件完整，tab + AI 按钮都存在

`web-ui/src/views/RunDetailView.vue`：

- 4 个 tab 结构正确（`RunDetailView.vue:25-150`）：`overview` / `steps` / `reports` / `testdata`。
- 「AI 生成步骤」按钮存在（`RunDetailView.vue:73-77`），位于 steps tab 的 `.steps-actions` 工具栏，仅受 `v-if="canEdit"` 控制。
- `canEdit = computed(() => !!auth.user && auth.user.role !== 'viewer')`（`RunDetailView.vue:391`）—— **admin 为 true**，非权限问题。
- 依赖 API（`RunDetailView.vue:259-275`）：`getRun`、`listRunSteps`、`applyRunTemplate`、`generateSteps`、`createTemplate`、`listTemplates` 全部导入且存在于 `web-ui/src/api/`。

### 关键 gating 条件（根因所在）

```html
<!-- RunDetailView.vue:7-8 -->
<el-empty v-else-if="!run && !loading" :description="t('runDetail.runNotFound')" />
<template v-else-if="run">
   …tab + AI 按钮全在这里面…
</template>
```

**tab 区域整体被 `<template v-else-if="run">` 包裹**，`run` 由 `load()` 里的 `getRun(runId)` 填充：

```ts
// RunDetailView.vue:437-450
run.value = await getRun(runId)
```

只要 `getRun()` 失败（404/500）或返回空，`run` 就是 null → 4 个 tab、AI 按钮全部不渲染，只会显示错误框或「批次不存在」。

---

## 2. 与 AssemblyView 的关键对比（解释「装配界面正常」）

`web-ui/src/views/AssemblyView.vue:3-11`：

```html
<div class="toolbar">
  <h2>{{ t('assembly.title') }}</h2>
  <el-select …/>            <!-- 状态筛选 -->
  <el-button v-if="canOperate" … @click="aiDialog = true">{{ t('assembly.aiGenerate') }}</el-button>
  <RouterLink to="/step-templates"><el-button>{{ t('assembly.templateLibrary') }}</el-button></RouterLink>
  <el-button v-if="canOperate" … @click="createDialog = true">{{ t('assembly.create') }}</el-button>
</div>
```

**AssemblyView 的 AI 按钮/模板库按钮在顶层 `.toolbar` 里，不受任何数据加载结果控制**（只受角色 `canOperate` 控制）。所以即使装配步骤列表加载失败/为空，按钮照样显示。

而 **RunDetailView 的 tab + AI 按钮整块被 `v-else-if="run"` 锁死**，数据没加载出来就什么都不显示。这是两者「一个能看到、一个看不到」的直接原因。

---

## 3. 构建产物 —— 与源码**逐字节一致**，无缺失

在本机重新构建前端：

```bash
cd web-ui && npm run build
# 产物：dist/assets/RunDetailView-BTVjzuW7.js  ← 与线上 hash 完全一致
```

```bash
diff -q web-ui/dist/assets/RunDetailView-BTVjzuW7.js go-server/static/assets/RunDetailView-BTVjzuW7.js
# 输出：IDENTICAL（25117 字节）
diff -q web-ui/dist/index.html go-server/static/index.html
# 输出：IDENTICAL
```

- `RunDetailView-BTVjzuW7.js` 内含 `stepsTab`、`aiGenerateSteps`、4 个 tab name（overview/steps/reports/testdata），ES module 语法可解析。
- `go-server/static/assets/` 中该 chunk 依赖的全部 9 个兄弟 chunk（`vendor-element-*`、`runs-BwnMdZmB.js`、`logs-jt8Atia6.js`、`testdata-Jo8aiXaT.js`、`useNotify-*`、`StatusBadge-*`、`stepTemplates-*`、`index-BpjK9tvg.js` 等）均已存在。

---

## 4. 线上运行 —— 所有 chunk 200，SPA 路由正确

对 `http://10.144.144.12:8000` 实测（curl）：

- `GET /` → 新 index.html（780B），引用 `index-BpjK9tvg.js` + `RunDetailView` 懒加载链。
- 上述 11 个 asset 全部 `200`，无 404/缺失。
- 后端路由 `GET /api/v1/experiment-runs/{id}` 存在（`main.go:254-268` → `runs/handler.go:60 GetByID`），带不带尾斜杠都能命中（均返回 401 未授权而非 404，证明路由存在）。
- admin 的 ACL 校验正确（`runs/service.go:726-728` `CanAccessProject` 对 `RoleAdmin` 直接放行）。

---

## 5. 浏览器实测（Playwright + 真实登录）——决定性证据

用 Chromium 自动化，以 admin（haofan/Test1234!）登录线上环境复现：

| 场景 | 结果 |
|------|------|
| 创建一个真实批次 → 打开 `/experiment-runs/{id}` | ✅ **4 个 tab（概览/步骤/关联日报/测试数据）+ 「AI 生成步骤」按钮全部正常显示** |
| 从列表点击批次进入详情 | ✅ 同上，4 tab + AI 按钮 |
| 打开**不存在的**批次 id（合法 UUID 格式） | ❌ 无 tab，显示「实验批次不存在」错误框 |
| 打开非法格式的 id（如 `nonexistent-abc`） | ❌ 无 tab，显示「服务器内部错误」错误框 |

浏览器控制台**没有任何 JS 报错**（无 TypeError、无组件未注册、无 chunk 加载失败），仅有无害的 401（某轮询接口）和本次构造的 404/500。

---

## 6. 根本原因：线上数据库当前**没有实验批次**

- 迁移 `016_experiment_runs.up.sql` 只 `CREATE TABLE`，**不含任何 seed 数据**。
- `migrations/018_test_data.up.sql` 只给 `test_data` 加了 `run_id` 外键，也不插批次。
- 线上实测：3 个项目的 `GET /api/v1/projects/{id}/experiment-runs` 全部返回 `items:[]`、`total:0`。
- 用 admin 真实登录后，批次列表页显示「暂无实验批次」，无任何行可点。

因此，当用户访问 `/experiment-runs/:id`（无论是旧书签、从日报/测试数据/issue 里的 `run_id` 跳转，还是手输 URL），`getRun()` 都会 404（合法 UUID 但不存在）或 500（非 UUID 格式）→ `run` 为 null → `<template v-else-if="run">` 不渲染 → 看不到 tab 和 AI 按钮。这与用户报告的现象**完全吻合**。

---

## 结论 / 建议动作

1. **前端不需要任何修复，也不需要重新部署**——源码、构建、部署、后端全部正确且已实测可用（存在真实批次时 4 tab + AI 按钮正常渲染）。
2. 用户在「实验批次」列表页点「新建批次」创建一条批次后，详情页的 tab 和「AI 生成步骤」按钮即会正常出现。当前看不到，是因为数据库里 0 条批次、访问的是不存在的 id。
3. 如果用户手头有旧链接/旧书签指向已删除的批次，属预期 404 行为（显示「实验批次不存在」），非 bug。
4. 可选卫生项：
   - 后端 `runs/repository.go` 的 `GetByID` 对非 UUID 格式 id 直接报 500（`nonexistent-abc` 时），建议把非法 UUID 归一化为「批次不存在」404，避免 500 落审计日志（`runs/handler.go:280-305` 的 `writeError` 可加 `ErrInvalidID` 分支）。
   - 部署脚本同步前端前先清空 `go-server/static/assets/`，避免旧 hash 孤儿文件堆积。

### 复现命令（如需复核）

```bash
# 1. 构建并比对
cd web-ui && npm run build
diff web-ui/dist/assets/RunDetailView-BTVjzuW7.js go-server/static/assets/RunDetailView-BTVjzuW7.js

# 2. 浏览器实测（admin：haofan / Test1234!）
#   - 新建一个批次 → 打开 /experiment-runs/{id} → 4 tab + AI 按钮正常
#   - 打开 /experiment-runs/00000000-0000-4000-8000-000000000000 → 无 tab，显示「实验批次不存在」

# 3. 线上数据核对（登录后）
curl -b <cookie> http://10.144.144.12:8000/api/v1/projects/b0000000-0000-4000-8000-000000000001/experiment-runs
# → {"data":{"items":[],"total":0,...}}
```
