<template>
  <div class="fallback-page">
    <el-empty description="请先在项目页面选择一个项目" />
    <el-button type="primary" @click="$router.push('/projects')">前往项目列表</el-button>
  </div>
</template>

<script setup lang="ts">
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
