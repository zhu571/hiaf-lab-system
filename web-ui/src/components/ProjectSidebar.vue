<template>
  <aside class="project-sidebar">
    <el-input v-model="keyword" placeholder="搜索项目" clearable />
    <el-scrollbar>
      <button
        v-for="project in filtered"
        :key="project.id"
        :class="['project-item', { active: project.id === store.currentId }]"
        @click="store.select(project.id)"
      >
        <strong>{{ project.short_name || project.name }}</strong>
        <span>{{ project.code }}</span>
      </button>
    </el-scrollbar>
  </aside>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useProjectStore } from '../stores/project'

const store = useProjectStore()
const keyword = ref('')
const filtered = computed(() => store.projects.filter((item) => `${item.name} ${item.code}`.toLowerCase().includes(keyword.value.toLowerCase())))
</script>

<style scoped>
.project-sidebar {
  align-content: start;
  display: grid;
  gap: 12px;
}

.project-item {
  background: transparent;
  border: 1px solid transparent;
  border-radius: 10px;
  cursor: pointer;
  display: grid;
  gap: 2px;
  padding: 10px 12px;
  text-align: left;
  transition:
    background 0.15s ease,
    border-color 0.15s ease,
    box-shadow 0.15s ease;
  width: 100%;
}

.project-item:hover {
  background: var(--surface-2);
}

.project-item strong {
  color: var(--text-1);
  font-size: 14px;
}

.project-item span {
  color: var(--text-3);
  font-size: 12px;
  letter-spacing: 0.02em;
}

.project-item.active {
  background: var(--brand-050);
  border-color: var(--brand-100);
  box-shadow: inset 3px 0 0 var(--brand-600);
}

.project-item.active span {
  color: var(--brand-600);
}
</style>
