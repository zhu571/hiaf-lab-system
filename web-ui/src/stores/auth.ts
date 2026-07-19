import { defineStore } from 'pinia'
import * as authApi from '../api/auth'
import { setCSRFToken } from '../api/client'
import type { UserInfo } from '../api/auth'

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
      } finally {
        this.ready = true
      }
    },
    async logout() {
      await authApi.logout()
      this.user = null
    }
  }
})
