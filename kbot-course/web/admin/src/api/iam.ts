import { api } from './client'
import type { Workspace } from './types'
import type { User } from '@/store/authStore'

export async function listUsers(limit = 50, offset = 0): Promise<User[]> {
  const { data } = await api.get<User[]>('/users', { params: { limit, offset } })
  return data ?? []
}

export async function listWorkspaces(limit = 50, offset = 0): Promise<Workspace[]> {
  const { data } = await api.get<Workspace[]>('/workspaces', { params: { limit, offset } })
  return data ?? []
}

export interface CreateWorkspaceRequest {
  name: string
  description?: string
  parent_id?: string | null
}

export async function createWorkspace(req: CreateWorkspaceRequest): Promise<Workspace> {
  const { data } = await api.post<Workspace>('/workspaces', req)
  return data
}
