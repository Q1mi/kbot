import { Select, Typography } from 'antd'
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { listWorkspaces } from '@/api/iam'
import { useAuthStore } from '@/store/authStore'

// 顶栏工作空间切换器:Agents / Teams 等列表按 X-Workspace-ID 过滤,
// 必须先选一个工作空间。无选中时自动挑第一个。
export function WorkspaceSwitcher() {
  const { workspaceId, setWorkspace } = useAuthStore()
  const { data: workspaces = [] } = useQuery({
    queryKey: ['workspaces'],
    queryFn: () => listWorkspaces(),
  })

  useEffect(() => {
    const selectedStillExists = !!workspaceId && workspaces.some((workspace) => workspace.id === workspaceId)
    if (!selectedStillExists && workspaces.length > 0) {
      setWorkspace(workspaces[0].id)
    }
  }, [workspaceId, workspaces, setWorkspace])

  if (workspaces.length === 0) {
    return <Typography.Text type="secondary">无工作空间</Typography.Text>
  }

  return (
    <Select
      size="small"
      style={{ minWidth: 160 }}
      value={workspaceId ?? undefined}
      onChange={setWorkspace}
      placeholder="选择工作空间"
      options={workspaces.map((w) => ({ value: w.id, label: w.name }))}
    />
  )
}
