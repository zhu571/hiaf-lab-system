<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    :width="width"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form label-position="top" @submit.prevent>
      <slot />
    </el-form>
    <template #footer>
      <slot name="footer">
        <el-button @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="loading" @click="emit('submit')">{{ t('common.confirm') }}</el-button>
      </slot>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

// 通用弹窗表单封装（重构方案 §3.7 契约）：
// props { modelValue, title, loading?, width? } + slots（default=表单、footer 默认取消/确定双按钮，emit submit）；
// 内置 el-form label-position="top"（收敛对象：RunDetailView 4 弹窗 :170-266、IssuesView:50-60、InstrumentMeasureView:182-256）。
// 业务侧保留关闭钩子（@closed 等）时在 FormDialog 上透传 el-dialog 事件（v-bind="$attrs" 已由默认透传）。
defineProps<{
  modelValue: boolean
  title?: string
  loading?: boolean
  width?: string | number
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  submit: []
}>()

const { t } = useI18n()
</script>
