<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('projects.title') }}</h2>
      <el-button v-if="canCreate" type="primary" @click="dialog = true">{{ t('projects.create') }}</el-button>
    </div>
    <div class="panel">
      <ProjectSidebar />
    </div>
    <el-dialog v-model="dialog" :title="t('projects.create')" width="520">
      <el-form label-position="top">
        <el-form-item :label="t('projects.code')"><el-input v-model="draft.code" /></el-form-item>
        <el-form-item :label="t('projects.name')"><el-input v-model="draft.name" /></el-form-item>
        <el-form-item :label="t('projects.shortName')"><el-input v-model="draft.short_name" /></el-form-item>
        <el-form-item :label="t('projects.description')"><el-input v-model="draft.description" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="create">{{ t('projects.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import ProjectSidebar from '@/components/business/ProjectSidebar.vue'
import { useProjectStore } from '../stores/project'
import { useAuthStore } from '../stores/auth'
import { createProject } from '../api/projects'

const { t } = useI18n()
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
    ElMessage.success(t('projects.created'))
  } catch (err) {
    showApiError(err, t('projects.createFailed'))
  }
}
</script>
