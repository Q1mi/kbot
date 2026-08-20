import { api } from './client'
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
