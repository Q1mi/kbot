import { create } from 'zustand'
import { persist } from 'zustand/middleware'

// 后端 LoginResponse.user 的字段(见 internal/domain User)。
export interface User {
  id: string
  email: string
  name: string
  status?: string
}

interface AuthState {
  token: string | null
  user: User | null
  workspaceId: string | null
  setAuth: (token: string, user: User) => void
  setWorkspace: (id: string | null) => void
  logout: () => void
}

// token / user / 当前 workspace 持久化到 localStorage,刷新后保持登录。
export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      workspaceId: null,
      setAuth: (token, user) => set({ token, user }),
      setWorkspace: (workspaceId) => set({ workspaceId }),
      logout: () => set({ token: null, user: null, workspaceId: null }),
    }),
    { name: 'kbot-auth' },
  ),
)
