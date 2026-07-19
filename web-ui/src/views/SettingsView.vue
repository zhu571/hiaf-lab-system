<template>
  <div class="page settings">
    <section class="panel settings-card">
      <div class="user-head">
        <span class="avatar">{{ (auth.user?.username || '?').slice(0, 1).toUpperCase() }}</span>
        <div class="user-meta">
          <h2>个人设置</h2>
          <p class="muted">{{ auth.user?.username }} · {{ auth.user?.role }}</p>
        </div>
      </div>
      <el-alert v-if="auth.user?.must_change_password" title="首次登录需要修改密码" type="warning" show-icon :closable="false" />
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="旧密码"><el-input v-model="form.oldPassword" type="password" show-password /></el-form-item>
        <el-form-item label="新密码"><el-input v-model="form.newPassword" type="password" show-password /></el-form-item>
        <el-form-item label="确认新密码"><el-input v-model="form.confirm" type="password" show-password /></el-form-item>
        <div class="form-actions">
          <el-button type="primary" native-type="submit">修改密码</el-button>
          <el-button @click="doLogout">退出登录</el-button>
        </div>
      </el-form>
    </section>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { changePassword } from '../api/auth'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const form = reactive({ oldPassword: '', newPassword: '', confirm: '' })

async function submit() {
  if (form.newPassword !== form.confirm) {
    ElMessage.error('两次密码不一致')
    return
  }
  await changePassword(form.oldPassword, form.newPassword)
  await auth.loadMe()
  ElMessage.success('密码已修改')
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

.form-actions {
  display: flex;
  gap: 12px;
}
</style>
