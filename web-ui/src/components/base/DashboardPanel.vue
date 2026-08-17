<template>
  <section class="panel dashboard-panel" :class="{ 'dashboard-panel--divided': divided }">
    <div class="panel-head">
      <span v-if="icon" class="panel-icon"><el-icon><component :is="icon" /></el-icon></span>
      <h3 class="panel-title">{{ title }}</h3>
      <span v-if="meta" class="panel-meta">{{ meta }}</span>
      <slot name="actions" />
    </div>
    <template v-if="gated">
      <StateBlock
        :loading="loading"
        :error="error"
        :empty="empty"
        :error-text="errorText"
        :empty-text="emptyText"
        @retry="emit('retry')"
      >
        <slot />
      </StateBlock>
    </template>
    <slot v-else />
  </section>
</template>

<script setup lang="ts">
// 仪表盘块（结构改版 R5 §6.3 契约）：「panel-head（icon+title+meta+actions 槽）+ StateBlock」
// 组合封装，样式来自 utilities.css 既有 .panel-head/.panel-icon/.panel-meta 体系。
// divided 承接 DashboardView 的 scoped 覆写（.panel-head 下边线 + .panel-meta 右推）。
// 三态门控（loading/error/empty）为可选：传入任一状态 prop 即由组件内建 StateBlock 包裹
// default 槽；三者都不传时 default 槽直渲染，接入方在槽内自管 StateBlock
//（面板内存在 StateBlock 外常驻内容时必须保持此模式，避免加载/错误态吞掉常驻交互）。
// props/slots 驱动、无业务语义（base 层定义，AGENTS.md §5）。
import { computed, type Component } from 'vue'
import StateBlock from '@/components/base/StateBlock.vue'

const props = defineProps<{
  title: string
  icon?: Component
  meta?: string
  /** DashboardView 覆写承接：.panel-head 下边线 + .panel-meta 右推 */
  divided?: boolean
  loading?: boolean
  error?: { message?: string } | null
  empty?: boolean
  errorText?: string
  emptyText?: string
}>()

const emit = defineEmits<{ retry: [] }>()

const gated = computed(() => props.loading !== undefined || props.error !== undefined || props.empty !== undefined)
</script>

<style scoped>
/* DashboardView scoped 覆写（.panel-head 下边线 :491-494、.panel-meta :496-498）经 divided 承接，视觉等值 */
.dashboard-panel--divided .panel-head {
  border-bottom: 1px solid var(--border);
  padding-bottom: 12px;
}

.dashboard-panel--divided .panel-meta {
  margin-left: auto;
}
</style>
