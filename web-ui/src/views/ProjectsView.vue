<template>
  <div class="page">
    <div class="toolbar">
      <h2>项目</h2>
      <el-button v-if="canCreate" type="primary" @click="dialog = true">新建项目</el-button>
    </div>
    <div class="panel">
      <ProjectSidebar />
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
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import ProjectSidebar from '../components/ProjectSidebar.vue'
import { useProjectStore } from '../stores/project'
import { useAuthStore } from '../stores/auth'
import { createProject } from '../api/projects'

const store = useProjectStore()
const auth = useAuthStore()
const canCreate = computed(() => auth.user?.role !== 'viewer')
const dialog = ref(false)
const draft = reactive({ code: '', name: '', short_name: '', description: '' })

onMounted(() => store.load())

async function create() {
  try {
    await createProject(draft)
    await store.load()
    dialog.value = false
    ElMessage.success('项目已创建')
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '项目创建失败')
  }
}
</script>
