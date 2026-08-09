import { defineStore } from 'pinia'
import { listProjects, type Project } from '../api/projects'

export const useProjectStore = defineStore('project', {
  state: () => ({
    projects: [] as Project[],
    currentId: ''
  }),
  getters: {
    current: (state) => state.projects.find((item) => item.id === state.currentId) || state.projects[0]
  },
  actions: {
    async load() {
      // 后端空列表的 data: null（Go nil slice）由 client.ts 响应拦截器统一兜底为 []（C15）
      this.projects = await listProjects()
      if (!this.currentId && this.projects[0]) this.currentId = this.projects[0].id
    },
    select(id: string) {
      this.currentId = id
    }
  }
})
