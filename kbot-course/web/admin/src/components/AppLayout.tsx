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

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider collapsible collapsed={collapsed} trigger={null} theme="dark" width={220}>
        <div
          style={{
            height: 56,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            fontWeight: 700,
            fontSize: collapsed ? 18 : 20,
            letterSpacing: 1,
          }}
        >
          {collapsed ? 'k' : 'kbot'}
        </div>
        <Menu
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
      <Layout>
        <Header
          style={{
            background: '#fff',
            padding: '0 16px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            borderBottom: '1px solid #f0f0f0',
          }}
        >
          <Button
            type="text"
            icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            onClick={toggleCollapsed}
          />
          <span style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
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
              <span style={{ cursor: 'pointer' }}>
                <Avatar size="small" icon={<UserOutlined />} />
                <Typography.Text style={{ marginLeft: 8 }}>
                  {user?.name || user?.email || '未登录'}
                </Typography.Text>
              </span>
            </Dropdown>
          </span>
        </Header>
        <Content style={{ margin: 16 }}>
          <Breadcrumb
            style={{ marginBottom: 12 }}
            items={[{ title: 'kbot' }, { title: current?.label ?? '页面' }]}
          />
          <div style={{ background: '#fff', padding: 24, borderRadius: 8, minHeight: 360 }}>
            {children}
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}
