<template>
  <div class="page">
    <div class="toolbar">
      <h2>日报详情</h2>
      <RouterLink to="/daily-report/history"><el-button>返回历史</el-button></RouterLink>
    </div>
    <section v-loading="loading" class="panel">
      <template v-if="report">
        <el-descriptions border :column="2" size="small">
          <el-descriptions-item label="日期">{{ report.report_date }}</el-descriptions-item>
          <el-descriptions-item label="作者">{{ report.author_name || report.author_id }}</el-descriptions-item>
          <el-descriptions-item label="状态"><StatusBadge :value="report.content_status" /></el-descriptions-item>
          <el-descriptions-item label="摘要">{{ report.summary || '-' }}</el-descriptions-item>
        </el-descriptions>
        <h3>原文</h3>
        <pre class="raw-text">{{ report.raw_text || '（无）' }}</pre>
        <h3>项目化日志</h3>
        <el-table :data="report.logs || []">
          <el-table-column prop="category" label="分类" width="140" />
          <el-table-column prop="content" label="内容" />
          <el-table-column label="状态" width="120">
            <template #default="{ row }">
              <StatusBadge :value="row.content_status" />
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="暂无日志" />
          </template>
        </el-table>
      </template>
      <el-empty v-else-if="!loading" description="日报不存在或无权查看" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import { getReport, type DailyReport } from '../api/logs'

const route = useRoute()
const report = ref<DailyReport | null>(null)
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    report.value = await getReport(route.params.id as string)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '日报加载失败')
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.panel {
  align-content: start;
  display: grid;
  gap: 14px;
}

.panel h3 {
  color: var(--text-1);
  font-size: 15px;
}

.raw-text {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text-2);
  font-size: 13px;
  overflow: auto;
  padding: 10px;
  white-space: pre-wrap;
}
</style>
