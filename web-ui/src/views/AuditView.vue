<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('audit.title') }}</h2>
      <div class="query-group">
        <el-input v-model="requestId" placeholder="request_id" clearable class="request-input" @keyup.enter="load" />
        <el-button type="primary" @click="load">{{ t('audit.query') }}</el-button>
      </div>
    </div>
    <section class="panel">
      <el-descriptions v-if="records[0]" border :column="isMobile ? 1 : 2">
        <el-descriptions-item label="Request ID">{{ records[0].request_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('audit.recordCount')">{{ records.length }}</el-descriptions-item>
        <el-descriptions-item :label="t('audit.user')">{{ records[0].username || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('audit.client')">{{ records[0].client_ip || '-' }}</el-descriptions-item>
      </el-descriptions>
      <el-empty v-else :description="t('audit.inputRequestId')" />
    </section>
    <section class="panel">
      <el-table :data="records">
        <el-table-column prop="created_at" :label="t('audit.time')" width="190" />
        <el-table-column prop="method" :label="t('audit.method')" width="90" />
        <el-table-column prop="path" :label="t('audit.path')" />
        <el-table-column prop="status_code" :label="t('audit.status')" width="90" />
        <el-table-column prop="action" :label="t('audit.action')" />
        <template #empty>
          <el-empty :description="t('audit.noRecords')" />
        </template>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import { useMobile } from '../composables/useMobile'
import { getAudit, type AuditRecord } from '../api/audit'

const { t } = useI18n()
const isMobile = useMobile()

const requestId = ref('')
const records = ref<AuditRecord[]>([])

async function load() {
  if (!requestId.value) return
  try {
    const data = await getAudit(requestId.value)
    records.value = data.items ?? []
  } catch (err) {
    records.value = []
    showApiError(err, t('audit.loadFailed'))
  }
}
</script>

<style scoped>
.query-group {
  align-items: center;
  display: flex;
  flex: 1;
  gap: 10px;
  justify-content: flex-end;
  min-width: 0;
}

.request-input {
  max-width: 420px;
}

@media (max-width: 768px) {
  .query-group {
    flex-basis: 100%;
    justify-content: stretch;
  }

  .request-input {
    max-width: none;
  }
}
</style>
