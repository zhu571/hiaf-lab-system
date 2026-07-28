<template>
  <div class="fallback-page">
    <el-empty :description="t('issuesFallback.selectProject')" />
    <el-button type="primary" @click="$router.push('/projects')">{{ t('issuesFallback.goToProjects') }}</el-button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<style scoped>
.fallback-page {
  align-items: center;
  display: flex;
  flex-direction: column;
  gap: 16px;
  justify-content: center;
  min-height: 400px;
}
</style>
