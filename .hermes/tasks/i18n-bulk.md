翻译以下 Vue 页面的中文到英文，使用 i18n 模式（已有的 zh.ts/en.ts + t() 函数）。

每个页面需要：
1. 加 import { useI18n } from 'vue-i18n' 和 const { t } = useI18n()
2. 模板中中文文字替换为 {{ t('xxx.xxx') }} 或 $t('xxx.xxx')
3. script 中中文消息替换为 t('xxx.xxx')
4. zh.ts/en.ts 补缺的 key
5. npm run build 验证

页面列表（按难度排列）：
1. views/TestDataView.vue
2. views/StepTemplatesView.vue  
3. views/IssuesView.vue
4. views/ExperiencesView.vue
5. views/RunListView.vue
6. views/RunDetailView.vue
7. views/AdminUsersView.vue
8. views/AgentCandidatesView.vue
9. views/RFMatchingView.vue
10. views/AttachmentView.vue
11. views/ProjectDashboard.vue
12. views/DailyReportView.vue
13. views/DailyReportDetailView.vue
14. views/DailyHistoryView.vue
15. views/AuditView.vue

每个页面的 key 用页面名前缀（如 testData.xxx, stepTemplates.xxx 等）
不要改功能、样式、逻辑。
翻译完后 git add -A && git commit -m "i18n: translate XxxView" 并推送。
