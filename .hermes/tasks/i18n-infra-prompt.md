设计中英文切换方案并实现基础设施和首轮翻译。

## 决策
1. 翻译前端 UI 文字，不改后端错误消息和数据库内容
2. 用 vue-i18n，zh.json + en.json
3. 语言偏好存在用户设置（后端 api），localStorage 兜底

## 阶段：先搭基础设施 + 翻译高频页面

### 第一阶段：基础设施

#### 1. 后端用户语言偏好
- 在 users 表或 user_settings 表加 `language VARCHAR(8) DEFAULT 'zh'`
- 或直接在 auth/user 模块加 profile 更新接口
- 新增 PATCH /api/v1/auth/profile { language: 'zh'|'en' }
- 登录时在 user claims 里返回 language 字段

#### 2. 前端 i18n 框架
- 安装 vue-i18n
- src/i18n/index.ts：创建 i18n 实例，从 localStorage 读取语言，fallback zh
- src/i18n/zh.ts：中文翻译映射
- src/i18n/en.ts：英文翻译映射
- main.ts：导入 i18n 并 use

- src/stores/auth.ts：在 user 信息中加 language 字段
- src/api/auth.ts：加 updateProfile 函数
- src/views/SettingsView.vue：加语言切换下拉框（zh/en），保存调 PATCH /auth/profile + 更新 localStorage + 刷新 i18n locale

### 第二阶段：首轮翻译覆盖的页面
先翻译这些高频页面：
1. LoginView.vue - 登录/注册
2. AppLayout.vue - 侧边栏导航
3. DashboardView.vue - 仪表盘
4. ProjectLayout.vue - 项目导航

其他页面后续逐步补。

### 翻译方式
zh.json 是完整的，en.json 先机翻。
创建 en.json 时在 keys 末尾加 `_comment` 说明上下文。

## 实现

新建的文件：
- src/i18n/index.ts
- src/i18n/zh.ts
- src/i18n/en.ts

修改的文件：
- package.json（加 vue-i18n 依赖）
- main.ts（注册 i18n）
- src/stores/auth.ts（language 字段）
- src/api/auth.ts（updateProfile 函数）
- src/views/SettingsView.vue（语言切换）
- src/views/LoginView.vue（$t 替换）
- src/components/AppLayout.vue（$t 替换）
- src/views/DashboardView.vue（$t 替换）
- src/components/ProjectLayout.vue（$t 替换）
- go-server/auth/*（language 字段 + profile 更新接口）
- migrations/（language 字段迁移）

方案和具体实现一起做。验证：npm run build
