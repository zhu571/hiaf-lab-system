import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LoginView from '@/views/LoginView.vue'
import { createTestI18n } from '@/test-utils/setup'

// LoginView 页面测试（测试方案 §3.2 🔴）：表单提交调 store.login、
// 错误提示展示、登录中防重；注册对话框前端校验。

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  register: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

import { login, register } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const pushMock = vi.fn()

async function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const wrapper = mount(LoginView, {
    global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true } }
  })
  await flushPromises()
  return wrapper
}

function formInputs(wrapper: Awaited<ReturnType<typeof mountView>>) {
  const inputs = wrapper.findAll('input')
  return { username: inputs[0], password: inputs[1] }
}

function submitButton(wrapper: Awaited<ReturnType<typeof mountView>>) {
  return wrapper.findAll('button').find((b) => b.text().trim() === '登录')!
}

/** jsdom 下点击 submit 按钮不触发隐式表单提交，直接对 form 元素派发 submit */
async function submitLoginForm(wrapper: Awaited<ReturnType<typeof mountView>>) {
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

async function submitRegisterForm(wrapper: Awaited<ReturnType<typeof mountView>>) {
  await wrapper.findAll('form')[1].trigger('submit')
  await flushPromises()
}

beforeEach(() => {
  vi.mocked(login).mockReset()
  vi.mocked(register).mockReset()
  pushMock.mockReset()
  // 登录成功后 auth.login 走真实 store action → authApi.login（已 mock）→ router.push
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LoginView 登录', () => {
  it('提交表单调用 store.login 并跳转首页；登录中按钮 loading 防重', async () => {
    let resolveLogin: (v: { user: { id: string }; csrf_token: string }) => void = () => {}
    vi.mocked(login).mockImplementation(
      () => new Promise((resolve) => (resolveLogin = resolve as (v: { user: { id: string }; csrf_token: string }) => void))
    )
    const wrapper = await mountView()
    const { username, password } = formInputs(wrapper)
    await username.setValue('haofan')
    await password.setValue('Test1234!')
    await submitLoginForm(wrapper)
    expect(login).toHaveBeenCalledWith('haofan', 'Test1234!')
    // 防重：loading 期间提交按钮 disabled（el-button loading 语义）
    expect(submitButton(wrapper).classes()).toContain('is-loading')
    expect(submitButton(wrapper).attributes('disabled')).toBeDefined()
    resolveLogin({ user: { id: 'u1' }, csrf_token: 't' })
    await flushPromises()
    expect(useAuthStore().user?.id).toBe('u1')
  })

  it('登录失败：el-alert 展示后端错误 message', async () => {
    vi.mocked(login).mockRejectedValue(new Error('用户名或密码错误'))
    const wrapper = await mountView()
    const { username, password } = formInputs(wrapper)
    await username.setValue('haofan')
    await password.setValue('wrong')
    await submitLoginForm(wrapper)
    const alert = wrapper.find('.el-alert')
    expect(alert.exists()).toBe(true)
    expect(alert.find('.el-alert__title').text()).toBe('用户名或密码错误')
  })
})

describe('LoginView 注册', () => {
  it('注册对话框前端校验：密码不一致 / 用户名过短 / 密码过短给出对应提示', async () => {
    const wrapper = await mountView()
    const registerLink = wrapper.findAll('button').find((b) => b.text().trim() === '注册')!
    await registerLink.trigger('click')
    await flushPromises()
    // 对话框内 4 个输入框：用户名/密码/确认密码/邀请码
    const dialogInputs = wrapper.findAll('.el-dialog input')
    expect(dialogInputs).toHaveLength(4)
    const [u, p, c] = dialogInputs

    // 用户名 1 字符 → usernameLength
    await u.setValue('a')
    await p.setValue('Test12345!')
    await c.setValue('Test12345!')
    await submitRegisterForm(wrapper)
    expect(wrapper.find('.el-dialog .el-alert').text()).toContain('用户名长度需为 2-32 个字符')
    expect(register).not.toHaveBeenCalled()

    // 密码不一致 → passwordMismatch
    await u.setValue('haofan')
    await c.setValue('Different123!')
    await submitRegisterForm(wrapper)
    expect(wrapper.find('.el-dialog .el-alert').text()).toContain('两次输入的密码不一致')
    expect(register).not.toHaveBeenCalled()

    // 合法输入 → register + login 依次调用
    await c.setValue('Test12345!')
    await dialogInputs[3].setValue('invite-code')
    await submitRegisterForm(wrapper)
    expect(register).toHaveBeenCalledWith('haofan', 'Test12345!', 'invite-code')
    expect(login).toHaveBeenCalledWith('haofan', 'Test12345!')
  })

  it('注册密码不符合规则：passwordLength 提示，不发请求', async () => {
    const wrapper = await mountView()
    const registerLink = wrapper.findAll('button').find((b) => b.text().trim() === '注册')!
    await registerLink.trigger('click')
    await flushPromises()
    const dialogInputs = wrapper.findAll('.el-dialog input')
    await dialogInputs[0].setValue('haofan')
    await dialogInputs[1].setValue('short')
    await dialogInputs[2].setValue('short')
    await submitRegisterForm(wrapper)
    expect(wrapper.find('.el-dialog .el-alert').text()).toContain('密码至少 10 位')
    expect(register).not.toHaveBeenCalled()
  })
})
