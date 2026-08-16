import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface UIState {
  collapsed: boolean
  toggleCollapsed: () => void
}

// 侧边栏折叠状态(主题切换等留作后续)。
export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      collapsed: false,
      toggleCollapsed: () => set((s) => ({ collapsed: !s.collapsed })),
    }),
    { name: 'kbot-ui' },
  ),
)
