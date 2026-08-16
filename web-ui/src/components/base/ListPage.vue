<template>
  <div class="page list-page">
    <div v-if="title || $slots.actions" class="toolbar">
      <h2 v-if="title">{{ title }}</h2>
      <div v-if="$slots.actions" class="list-page-actions">
        <slot name="actions" />
      </div>
    </div>
    <section v-if="$slots.filters" class="panel list-page-filters">
      <slot name="filters" />
    </section>
    <section class="panel">
      <StateBlock
        :loading="loading"
        :error="error"
        :empty="empty"
        :error-text="errorText"
        :empty-text="emptyText"
        @retry="emit('retry')"
      >
        <slot />
        <div v-if="$slots.pagination" class="list-page-pagination">
          <slot name="pagination" />
        </div>
      </StateBlock>
    </section>
  </div>
</template>

<script setup lang="ts">
// 列表页骨架（结构改版 R3 §4.1 契约）：toolbar（title prop + actions 槽）→ filters 槽（.panel 包裹，可空）
// → 内容 .panel（StateBlock 四态 > default 槽 + pagination 槽）。
// 四态优先级继承 StateBlock：loading 骨架 > error 重试 > empty > 内容；操作级错误仍走 showApiError toast。
// 双 tab 页内嵌用法：省略 title/actions（toolbar 不渲染），仅用 filters/default/pagination 槽。
// base 层无业务语义：标题/筛选/列表/分页全部由 props/slots 驱动。
import StateBlock from '@/components/base/StateBlock.vue'

defineProps<{
  title?: string
  loading?: boolean
  error?: { message?: string } | null
  empty?: boolean
  errorText?: string
  emptyText?: string
}>()

const emit = defineEmits<{ retry: [] }>()
</script>

<style scoped>
.list-page-actions {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

/* 等值原各页 scoped .filters-panel（14px 20px 全端生效，不被 .panel 移动端媒体查询收窄） */
.list-page-filters {
  padding: 14px 20px;
}

/* 等值原各页 scoped .pager（右对齐 + 上间距），随 StateBlock 内容态显隐 */
.list-page-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}
</style>
