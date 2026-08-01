检查批次页面为什么看不到 AI 生成步骤按钮，并修复。

## 背景
用户反馈：装配界面（AssemblyView）能看到「AI 生成步骤」，但批次页面看不到。

已确认：
1. RunDetailView.vue 顶栏已有 AI 按钮（line 17-19），但被 `<template v-else-if="run">` 包裹——**必须有一条真实的实验批次数据**（getRun 成功）才会显示
2. RunListView.vue（批次列表页）**没有** AI 生成入口
3. 线上数据库 experiment_runs 表是 0 条（用户还没创建过批次）

## 用户需求
"为什么不能像装配一样，也是一个顶栏工具？ai自动创建？"

用户希望在批次页面（列表页或详情页）像装配界面一样，顶栏常驻 AI 生成按钮，不依赖已有数据。

## 请做

### 1. 检查 RunListView.vue（批次列表页）
- 顶栏（toolbar）是否有 AI 相关按钮？目前 grep 只有 toolbar 没有 aiGenerate
- 如果没有：在列表页顶栏加「AI 生成步骤」按钮，点击后可以直接用 AI 生成步骤，生成后引导创建批次或应用到新批次

### 2. 检查 RunDetailView.vue 的 v-else-if="run" 限制
- 如果 run 不存在（批次不存在或未加载），整个页面包括顶栏按钮都不可见
- 是否可以把「AI 生成步骤」按钮放到 v-else-if="run" 块外面（比如页面级 toolbar），这样即使批次数据未加载也能看到入口？

### 3. 检查 AssemblyView 的做法作参照
- AssemblyView 的 AI 按钮在哪个层级？为什么它不依赖数据？

## 输出
- 检查结论（为什么批次页看不到）
- 修复方案：让批次页面顶栏常驻 AI 生成按钮（无论有没有批次数据）
- 实施修改，npm run build 验证
