<template>
  <main class="login-page">
    <section class="login-panel">
      <div class="brand-block">
        <span class="brand-mark">H</span>
        <h1>HIAF Lab System</h1>
        <p class="muted">{{ t('login.subtitle') }}</p>
      </div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('login.username')">
          <el-input v-model="form.username" autocomplete="username" />
        </el-form-item>
        <el-form-item :label="t('login.password')">
          <el-input v-model="form.password" type="password" autocomplete="current-password" show-password />
        </el-form-item>
        <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
        <el-button type="primary" native-type="submit" :loading="loading">{{ t('login.submit') }}</el-button>
        <span class="register-tip">{{ t('login.noAccount') }}<el-button link type="primary" @click="registerDialogVisible = true">{{ t('login.register') }}</el-button></span>
      </el-form>
    </section>
    <el-dialog v-model="registerDialogVisible" :title="t('login.registerTitle')" width="360px" :close-on-click-modal="false">
      <el-form label-position="top" @submit.prevent="submitRegister">
        <el-form-item :label="t('login.username')">
          <el-input v-model="registerForm.username" autocomplete="username" :placeholder="t('login.usernamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('login.password')">
          <el-input v-model="registerForm.password" type="password" autocomplete="new-password" show-password :placeholder="t('login.passwordPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('login.confirmPassword')">
          <el-input v-model="registerForm.confirm" type="password" autocomplete="new-password" show-password />
        </el-form-item>
        <el-form-item :label="t('login.invitationCode')"><el-input v-model="registerForm.invitationCode" autocomplete="off" autocapitalize="off" spellcheck="false" :placeholder="t('login.invitationCodePlaceholder')" /><small class="muted">{{ t('login.invitationCodeHelp') }}</small></el-form-item>
        <el-alert v-if="registerError" :title="registerError" type="error" show-icon :closable="false" />
      </el-form>
      <template #footer>
        <el-button @click="registerDialogVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" native-type="submit" :loading="registering" :disabled="registering" @click="submitRegister">{{ t('login.register') }}</el-button>
      </template>
    </el-dialog>
  </main>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { register } from '../api/auth'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const form = reactive({ username: '', password: '' })

const registerDialogVisible = ref(false)
const registering = ref(false)
const registerError = ref('')
const registerForm = reactive({ username: '', password: '', confirm: '', invitationCode: '' })

async function submit() {
  loading.value = true
  error.value = ''
  try {
    await auth.login(form.username, form.password)
    await router.push('/')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('login.loginFailed')
  } finally {
    loading.value = false
  }
}

async function submitRegister() {
  const username = registerForm.username.trim()
  if (username.length < 2 || username.length > 32) {
    registerError.value = t('login.usernameLength')
    return
  }
  if (new TextEncoder().encode(registerForm.password).length < 10 || !/\p{L}/u.test(registerForm.password) || !/\p{N}/u.test(registerForm.password)) {
    registerError.value = t('login.passwordLength')
    return
  }
  if (registerForm.password !== registerForm.confirm) {
    registerError.value = t('login.passwordMismatch')
    return
  }
  if (!registerForm.invitationCode.trim()) { registerError.value = t('login.invitationCodeRequired'); return }
  registering.value = true
  registerError.value = ''
  try {
    await register(username, registerForm.password, registerForm.invitationCode.trim())
    // 注册成功后直接登录；跳转由调用方负责（authStore.login 不做跳转）
    await auth.login(username, registerForm.password)
    registerDialogVisible.value = false
    await router.push('/')
  } catch (err) {
    const code = err && typeof err === 'object' && 'details' in err ? String((err as {details?:{code?:string}}).details?.code || '') : ''
    registerError.value = code === 'registration_disabled' ? t('login.registrationDisabled') : code === 'invitation_code_required' ? t('login.invitationCodeRequired') : code === 'invalid_invitation_code' ? t('login.invitationCodeInvalid') : (err instanceof Error ? err.message : t('login.registerFailed'))
  } finally {
    registering.value = false
  }
}
</script>

<style scoped>
.login-page {
  align-items: center;
  background: var(--login-bg);
  display: grid;
  min-height: 100dvh;
  overflow: auto;
  padding: var(--space-6);
  place-items: center;
  position: relative;
}

.login-page::before {
  background-image: var(--login-grid);
  background-size: 44px 44px;
  content: '';
  inset: 0;
  pointer-events: none;
  position: absolute;
}

.login-panel {
  animation: rise var(--dur-slow) var(--ease-out);
  background: var(--login-panel-bg);
  border: 1px solid var(--login-panel-border);
  border-radius: var(--radius-xl);
  box-shadow: var(--login-panel-shadow);
  display: grid;
  gap: var(--space-6);
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
  box-shadow: var(--login-mark-shadow);
  color: var(--text-inverse);
  display: grid;
  font-size: 22px;
  font-weight: var(--fw-bold);
  height: 46px;
  margin-bottom: 4px;
  place-items: center;
  width: 46px;
}

h1 {
  font-size: var(--fs-display);
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
