import { api } from './client'
import type { Tool, ToolTestRun, ToolVersion } from './types'

export async function listTools(): Promise<Tool[]> {
  const { data } = await api.get<Tool[]>('/tools')
  return data ?? []
}

export interface CreateToolRequest {
  name: string
  source_type: string
  description: string
  schema_json: string
  endpoint_config: string
  auth_config: string
  sensitive: boolean
}

export async function createTool(req: CreateToolRequest): Promise<Tool> {
  const { data } = await api.post<Tool>('/tools', req)
  return data
}

// Sandbox 试调:真正构造执行器调用一次并落账(供 Publish 门禁)。input 是 JSON 串。
export async function testTool(toolId: string, input: unknown): Promise<ToolTestRun> {
  const { data } = await api.post<ToolTestRun>(`/tools/${toolId}/test`, { input })
  return data
}

export async function publishTool(toolId: string): Promise<void> {
  await api.post(`/tools/${toolId}/publish`)
}

export async function listToolVersions(toolId: string): Promise<ToolVersion[]> {
  const { data } = await api.get<ToolVersion[]>(`/tools/${toolId}/versions`)
  return data ?? []
}

export async function createToolVersion(
  toolId: string,
  body: Pick<ToolVersion, 'schema_json' | 'endpoint_config' | 'auth_config' | 'retry_policy'>,
): Promise<ToolVersion> {
  const { data } = await api.post<ToolVersion>(`/tools/${toolId}/versions`, body)
  return data
}

export async function publishToolVersion(toolId: string, versionId: string): Promise<void> {
  await api.post(`/tools/${toolId}/versions/${versionId}/publish`)
}
