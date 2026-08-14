import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import CommentSection from '@/components/business/CommentSection.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { Comment } from '@/api/issues'

// CommentSection 组件测试（测试方案 §3.2 🔴 深测）：
// 评论列表渲染（空态/内容）、submit emit 载荷 + 输入清空、空输入拦截。

function makeComment(overrides: Partial<Comment> = {}): Comment {
  return {
    id: 'c_01',
    issue_id: 'issue_01',
    author_id: 'haofan',
    content: '已复现，正在定位原因',
    created_at: '2026-01-05T10:00:00+08:00',
    ...overrides
  }
}

function mountSection(comments: Comment[]) {
  return mount(CommentSection, {
    props: { comments },
    global: { plugins: [createTestI18n()] }
  })
}

describe('CommentSection 渲染', () => {
  it('无评论显示空态提示', () => {
    const wrapper = mountSection([])
    expect(wrapper.text()).toContain('暂无评论')
    expect(wrapper.findAll('.comment')).toHaveLength(0)
  })

  it('评论列表渲染：头像首字母、作者、内容、时间', () => {
    const wrapper = mountSection([makeComment()])
    const comment = wrapper.find('.comment')
    expect(comment.find('.avatar').text()).toBe('H')
    expect(comment.find('strong').text()).toBe('haofan')
    expect(comment.text()).toContain('已复现，正在定位原因')
    expect(comment.text()).toContain('2026')
  })
})

describe('CommentSection 提交', () => {
  it('输入内容发送：emit submit 载荷为内容并清空输入框；空输入按钮禁用', async () => {
    const wrapper = mountSection([])
    const textarea = wrapper.find('textarea')
    const sendButton = wrapper.findAll('button').find((b) => b.text().trim() === '发送')!
    expect(sendButton.attributes('disabled')).toBeDefined()
    await textarea.setValue('补充：低压段数据异常')
    expect(sendButton.attributes('disabled')).toBeUndefined()
    await sendButton.trigger('click')
    expect(wrapper.emitted('submit')).toEqual([['补充：低压段数据异常']])
    expect(textarea.element.value).toBe('')
    // 清空后按钮恢复禁用
    expect(sendButton.attributes('disabled')).toBeDefined()
  })
})
