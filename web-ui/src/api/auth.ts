import { request, requestWithMeta, setCSRFToken } from './client'

export type UserInfo = {
  id: string
  username: string
  display_name: string
  role: string
  must_change_password: boolean
  created_at: string
  disabled: boolean
  language: string
}

type LoginResponse = {
  csrf_token: string
  must_change_password: boolean
  user: UserInfo
}

export function register(username: string, password: string, invitation_code: string): Promise<UserInfo> {
  return request({ method: 'POST', url: '/auth/register', data: { username, password, invitation_code } })
}

export type InvitationCode = { id:string; code_prefix:string; status:'active'|'used'|'expired'|'revoked'; created_by:string; used_by:string|null; expires_at:string; used_at:string|null; revoked_at:string|null; created_at:string }
export type InvitationCodeList = { items: InvitationCode[]; total:number; page:number; per_page:number }
export function listInvitationCodes(params: {page?:number;per_page?:number;status?:string}={}) { return request<InvitationCodeList>({url:'/admin/invitation-codes',params}) }
export function createInvitationCode(data: {expires_at?:string}) { return requestWithMeta<{invitation:InvitationCode;code:string}>({url:'/admin/invitation-codes',method:'POST',data}) }
export function revokeInvitationCode(id:string) { return requestWithMeta<InvitationCode>({url:`/admin/invitation-codes/${id}/revoke`,method:'POST',data:{}}) }

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

export function updateProfile(data: { language: string }) {
  return request<UserInfo>({ url: '/auth/profile', method: 'PATCH', data })
}

export function logout() {
  return request<{ success: boolean }>({ url: '/auth/logout', method: 'POST', data: {} })
}

export function listUsers() {
  return request<UserInfo[]>({ url: '/admin/users' })
}

export function createUser(data: { username: string; display_name?: string; role?: string; password?: string }) {
  return requestWithMeta<{ user: UserInfo; temporary_password: string }>({ url: '/admin/users', method: 'POST', data })
}

export function updateUser(id: string, data: { display_name?: string; role?: string; disabled?: boolean }) {
  return requestWithMeta<UserInfo>({ url: `/admin/users/${id}`, method: 'PATCH', data })
}

export function resetPassword(id: string, new_password?: string) {
  return requestWithMeta<{ temporary_password: string }>({ url: `/admin/users/${id}/reset-password`, method: 'POST', data: { new_password } })
}
