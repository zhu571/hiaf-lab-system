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

// 通用弹窗表单封装（重构方案 §3.7 契约，结构改版 R4 全站单轨化）：
// props { modelValue, title, loading?, width? } + slots（default=表单、footer 默认取消/确定双按钮，emit submit）；
// 内置 el-form label-position="top"。全站表单弹窗统一入口：RunDetailView 4 处 + R4 迁入 13 视图 19 处；
// 确认/展示型弹窗（方案 §5.1 排除清单）仍用裸 el-dialog。
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
