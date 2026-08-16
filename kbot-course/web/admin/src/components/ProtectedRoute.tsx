import { Redirect, useLocation } from 'wouter'
import { useAuthStore } from '@/store/authStore'
import type { ReactNode } from 'react'

// 未登录(无 token)→ 重定向到 /login,并记下来源以便登录后跳回。
export function ProtectedRoute({ children }: { children: ReactNode }) {
  const token = useAuthStore((s) => s.token)
  const [location] = useLocation()
  if (!token) {
    return <Redirect to={`/login?from=${encodeURIComponent(location)}`} />
  }
  return <>{children}</>
}
