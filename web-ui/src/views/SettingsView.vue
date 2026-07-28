<template>
  <div class="page settings">
    <section class="panel settings-card">
      <div class="user-head">
        <span class="avatar">{{ (auth.user?.username || '?').slice(0, 1).toUpperCase() }}</span>
        <div class="user-meta">
          <h2>{{ t('settings.title') }}</h2>
          <p class="muted">{{ auth.user?.username }} · {{ auth.user?.role }}</p>
        </div>
      </div>
      <el-alert v-if="auth.user?.must_change_password" :title="t('settings.mustChangePassword')" type="warning" show-icon :closable="false" />
      <div class="language-row">
        <span class="language-label">{{ t('settings.language') }}</span>
        <el-select :model-value="locale" style="width: 160px" @change="onLanguageChange">
          <el-option label="中文" value="zh" />
          <el-option label="English" value="en" />
        </el-select>
      </div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('settings.oldPassword')"><el-input v-model="form.oldPassword" type="password" show-password /></el-form-item>
        <el-form-item :label="t('settings.newPassword')"><el-input v-model="form.newPassword" type="password" show-password /></el-form-item>
        <el-form-item :label="t('settings.confirmNewPassword')"><el-input v-model="form.confirm" type="password" show-password /></el-form-item>
        <div class="form-actions">
          <el-button type="primary" native-type="submit">{{ t('settings.changePassword') }}</el-button>
          <el-button @click="doLogout">{{ t('nav.logout') }}</el-button>
        </div>
      </el-form>
    </section>

    <section v-if="quickLinks.length" class="panel quick-links">
      <h3 class="section-title">{{ t('settings.quickLinks') }}</h3>
      <el-link v-for="link in quickLinks" :key="link.path" :underline="false" @click="router.push(link.path)">
        <el-icon style="margin-right:6px"><component :is="link.icon" /></el-icon>
        {{ link.label }}
      </el-link>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { User, DataBoard, Tickets } from '@element-plus/icons-vue'
import { changePassword } from '../api/auth'
import { useAuthStore } from '../stores/auth'
import { setLocale, type AppLocale } from '../i18n'

const router = useRouter()
const auth = useAuthStore()
const { t, locale } = useI18n()
const form = reactive({ oldPassword: '', newPassword: '', confirm: '' })

interface QuickLink { label: string; path: string; icon: any }

const quickLinks = computed<QuickLink[]>(() => {
  const links: QuickLink[] = []
  if (auth.canReviewAgent) links.push({ label: t('nav.aiReview'), path: '/agent-candidates', icon: Tickets })
  if (auth.isAdmin) links.push({ label: t('nav.adminUsers'), path: '/admin/users', icon: User })
  links.push({ label: t('nav.audit'), path: '/audit', icon: DataBoard })
  return links
})

async function onLanguageChange(value: string | number | boolean) {
  const lang = value as AppLocale
  const prev = locale.value as AppLocale
  setLocale(lang) // 本地先生效，后端保存失败再回滚
  try {
    await auth.setLanguage(lang)
    ElMessage.success(t('settings.languageSaved'))
  } catch {
    setLocale(prev)
    ElMessage.error(t('settings.languageSaveFailed'))
  }
}

async function submit() {
  if (form.newPassword !== form.confirm) {
    ElMessage.error(t('settings.passwordMismatch'))
    return
  }
  await changePassword(form.oldPassword, form.newPassword)
  await auth.loadMe()
  ElMessage.success(t('settings.passwordChanged'))
}

async function doLogout() {
  await auth.logout()
  await router.push('/login')
}
</script>

<style scoped>
.settings {
  margin: 0 auto;
  max-width: 640px;
  width: 100%;
}

.settings-card {
  display: grid;
  gap: 20px;
}

.user-head {
  align-items: center;
  display: flex;
  gap: 14px;
}

.avatar {
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 50%;
  box-shadow: 0 6px 16px -6px rgba(20, 112, 138, 0.5);
  color: #fff;
  display: grid;
  flex-shrink: 0;
  font-size: 18px;
  font-weight: 700;
  height: 46px;
  place-items: center;
  width: 46px;
}

.user-meta {
  display: grid;
  gap: 2px;
}

.language-row {
  align-items: center;
  display: flex;
  gap: 12px;
}

.language-label {
  color: var(--text-2);
  font-size: 14px;
}

.form-actions {
  display: flex;
  gap: 12px;
}

.quick-links {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 20px;
}

.section-title {
  color: var(--text-secondary, #6b7280);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.5px;
  margin: 0;
  text-transform: uppercase;
  width: 100%;
}

.quick-links .el-link {
  align-items: center;
  background: var(--bg-panel, #fff);
  border: 1px solid var(--border-light, #e5e7eb);
  border-radius: 8px;
  display: flex;
  padding: 10px 16px;
}
</style>
