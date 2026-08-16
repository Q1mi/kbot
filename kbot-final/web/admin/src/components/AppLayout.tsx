import { Layout, Menu, Breadcrumb, Dropdown, Avatar, Button, Typography } from 'antd'
import {
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  UserOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import { useLocation } from 'wouter'
import { useMemo, type ReactNode } from 'react'
import { NAV } from '@/nav'
import { useAuthStore } from '@/store/authStore'
import { useUIStore } from '@/store/uiStore'
import { WorkspaceSwitcher } from './WorkspaceSwitcher'
import './AppLayout.css'

const { Header, Sider, Content } = Layout

// 把 NAV 按 group 折成 AntD Menu 的分组结构。
function useMenuItems() {
  return useMemo(() => {
    const groups: Record<string, typeof NAV> = {}
    for (const item of NAV) (groups[item.group] ??= []).push(item)
    return Object.entries(groups).map(([group, items]) => ({
      key: group,
      label: group,
      type: 'group' as const,
      children: items.map((it) => ({ key: it.key, icon: it.icon, label: it.label })),
    }))
  }, [])
}

export function AppLayout({ children }: { children: ReactNode }) {
  const [location, navigate] = useLocation()
  const { collapsed, toggleCollapsed } = useUIStore()
  const { user, logout } = useAuthStore()
  const items = useMenuItems()

  // 当前选中 = path 第一段;面包屑 = 该 NAV 项的 label。
  const seg = location.split('/')[1] || 'workspaces'
  const current = NAV.find((n) => n.key === seg)
  const isConversationPage = seg === 'conversations'

  return (
    <Layout className="app-shell">
      <Sider
        className="app-sidebar"
        collapsible
        collapsed={collapsed}
        trigger={null}
        theme="dark"
        width={220}
      >
        <div className={`app-brand${collapsed ? ' is-collapsed' : ''}`}>
          {collapsed ? 'k' : 'kbot'}
        </div>
        <Menu
          className="app-navigation"
          theme="dark"
          mode="inline"
          selectedKeys={[seg]}
          items={items}
          onClick={({ key }) => {
            const item = NAV.find((n) => n.key === key)
            if (item) navigate(item.path)
          }}
        />
      </Sider>
      <Layout className="app-main-shell">
        <Header className="app-header">
          <Button
            className="app-sidebar-toggle"
            type="text"
            icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            onClick={toggleCollapsed}
          />
          <span className="app-header-actions">
            <WorkspaceSwitcher />
            <Dropdown
              menu={{
                items: [
                  {
                    key: 'logout',
                    icon: <LogoutOutlined />,
                    label: '退出登录',
                    onClick: () => {
                      logout()
                      navigate('/login')
                    },
                  },
                ],
              }}
            >
              <span className="app-user-menu">
                <Avatar size="small" icon={<UserOutlined />} />
                <Typography.Text className="app-user-name">
                  {user?.name || user?.email || '未登录'}
                </Typography.Text>
              </span>
            </Dropdown>
          </span>
        </Header>
        <Content className={`app-content${isConversationPage ? ' is-conversation-page' : ''}`}>
          <Breadcrumb
            className="app-breadcrumb"
            items={[{ title: 'kbot' }, { title: current?.label ?? '页面' }]}
          />
          <div className={`app-page-surface${isConversationPage ? ' is-conversation-page' : ''}`}>
            {children}
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}
