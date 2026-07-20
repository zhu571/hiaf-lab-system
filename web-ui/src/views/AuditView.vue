<template>
  <div class="page">
    <div class="toolbar">
      <h2>审计查询</h2>
      <div class="query-group">
        <el-input v-model="requestId" placeholder="request_id" clearable class="request-input" @keyup.enter="load" />
        <el-button type="primary" @click="load">查询</el-button>
      </div>
    </div>
    <section class="panel">
      <el-descriptions v-if="records[0]" border :column="2">
        <el-descriptions-item label="Request ID">{{ records[0].request_id }}</el-descriptions-item>
        <el-descriptions-item label="记录数">{{ records.length }}</el-descriptions-item>
        <el-descriptions-item label="用户">{{ records[0].username || '-' }}</el-descriptions-item>
        <el-descriptions-item label="客户端">{{ records[0].client_ip || '-' }}</el-descriptions-item>
      </el-descriptions>
      <el-empty v-else description="输入 request_id 查询" />
    </section>
    <section class="panel">
      <el-table :data="records">
        <el-table-column prop="created_at" label="时间" width="190" />
        <el-table-column prop="method" label="方法" width="90" />
        <el-table-column prop="path" label="路径" />
        <el-table-column prop="status_code" label="状态" width="90" />
        <el-table-column prop="action" label="动作" />
        <template #empty>
          <el-empty description="暂无审计记录" />
        </template>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { getAudit, type AuditRecord } from '../api/audit'

const requestId = ref('')
const records = ref<AuditRecord[]>([])

async function load() {
  if (!requestId.value) return
  try {
    const data = await getAudit(requestId.value)
    records.value = data.items ?? []
  } catch (err) {
    records.value = []
    ElMessage.error(err instanceof Error ? err.message : '审计记录加载失败')
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
