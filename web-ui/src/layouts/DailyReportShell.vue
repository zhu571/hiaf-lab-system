<template>
  <div class="page">
    <el-tabs :model-value="activeTab" class="shell-tabs" @tab-change="onTabChange">
      <el-tab-pane label="今日录入" name="today" />
      <el-tab-pane label="历史查询" name="history" />
    </el-tabs>
    <RouterView />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

const route = useRoute()
const router = useRouter()

const activeTab = computed(() => (route.path.endsWith('/history') ? 'history' : 'today'))

function onTabChange(name: string | number) {
  const target = name === 'history' ? '/daily-report/history' : '/daily-report'
  if (route.path !== target) router.push(target)
}
</script>

<style scoped>
.shell-tabs :deep(.el-tabs__header) {
  margin-bottom: 0;
}
</style>
