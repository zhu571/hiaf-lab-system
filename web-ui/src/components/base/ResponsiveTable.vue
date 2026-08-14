<template>
  <div v-if="isMobile && hasCardSlot" v-loading="loading" class="rt-card-list">
    <div v-for="(row, i) in rows" :key="rowKey(row, i)" class="rt-card">
      <slot name="card" :row="row" />
    </div>
    <template v-if="!loading && rows.length === 0">
      <slot name="empty">
        <el-empty />
      </slot>
    </template>
  </div>
  <el-table v-else v-loading="loading" :data="rows" v-bind="$attrs">
    <slot />
    <template #empty>
      <slot name="empty" />
    </template>
  </el-table>
</template>

<script setup lang="ts" generic="T">
import { computed, useSlots } from 'vue'
import { useMobile } from '@/composables/useMobile'

defineOptions({ inheritAttrs: false })

defineProps<{
  rows: T[]
  loading?: boolean
}>()

defineSlots<{
  default(): unknown
  card(props: { row: T }): unknown
  empty(): unknown
}>()

const slots = useSlots()
const isMobile = useMobile()
const hasCardSlot = computed(() => Boolean(slots.card))

function rowKey(row: T, i: number) {
  if (row && typeof row === 'object' && 'id' in row) {
    const id = (row as { id: unknown }).id
    return typeof id === 'string' || typeof id === 'number' ? id : i
  }
  return i
}
</script>

<style scoped>
.rt-card-list {
  display: grid;
  gap: 12px;
}

.rt-card {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  display: grid;
  gap: 8px;
  padding: 12px 14px;
}

.rt-card :deep(.card-title) {
  color: var(--text-1);
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
  overflow-wrap: anywhere;
}

.rt-card :deep(.card-fields) {
  color: var(--text-3);
  display: flex;
  flex-wrap: wrap;
  font-size: 13px;
  gap: 2px 12px;
  line-height: 1.5;
}

.rt-card :deep(.card-actions) {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 2px;
}
</style>
