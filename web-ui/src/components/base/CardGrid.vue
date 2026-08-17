<template>
  <div class="card-grid" :style="gridVars">
    <slot />
  </div>
</template>

<script setup lang="ts">
// 卡片网格（结构改版 R5 §6.1 契约）：五处自制网格（Attachment/InstrumentMeasure .card-grid、
// RunList .run-grid、Sensors .reading-grid、GasControl .status-grid）归一为
// repeat(auto-fill, minmax(min, 1fr)) + gap，参数经 CSS 变量注入，视觉等值替换。
// props/slots 驱动、无业务语义（base 层定义，AGENTS.md §5）。
// min/mode/gap 逐视图对齐现状值；固定列（RunList）走 columns + mobileColumns 覆盖；
// Sensors 的 ≤480px 窄屏档走 smMin。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** minmax 最小列宽（逐视图对齐现状值，如 170px/200px/320px） */
    min?: string
    /** 网格间距，默认 --space-4 */
    gap?: string
    /** auto-fill（默认）/ auto-fit（GasControl 现状） */
    mode?: 'auto-fill' | 'auto-fit'
    /** 完整 grid-template-columns 覆盖（RunList 固定 3 列 repeat(3, minmax(0, 1fr))） */
    columns?: string
    /** ≤768px 列模板覆盖（RunList 1fr）；缺省沿用模板（auto-fill 网格自然塌缩） */
    mobileColumns?: string
    /** ≤480px 最小列宽覆盖（Sensors 140px） */
    smMin?: string
  }>(),
  { min: '240px', gap: 'var(--space-4)', mode: 'auto-fill' }
)

const template = computed(() => props.columns ?? `repeat(${props.mode}, minmax(${props.min}, 1fr))`)
const smTemplate = computed(() => (props.smMin ? `repeat(${props.mode}, minmax(${props.smMin}, 1fr))` : undefined))

const gridVars = computed(() => ({
  '--card-grid-template': template.value,
  '--card-grid-gap': props.gap,
  ...(props.mobileColumns ? { '--card-grid-template-mobile': props.mobileColumns } : {}),
  ...(smTemplate.value ? { '--card-grid-template-sm': smTemplate.value } : {})
}))
</script>

<style scoped>
.card-grid {
  display: grid;
  gap: var(--card-grid-gap, var(--space-4));
  grid-template-columns: var(--card-grid-template);
}

@media (max-width: 768px) {
  .card-grid {
    grid-template-columns: var(--card-grid-template-mobile, var(--card-grid-template));
  }
}

@media (max-width: 480px) {
  .card-grid {
    grid-template-columns: var(
      --card-grid-template-sm,
      var(--card-grid-template-mobile, var(--card-grid-template))
    );
  }
}
</style>
