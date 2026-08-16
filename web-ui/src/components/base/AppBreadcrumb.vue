<template>
  <nav v-if="items.length" class="app-breadcrumb">
    <ol class="crumb-list">
      <li v-for="(item, index) in items" :key="index" class="crumb-item">
        <RouterLink v-if="item.to && index < items.length - 1" :to="item.to" class="crumb-link">{{ item.label }}</RouterLink>
        <span v-else class="crumb-text" :aria-current="index === items.length - 1 ? 'page' : undefined">{{ item.label }}</span>
        <el-icon v-if="index < items.length - 1" class="crumb-sep"><ArrowRight /></el-icon>
      </li>
    </ol>
  </nav>
</template>

<script setup lang="ts">
import { ArrowRight } from '@element-plus/icons-vue'

// 面包屑（结构改版 R1 §2.3）：base 层纯 props 件，无业务语义。
// 规则：单段页渲染纯标题文本（不可点）；≥2 段渲染分隔符且父段可点；末段恒为当前页纯文本。
// 段 label 由父级（AppLayout）完成 i18n 与项目名解析，本组件不做任何路由/store 读取。
defineProps<{
  items: { label: string; to?: string }[]
}>()
</script>

<style scoped>
.app-breadcrumb {
  min-width: 0;
}

.crumb-list {
  align-items: center;
  display: flex;
  gap: 6px;
  list-style: none;
  margin: 0;
  min-width: 0;
  padding: 0;
}

.crumb-item {
  align-items: center;
  display: flex;
  gap: 6px;
  min-width: 0;
}

.crumb-link,
.crumb-text {
  font-size: var(--fs-body);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.crumb-link {
  color: var(--text-2);
  max-width: 220px;
  transition: color 0.15s ease;
}

.crumb-link:hover {
  color: var(--brand-600);
}

.crumb-text {
  color: var(--text-1);
  font-weight: var(--fw-semibold);
  max-width: 320px;
}

.crumb-sep {
  color: var(--text-3);
  flex: 0 0 auto;
  font-size: 12px;
}
</style>
