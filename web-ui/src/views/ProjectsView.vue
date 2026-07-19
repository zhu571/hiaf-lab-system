<template>
  <div class="page">
    <div class="toolbar">
      <h2>项目</h2>
      <el-button type="primary" @click="dialog = true">新建项目</el-button>
    </div>
    <div v-if="isMobile" class="panel">
      <el-tabs v-model="mobileTab">
        <el-tab-pane label="列表" name="list"><ProjectSidebar /></el-tab-pane>
        <el-tab-pane label="仪表盘" name="dashboard"><ProjectDashboard /></el-tab-pane>
      </el-tabs>
    </div>
    <div v-else class="projects-layout">
      <div class="panel"><ProjectSidebar /></div>
      <ProjectDashboard />
    </div>
    <el-dialog v-model="dialog" title="新建项目" width="520">
      <el-form label-position="top">
        <el-form-item label="编号"><el-input v-model="draft.code" /></el-form-item>
        <el-form-item label="名称"><el-input v-model="draft.name" /></el-form-item>
        <el-form-item label="简称"><el-input v-model="draft.short_name" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="draft.description" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" @click="create">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import ProjectSidebar from '../components/ProjectSidebar.vue'
import ProjectDashboard from '../components/ProjectDashboard.vue'
import { useMobile } from '../composables/useMobile'
import { useProjectStore } from '../stores/project'
import { createProject } from '../api/projects'

const isMobile = useMobile()
const store = useProjectStore()
const dialog = ref(false)
const mobileTab = ref('list')
const draft = reactive({ code: '', name: '', short_name: '', description: '' })

onMounted(() => store.load())

async function create() {
  await createProject(draft)
  await store.load()
  dialog.value = false
  ElMessage.success('项目已创建')
}
</script>

<style scoped>
.projects-layout {
  align-items: start;
  display: grid;
  gap: 20px;
  grid-template-columns: 280px minmax(0, 1fr);
}

@media (max-width: 768px) {
  .projects-layout {
    grid-template-columns: 1fr;
  }
}
</style>
