import axios from 'axios'
import { api, getAPIErrorMessage } from './client'
import type { User } from '@/store/authStore'

export interface LoginRequest {
  email: string
  password: string
}

export interface LoginResponse {
  token: string
  expires_at: string
  user: User
}

export function getLoginErrorMessage(error: unknown) {
  if (axios.isAxiosError(error) && error.response?.status === 401) {
    return '邮箱或密码错误'
  }
  return getAPIErrorMessage(error, '登录失败，请稍后重试')
}

export async function login(req: LoginRequest): Promise<LoginResponse> {
  const { data } = await api.post<LoginResponse>('/auth/login', req)
  return data
}

export interface CreateUserRequest {
  email: string
  password: string
  name: string
}

export async function register(req: CreateUserRequest): Promise<User> {
  const { data } = await api.post<User>('/auth/register', req)
  return data
}
