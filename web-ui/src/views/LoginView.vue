<template>
  <main class="login-page">
    <section class="login-panel">
      <div class="brand-block">
        <span class="brand-mark">H</span>
        <h1>HIAF Lab System</h1>
        <p class="muted">实验室日志管理平台</p>
      </div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="用户名">
          <el-input v-model="form.username" autocomplete="username" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" autocomplete="current-password" show-password />
        </el-form-item>
        <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
        <el-button type="primary" native-type="submit" :loading="loading">登录</el-button>
      </el-form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const loading = ref(false)
const error = ref('')
const form = reactive({ username: '', password: '' })

async function submit() {
  loading.value = true
  error.value = ''
  try {
    const data = await auth.login(form.username, form.password)
    await router.push(data.must_change_password ? '/settings' : '/projects')
  } catch (err) {
    error.value = err instanceof Error ? err.message : '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  align-items: center;
  background:
    radial-gradient(1100px 560px at 85% -10%, rgba(230, 184, 76, 0.16), transparent 60%),
    radial-gradient(900px 520px at -10% 110%, rgba(26, 134, 162, 0.35), transparent 55%),
    linear-gradient(150deg, #0a1c2e 0%, #123652 55%, #155a72 100%);
  display: grid;
  min-height: 100vh;
  overflow: hidden;
  padding: 24px;
  place-items: center;
  position: relative;
}

.login-page::before {
  background-image:
    linear-gradient(rgba(255, 255, 255, 0.05) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255, 255, 255, 0.05) 1px, transparent 1px);
  background-size: 44px 44px;
  content: '';
  inset: 0;
  pointer-events: none;
  position: absolute;
}

.login-panel {
  animation: rise 0.35s ease;
  background: rgba(255, 255, 255, 0.97);
  border: 1px solid rgba(255, 255, 255, 0.6);
  border-radius: 18px;
  box-shadow: 0 32px 80px -24px rgba(4, 18, 30, 0.55);
  display: grid;
  gap: 24px;
  max-width: 400px;
  padding: 36px 32px;
  position: relative;
  width: 100%;
  z-index: 1;
}

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
}

.brand-block {
  display: grid;
  gap: 8px;
  justify-items: center;
  text-align: center;
}

.brand-mark {
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 13px;
  box-shadow: 0 8px 20px -6px rgba(20, 112, 138, 0.55);
  color: #fff;
  display: grid;
  font-size: 22px;
  font-weight: 800;
  height: 46px;
  margin-bottom: 4px;
  place-items: center;
  width: 46px;
}

h1 {
  font-size: 22px;
  margin: 0;
}

.login-panel .el-button--primary {
  font-size: 15px;
  height: 40px;
  margin-top: 4px;
  width: 100%;
}

@media (max-width: 480px) {
  .login-panel {
    padding: 28px 22px;
  }
}
</style>
