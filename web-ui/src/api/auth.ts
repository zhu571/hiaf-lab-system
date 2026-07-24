import { request, setCSRFToken } from './client'

export type UserInfo = {
  id: string
  username: string
  display_name: string
  role: string
  must_change_password: boolean
  created_at: string
  disabled: boolean
}

type LoginResponse = {
  csrf_token: string
  must_change_password: boolean
  user: UserInfo
}

export function register(username: string, password: string): Promise<UserInfo> {
  return request({ method: 'POST', url: '/auth/register', data: { username, password } })
}

export async function login(username: string, password: string) {
  const data = await request<LoginResponse>({ url: '/auth/login', method: 'POST', data: { username, password } })
  setCSRFToken(data.csrf_token)
  return data
}

export async function refresh() {
  const data = await request<LoginResponse>({ url: '/auth/refresh', method: 'POST', data: {} })
  setCSRFToken(data.csrf_token)
  return data
}

export function me() {
  return request<UserInfo>({ url: '/auth/me' })
}

export function changePassword(old_password: string, new_password: string) {
  return request<{ success: boolean }>({ url: '/auth/change-password', method: 'POST', data: { old_password, new_password } })
}

export function logout() {
  return request<{ success: boolean }>({ url: '/auth/logout', method: 'POST', data: {} })
}

export function listUsers() {
  return request<UserInfo[]>({ url: '/admin/users' })
}

export function createUser(data: { username: string; display_name?: string; role?: string; password?: string }) {
  return request<{ user: UserInfo; temporary_password: string }>({ url: '/admin/users', method: 'POST', data })
}

export function updateUser(id: string, data: { display_name?: string; role?: string; disabled?: boolean }) {
  return request<UserInfo>({ url: `/admin/users/${id}`, method: 'PATCH', data })
}

export function resetPassword(id: string, new_password?: string) {
  return request<{ temporary_password: string }>({ url: `/admin/users/${id}/reset-password`, method: 'POST', data: { new_password } })
}
