import axios, { AxiosError } from 'axios'
import { message } from 'antd'
import { useAuthStore } from '@/store/authStore'

// 后端挂在 /api/v1;dev 期由 vite proxy 转发到 :8080,生产期同源。
export const api = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
})

type APIErrorPayload = { error?: string; message?: string } | string

export function getAPIErrorMessage(error: unknown, fallback = '请求失败') {
  if (!axios.isAxiosError<APIErrorPayload>(error)) {
    return error instanceof Error ? error.message : fallback
  }
  const data = error.response?.data
  if (typeof data === 'string') return data.trim() || error.message || fallback
  return data?.error || data?.message || error.message || fallback
}

// 请求拦截:带上 Bearer token + 当前 workspace。
api.interceptors.request.use((config) => {
  const { token, workspaceId } = useAuthStore.getState()
  if (token) config.headers.Authorization = `Bearer ${token}`
  if (workspaceId) config.headers['X-Workspace-ID'] = workspaceId
  return config
})

// 响应拦截:401 → 清登录态回登录页;其余错误统一弹 message。
api.interceptors.response.use(
  (resp) => resp,
  (err: AxiosError<APIErrorPayload>) => {
    if (err.response?.status === 401) {
      useAuthStore.getState().logout()
      if (location.pathname !== '/login') location.assign('/login')
    } else {
      message.error(getAPIErrorMessage(err))
    }
    return Promise.reject(err)
  },
)
