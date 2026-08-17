<template>
  <div class="kanban-board" :style="{ '--kanban-columns': String(columns.length) }">
    <section v-for="col in columns" :key="col.key" class="panel column" :data-status="col.key">
      <div class="column-head">
        <h3><span class="dot" :style="col.tone ? { background: `var(${col.tone})` } : undefined" />{{ col.label }}</h3>
        <span class="count">{{ col.count ?? col.items.length }}</span>
      </div>
      <template v-for="(item, index) in col.items" :key="index">
        <slot name="card" :column="col" :item="item" />
      </template>
      <p v-if="col.items.length === 0 && $slots.empty" class="empty-hint"><slot name="empty" :column="col" /></p>
    </section>
  </div>
</template>

<script setup lang="ts" generic="T">
// 看板（结构改版 R5 §6.2 契约）：IssuesView/ExperiencesView 同构看板
//（.board > section.panel.column > .column-head + 卡片 + .empty-hint）抽取。
// items 随列传入，组件内每列 v-for 渲染；列头 count 由 items.length 派生，
// 需要与分页 total 区分时用显式 count 覆盖。卡片元素由父级 card 槽控制
//（兼容 Issues 的 button.issue-card 与 Experiences 的 article.exp-card 差异）。
// tone 为列头圆点色的 CSS 变量名（如 '--warn'），缺省 --text-3。
// props/slots 驱动、无业务语义（base 层定义，AGENTS.md §5）。
interface KanbanColumn {
  key: string
  label: string
  /** 列头圆点色（CSS 变量名，如 '--warn'）；缺省 --text-3 */
  tone?: string
  items: T[]
  /** 与分页 total 区分时显式覆盖列头计数；缺省 items.length */
  count?: number
}

defineProps<{ columns: KanbanColumn[] }>()

defineSlots<{
  card(props: { column: KanbanColumn; item: T }): unknown
  empty(props: { column: KanbanColumn }): unknown
}>()
</script>

<style scoped>
.kanban-board {
  display: grid;
  gap: var(--space-4);
  grid-template-columns: repeat(var(--kanban-columns), minmax(0, 1fr));
}

.column {
  align-content: start;
  background: var(--surface-2);
  display: grid;
  gap: var(--space-3);
}

.column-head {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.column-head h3 {
  align-items: center;
  display: flex;
  font-size: 14px;
  gap: 8px;
  letter-spacing: 0.01em;
}

.dot {
  background: var(--text-3);
  border-radius: 50%;
  height: 8px;
  width: 8px;
}

.count {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  color: var(--text-3);
  font-size: 12px;
  font-weight: 600;
  min-width: 26px;
  padding: 0 8px;
  text-align: center;
}

.empty-hint {
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius-md);
  color: var(--text-3);
  font-size: 12px;
  padding: 16px 0;
  text-align: center;
}

@media (max-width: 768px) {
  .kanban-board {
    grid-template-columns: 1fr;
  }
}
</style>
