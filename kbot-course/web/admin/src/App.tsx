import { Redirect, Route, Switch } from 'wouter'
import type { ReactNode } from 'react'
import { AppLayout } from '@/components/AppLayout'
import { ProtectedRoute } from '@/components/ProtectedRoute'
import { LoginPage } from '@/pages/LoginPage'
import { WorkspacesPage } from '@/pages/WorkspacesPage'
import { UsersPage } from '@/pages/UsersPage'
import { AgentsPage } from '@/pages/AgentsPage'
import { AgentDetailPage } from '@/pages/AgentDetailPage'
import { ConversationsPage } from '@/pages/ConversationsPage'
import { TeamsPage } from '@/pages/TeamsPage'
import { PromptsPage } from '@/pages/PromptsPage'
import { PromptDetailPage } from '@/pages/PromptDetailPage'
import { SkillsPage } from '@/pages/SkillsPage'
import { SkillDetailPage } from '@/pages/SkillDetailPage'
import { ToolsPage } from '@/pages/ToolsPage'
import { ToolDetailPage } from '@/pages/ToolDetailPage'
import { KBsPage } from '@/pages/KBsPage'
import { KBDetailPage } from '@/pages/KBDetailPage'
import { EvalPage } from '@/pages/EvalPage'
import { GuardPage } from '@/pages/GuardPage'
import { AuditPage } from '@/pages/AuditPage'
import { ObservabilityPage } from '@/pages/ObservabilityPage'
import { ModelsPage } from '@/pages/ModelsPage'
import { NAV } from '@/nav'

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
  )
}
