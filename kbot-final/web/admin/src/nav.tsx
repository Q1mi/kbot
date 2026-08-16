import {
  ApartmentOutlined,
  TeamOutlined,
  RobotOutlined,
  MessageOutlined,
  FileTextOutlined,
  ThunderboltOutlined,
  ToolOutlined,
  DatabaseOutlined,
  ExperimentOutlined,
  SafetyOutlined,
  AuditOutlined,
  DashboardOutlined,
  CloudServerOutlined,
  UsergroupAddOutlined,
} from '@ant-design/icons'
import type { ReactNode } from 'react'

export interface NavItem {
  key: string // 同时是路由 path(去掉前导 /)
  path: string
  label: string
  icon: ReactNode
  group: string
}

// 左侧菜单 + 主路由表共用的单一数据源（14 个主页面；详情页在 App.tsx 中配置）。
export const NAV: NavItem[] = [
  { key: 'workspaces', path: '/workspaces', label: '工作空间', icon: <ApartmentOutlined />, group: '组织管理' },
  { key: 'users', path: '/users', label: '用户', icon: <TeamOutlined />, group: '组织管理' },

  { key: 'models', path: '/models', label: '模型配置', icon: <CloudServerOutlined />, group: 'Agent 构建' },
  { key: 'prompts', path: '/prompts', label: 'Prompts', icon: <FileTextOutlined />, group: 'Agent 构建' },
  { key: 'tools', path: '/tools', label: 'Tools', icon: <ToolOutlined />, group: 'Agent 构建' },
  { key: 'kbs', path: '/kbs', label: '知识库', icon: <DatabaseOutlined />, group: 'Agent 构建' },
  { key: 'skills', path: '/skills', label: 'Skills', icon: <ThunderboltOutlined />, group: 'Agent 构建' },
  { key: 'agents', path: '/agents', label: 'Agents', icon: <RobotOutlined />, group: 'Agent 构建' },
  { key: 'teams', path: '/teams', label: '团队', icon: <UsergroupAddOutlined />, group: 'Agent 构建' },

  { key: 'conversations', path: '/conversations', label: '会话', icon: <MessageOutlined />, group: '运行验证' },
  { key: 'eval', path: '/eval', label: '评测', icon: <ExperimentOutlined />, group: '运行验证' },

  { key: 'guard', path: '/guard', label: '护栏', icon: <SafetyOutlined />, group: '安全治理' },
  { key: 'audit', path: '/audit', label: '审计', icon: <AuditOutlined />, group: '安全治理' },

  { key: 'observability', path: '/observability', label: '可观测', icon: <DashboardOutlined />, group: '可观测' },
]
