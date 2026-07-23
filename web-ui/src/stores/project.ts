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
      // 后端空列表返回 data: null（Go nil slice），必须兜底为空数组，
      // 否则 this.projects[0] 直接抛 TypeError，且 store 里残留的 null 会让
      // ProjectSidebar/ProjectLayout 等所有消费方渲染崩溃
      this.projects = (await listProjects()) ?? []
      if (!this.currentId && this.projects[0]) this.currentId = this.projects[0].id
    },
    select(id: string) {
      this.currentId = id
    }
  }
})
