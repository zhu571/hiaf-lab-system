import { defineStore } from 'pinia'
import * as authApi from '../api/auth'
import { setCSRFToken } from '../api/client'
import type { UserInfo } from '../api/auth'
import { setLocale, type AppLocale } from '../i18n'

// 登录/刷新用户资料后，以后端保存的语言偏好覆盖本地显示语言
function applyUserLanguage(user: UserInfo | null) {
  if (user?.language === 'zh' || user?.language === 'en') setLocale(user.language)
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as UserInfo | null,
    ready: false
  }),
  getters: {
    isAdmin: (state) => state.user?.role === 'admin',
    canReviewAgent: (state) => ['admin', 'maintainer'].includes(state.user?.role || '')
  },
  actions: {
    async login(username: string, password: string) {
      const data = await authApi.login(username, password)
      this.user = data.user
      this.ready = true
      if (data.csrf_token) setCSRFToken(data.csrf_token)
      applyUserLanguage(this.user)
      return data
    },
    async loadMe() {
      try {
        try {
          this.user = await authApi.me()
        } catch {
          await authApi.refresh()
          this.user = await authApi.me()
        }
        applyUserLanguage(this.user)
      } finally {
        this.ready = true
      }
    },
    async setLanguage(language: AppLocale) {
      this.user = await authApi.updateProfile({ language })
      setLocale(language)
    },
    async logout() {
      await authApi.logout()
      this.user = null
    }
  }
})
