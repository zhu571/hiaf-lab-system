<template>
  <el-skeleton v-if="loading" :rows="4" animated class="state-block-skeleton" />
  <div v-else-if="error" class="state-block-error">
    <el-alert :title="errorText || errorMessage" type="error" show-icon :closable="false" />
    <el-button class="state-block-retry" size="small" type="primary" plain @click="emit('retry')">{{ t('common.retry') }}</el-button>
  </div>
  <el-empty v-else-if="empty" :description="emptyText || t('common.empty')" />
  <slot v-else />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// 三态容器（重构方案 §3.7/§3.8 契约）：loading > error > empty > 内容 四态优先级。
// 收敛对象：SensorsView.vue:35-37 错误态范式（el-alert + 重试按钮）；页面级首屏加载用骨架。
// 注意：操作级错误（按钮提交）不走本组件，仍用 showApiError toast（§3.8）。
// emptyText 为各页定制空态引导文案（美术 §4.9 空态规范），缺省落 common.empty。
const props = defineProps<{
  loading?: boolean
  error?: { message?: string } | null
  empty?: boolean
  errorText?: string
  emptyText?: string
}>()

const emit = defineEmits<{ retry: [] }>()

const { t } = useI18n()

const errorMessage = computed(() => props.errorText || (props.error && props.error.message ? props.error.message : ''))
</script>

<style scoped>
.state-block-error {
  display: grid;
  gap: var(--space-3);
  justify-items: start;
}

.state-block-retry {
  min-height: var(--touch-target);
}
</style>
