# 排查：前端两个功能不可见

> 用户访问 http://10.144.144.12:8000/，看不到
> 1. 实验批次详情页（RunDetailView）的「AI 生成步骤」按钮
> 2. 仪器测量页（InstrumentMeasureView）的曲线显示 + 「保存到测试数据」

**结论先行：代码、构建、部署全部到位且为最新。两个功能被隐藏是「权限角色条件」所致，不是代码缺失或部署陈旧。** 详细证据如下。

---

## 1. 源码层面 —— 功能都在，且被权限条件包裹

### RunDetailView.vue
- 模板：`AI 生成步骤` 按钮**存在**于 steps tab 的 `.steps-actions` 工具栏中（`RunDetailView.vue:73-77`）：

```html
<div v-if="canEdit" class="steps-actions">
  <el-button size="small" type="primary" plain @click="aiDialog = true">{{ t('runDetail.aiGenerateSteps') }}</el-button>
  <el-button size="small" plain @click="openImport">{{ t('runDetail.importFromTemplate') }}</el-button>
  <el-button size="small" type="primary" @click="openCreateStep">{{ t('runDetail.createManually') }}</el-button>
</div>
```

- 关键 gating 条件（`RunDetailView.vue:391`）：

```ts
// viewer 只读，隐藏状态转移/编辑/删除/关联入口（后端仍强校验）
const canEdit = computed(() => !!auth.user && auth.user.role !== 'viewer')
```

- 依赖的 API（`RunDetailView.vue:259-275`）全部导入：`listRunSteps`、`applyRunTemplate`、`generateSteps`、`createTemplate`、`listTemplates`。
- tabs 结构正确：`aiDialog` 里分为 input/result 两阶段，AI 功能完整实现（`RunDetailView.vue:205-236`）。

### InstrumentMeasureView.vue
- 曲线渲染逻辑**存在**：Chart.js 已注册（`InstrumentMeasureView.vue:270`）：

```ts
Chart.register(LineController, ScatterController, LineElement, PointElement, LinearScale, Legend, Tooltip)
```

- 曲线 canvas 在模板中（`InstrumentMeasureView.vue:73-79`），由 `renderChart()`（`553`）驱动，仅当 `parsedResult.type === 'sweep_xy'` 且有 points 时显示。
- 「保存到测试数据」按钮在模板中（`InstrumentMeasureView.vue:84`）：`v-if="!isViewer"`。
- **关键 gating 条件（`InstrumentMeasureView.vue:42` + `276`）** —— 整个命令执行区被 `canOperate` 包住：

```html
<template v-if="canOperate">   <!-- 42 行 -->
  ... cmdResult 区块 + 曲线 canvas + 保存到测试数据按钮 ...
</template>
<p v-else class="muted cmd-desc">{{ t('instrument.noPermission') }}</p>  <!-- 90 行 -->
```

```ts
// 与后端 RequireRole(maintainer, admin) 对应，前端隐藏只是 UX，后端仍强校验
const canOperate = computed(() => ['maintainer', 'admin'].includes(auth.user?.role || ''))
const isViewer = computed(() => auth.user?.role === 'viewer')
```

**含义：只有 `maintainer` / `admin` 角色能看到曲线和「保存到测试数据」；`member` / `viewer` 只会看到「无权限」文案。** 后端同样强校验（`instruments/handler.go:209`、`instruments/service.go:485`）。

---

## 2. 前端路由 —— 正常注册

`web-ui/src/router/index.ts:56` 和 `:59`：

```ts
{ path: '/experiment-runs/:id', component: RunDetailView },
...
{ path: '/instrument-measure', component: InstrumentMeasureView },
```

均使用懒加载 `() => import(...)`，无缺失。

---

## 3. API 层 —— 全部导出，后端 handler 存在

- `web-ui/src/api/runs.ts:103` 导出 `listRunSteps`；`:112` 导出 `applyRunTemplate`。
- `web-ui/src/api/instruments.ts:124` 导出 `parseResult`。
- 后端 handler 存在：
  - `go-server/main.go:267` `r.Post("/steps/apply-template", runsHandler.HandleApplyTemplate)`
  - `go-server/main.go:388` `r.Post("/{id}/parse-result", instrumentsHandler.ParseResult)`
- `parse-result` 无需 Idempotency-Key（只读解析，`main.go:383` 注释），`apply-template` 走 `requestWithMeta`（写接口，符合约定）。

---

## 4. 构建产物 —— go-server/static 是**最新 17:09 构建**，含全部功能

- 最新提交 `074e7fc fix: sync latest frontend build to static`（2026-08-01 17:11）已同步。
- `go-server/static/assets/` 中的新 chunk 均已验证含功能：

```
RunDetailView-BTVjzuW7.js        → 含 aiGenerateSteps (×2)
InstrumentMeasureView-DMh0OwDQ.js → 含 sweep_xy / saveToTestData / chartCanvas
instruments-frlv6GuU.js          → 含 parse-result
runs-BwnMdZmB.js                 → 含 apply-template
```

- `go-server/static/index.html`（17:09）引用最新 chunk：`index-BpjK9tvg.js`、`RunDetailView-BTVjzuW7.js`、`InstrumentMeasureView-DMh0OwDQ.js`。
- 注：static 目录混有 17:00 构建的孤儿文件（`index-DxDaFVM0.js`、`vendor-BFS-rBJR.js` 等），这是部署时未清空旧文件的卫生问题，**不是功能缺失原因**——index.html 只引用新 hash。

---

## 5. 运行时 —— 线上服务已是新镜像，缓存几乎排除

对 http://10.144.144.12:8000/ 实测：

- `GET /` 返回 **新 index.html**（引用 `index-BpjK9tvg.js`）。
- `GET /assets/RunDetailView-BTVjzuW7.js` → 含 `aiGenerateSteps`。
- `GET /assets/InstrumentMeasureView-DMh0OwDQ.js` → 含 `sweep_xy`、`saveToTestData`。
- `GET /assets/instruments-frlv6GuU.js` → 含 `parse-result`。
- 响应无 `Cache-Control` / `ETag` / `Last-Modified` 头（`http.FileServer` 未设置），浏览器会做启发式缓存，但 chunk 文件名带 hash，发布后新 index.html 会拉新 chunk，硬刷新可彻底解决。
- `sw.js` 是自注销的杀开关（`go-server/static/sw.js`），当前构建无 PWA 注册（源码无 VitePWA/registerSW），缓存劫持基本排除。

---

## 6. 根本原因：角色权限条件

两个功能被隐藏，全部可由 **登录用户角色** 解释：

| 功能 | 可见条件 | 不满足时的表现 |
|------|----------|----------------|
| RunDetail「AI 生成步骤」 | `role !== 'viewer'`（`canEdit`） | 按钮不渲染 |
| Instrument 曲线 + 保存到测试数据 | `role ∈ {maintainer, admin}`（`canOperate`） | 整块命令执行区被「无权限」替换 |

若用户以 **`member` 或 `viewer`** 登录：
- Instrument 页：**必然看不到**曲线与保存按钮（只能看到「无权限」提示）——因为这两个 UI 位于 `v-if="canOperate"` 块内。
- RunDetail 页：`viewer` 看不到 AI 按钮；`member` 能看到（`canEdit` 只排除 viewer）。

即「两个功能同时消失」最典型的是 **`viewer` 角色**，或用户实际在以非 maintainer/admin 身份查看仪器页。

### 辅助性 / 次要因素（不改变结论）
- **Role 是全局角色**（`auth.user.role`，来自登录态），不是项目成员角色；`auth/user.role` 在 `stores/auth.ts` 中由 `/api/v1/auth/me` 填充。
- 曲线只在**执行命令返回 sweep_xy 类型且解析出 points** 后渲染；不执行命令不会凭空出现。
- RunDetail 的 AI 按钮位于 **steps tab** 内，需切到「实验步骤」标签页才能看到（默认 `activeTab='overview'`）。

---

## 结论 / 建议动作

1. **代码、构建、线上部署全部确认到位**（curl 已证），不需要重新部署。
2. 让用户确认当前登录账号的角色（设置页右上角或 `GET /api/v1/auth/me` 的 `role`）：
   - 若是 `maintainer` / `admin` → 应可见；不可见则硬刷新（Ctrl+Shift+R）并切到 RunDetail 的「实验步骤」tab、在仪器页先执行一条命令。
   - 若是 `member` / `viewer` → 属预期权限行为，需管理员提升角色或改用 maintainer/admin 账号。
3. 可选卫生项：部署脚本同步前端前先清空 `go-server/static/assets/`，避免遗留旧 hash 孤儿文件。
