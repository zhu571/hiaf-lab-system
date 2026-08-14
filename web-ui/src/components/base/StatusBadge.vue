<template>
  <el-tag :type="tone" size="small" round effect="light">{{ label }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { findStatusMeta, statusMetaFor, type StatusDomain } from '@/utils/statusMeta'

// 泛化（重构方案 §3.7，M4）：domain+value 查 utils/statusMeta.ts 注册表。
// domain 可选——9 个既有视图仅传 :value（跨域值优先查找），行为兼容（美术 §3.8 行为兼容核对）；
// 新调用点传显式 domain，避免同名值跨域歧义。
const props = defineProps<{ domain?: StatusDomain; value: string }>()

const { t } = useI18n()

const meta = computed(() => (props.domain ? statusMetaFor(props.domain, props.value) : findStatusMeta(props.value)))

const tone = computed(() => meta.value?.tone ?? 'info')

const label = computed(() => {
  if (meta.value) return t(meta.value.labelKey)
  // 未命中（M4 定稿）：label 显示原文（现 value.replace(/_/g,' ') 行为不变）+ console.warn 提醒登记
  console.warn(`[statusMeta] 未登记状态值 domain=${props.domain ?? '-'} value=${props.value}，label 降级显示原文、tone 落 info`)
  return props.value.replace(/_/g, ' ')
})
</script>
