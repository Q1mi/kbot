import { Redirect, Route, Switch } from 'wouter'
import { lazy, Suspense, type ComponentType, type ReactNode } from 'react'
import { Spin } from 'antd'
import { AppLayout } from '@/components/AppLayout'
import { ProtectedRoute } from '@/components/ProtectedRoute'
import { NAV } from '@/nav'

const lazyNamed = <T extends Record<string, unknown>, K extends keyof T>(loader: () => Promise<T>, name: K) =>
  lazy(async () => ({ default: (await loader())[name] as ComponentType }))

const LoginPage = lazyNamed(() => import('@/pages/LoginPage'), 'LoginPage')
const WorkspacesPage = lazyNamed(() => import('@/pages/WorkspacesPage'), 'WorkspacesPage')
const UsersPage = lazyNamed(() => import('@/pages/UsersPage'), 'UsersPage')
const AgentsPage = lazyNamed(() => import('@/pages/AgentsPage'), 'AgentsPage')
const AgentDetailPage = lazyNamed(() => import('@/pages/AgentDetailPage'), 'AgentDetailPage')
const ConversationsPage = lazyNamed(() => import('@/pages/ConversationsPage'), 'ConversationsPage')
const TeamsPage = lazyNamed(() => import('@/pages/TeamsPage'), 'TeamsPage')
const PromptsPage = lazyNamed(() => import('@/pages/PromptsPage'), 'PromptsPage')
const PromptDetailPage = lazyNamed(() => import('@/pages/PromptDetailPage'), 'PromptDetailPage')
const SkillsPage = lazyNamed(() => import('@/pages/SkillsPage'), 'SkillsPage')
const SkillDetailPage = lazyNamed(() => import('@/pages/SkillDetailPage'), 'SkillDetailPage')
const ToolsPage = lazyNamed(() => import('@/pages/ToolsPage'), 'ToolsPage')
const ToolDetailPage = lazyNamed(() => import('@/pages/ToolDetailPage'), 'ToolDetailPage')
const KBsPage = lazyNamed(() => import('@/pages/KBsPage'), 'KBsPage')
const KBDetailPage = lazyNamed(() => import('@/pages/KBDetailPage'), 'KBDetailPage')
const EvalPage = lazyNamed(() => import('@/pages/EvalPage'), 'EvalPage')
const GuardPage = lazyNamed(() => import('@/pages/GuardPage'), 'GuardPage')
const AuditPage = lazyNamed(() => import('@/pages/AuditPage'), 'AuditPage')
const ObservabilityPage = lazyNamed(() => import('@/pages/ObservabilityPage'), 'ObservabilityPage')
const ModelsPage = lazyNamed(() => import('@/pages/ModelsPage'), 'ModelsPage')

// 左侧导航对应的 14 个主页面。
const REAL_PAGES: Record<string, ReactNode> = {
  workspaces: <WorkspacesPage />,
  users: <UsersPage />,
  agents: <AgentsPage />,
  conversations: <ConversationsPage />,
  teams: <TeamsPage />,
  prompts: <PromptsPage />,
  models: <ModelsPage />,
  skills: <SkillsPage />,
  tools: <ToolsPage />,
  kbs: <KBsPage />,
  eval: <EvalPage />,
  guard: <GuardPage />,
  audit: <AuditPage />,
  observability: <ObservabilityPage />,
}

// 已提供的详情页。
const DETAIL_ROUTES: { path: string; element: ReactNode }[] = [
  { path: 'agents/:id', element: <AgentDetailPage /> },
  { path: 'prompts/:id', element: <PromptDetailPage /> },
  { path: 'tools/:id', element: <ToolDetailPage /> },
  { path: 'skills/:id', element: <SkillDetailPage /> },
  { path: 'kbs/:id', element: <KBDetailPage /> },
]

export default function App() {
  return (
	<Suspense fallback={<div style={{ display: 'grid', minHeight: '100vh', placeItems: 'center' }}><Spin size="large" /></div>}>
	<Switch>
      <Route path="/login" component={LoginPage} />
      <Route>
        <ProtectedRoute>
          <AppLayout>
            <Switch>
              <Route path="/">
                <Redirect to="/agents" />
              </Route>
              {NAV.map((n) => (
                <Route key={n.key} path={n.path}>
                  {REAL_PAGES[n.key]}
                </Route>
              ))}
              {DETAIL_ROUTES.map((d) => (
                <Route key={d.path} path={`/${d.path}`}>
                  {d.element}
                </Route>
              ))}
              <Route>
                <Redirect to="/agents" />
              </Route>
            </Switch>
          </AppLayout>
        </ProtectedRoute>
      </Route>
	</Switch>
	</Suspense>
  )
}
