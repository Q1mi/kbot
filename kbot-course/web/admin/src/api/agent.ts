import { api } from './client'
import type { Agent, AgentVersion, AgentVersionConfig, CreateAgentRequest, Conversation, Message } from './types'
import { useAuthStore } from '@/store/authStore'

export async function listAgents(): Promise<Agent[]> {
  const { data } = await api.get<Agent[]>('/agents')
  return data ?? []
}

export async function getAgent(id: string): Promise<Agent> {
  const { data } = await api.get<Agent>(`/agents/${id}`)
  return data
}

export async function createAgent(req: CreateAgentRequest): Promise<Agent> {
  const { data } = await api.post<Agent>('/agents', req)
  return data
}

export async function listAgentVersions(agentId: string): Promise<AgentVersion[]> {
  const { data } = await api.get<AgentVersion[]>(`/agents/${agentId}/versions`)
  return data ?? []
}

export async function createAgentVersion(agentId: string, config: AgentVersionConfig): Promise<AgentVersion> {
  const { data } = await api.post<AgentVersion>(`/agents/${agentId}/versions`, config)
  return data
}

export async function promoteAgentVersion(agentId: string, versionId: string, env: string): Promise<void> {
  await api.post(`/agents/${agentId}/promote`, { version_id: versionId, env })
}

export interface ChatResult {
  content: string
  conversation_id?: string
  trace_id?: string
  approval_id?: string
  status?: 'completed' | 'awaiting_approval'
}

// 同步聊天:返回完整回答(底层走引擎流式后聚合)。conversationId 为空则后端新建会话。
export async function chat(agentId: string, message: string, conversationId?: string, agentEnv = 'dev'): Promise<ChatResult> {
  const { data } = await api.post<ChatResult>(`/agents/${agentId}/chat`, {
    message,
    conversation_id: conversationId,
    agent_env: agentEnv,
  })
  return data
}

export interface AgentStreamEvent<T = unknown> {
  type: string
  data?: T
  text?: string
}

export interface RunStarted {
  conversation_id: string
  agent_version_id: string
  trace_id?: string
  user_message?: string
  user_prompt_version_id?: string
}

export interface UserPromptInputSpec {
  enabled: boolean
  prompt_id?: string
  prompt_name?: string
  prompt_env?: string
  prompt_version_id?: string
  prompt_version?: number
  variables_schema?: string
}

export interface UserPromptSubmission {
  versionId: string
  variables: Record<string, unknown>
}

export async function getUserPromptInputSpec(agentId: string, agentEnv = 'dev'): Promise<UserPromptInputSpec> {
  const { data } = await api.get<UserPromptInputSpec>(`/agents/${agentId}/input-schema`, {
    params: { agent_env: agentEnv },
  })
  return data
}

export interface RunFinished {
  status: 'completed' | 'awaiting_approval'
}

// POST SSE 客户端。浏览器 EventSource 只支持 GET，这里通过 fetch + ReadableStream
// 逐块解析标准 SSE frame，并保留认证头与多轮 conversation_id。
export async function streamChat(
  agentId: string,
  message: string,
  conversationId: string | undefined,
  agentEnv: string,
  onEvent: (event: AgentStreamEvent) => void,
  userPrompt?: UserPromptSubmission,
  signal?: AbortSignal,
): Promise<void> {
  const { token, workspaceId } = useAuthStore.getState()
  const response = await fetch(`/stream/agents/${encodeURIComponent(agentId)}/chat`, {
    method: 'POST',
    signal,
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(workspaceId ? { 'X-Workspace-ID': workspaceId } : {}),
    },
    body: JSON.stringify({
      message,
      conversation_id: conversationId,
      agent_env: agentEnv,
      ...(userPrompt ? {
        user_prompt_version_id: userPrompt.versionId,
        user_prompt_variables: userPrompt.variables,
      } : {}),
    }),
  })
  if (!response.ok || !response.body) {
    throw new Error((await response.text()) || `SSE request failed: HTTP ${response.status}`)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  const consumeFrame = (frame: string) => {
    const dataLines = frame
      .split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trimStart())
    if (dataLines.length === 0) return
    onEvent(JSON.parse(dataLines.join('\n')) as AgentStreamEvent)
  }

  while (true) {
    const { value, done } = await reader.read()
    buffer += decoder.decode(value, { stream: !done })
    const frames = buffer.split(/\r?\n\r?\n/)
    buffer = frames.pop() ?? ''
    frames.forEach(consumeFrame)
    if (done) break
  }
  if (buffer.trim()) consumeFrame(buffer)
}

export interface ConversationDetail {
  conversation: Conversation
  messages: Message[]
}

export async function getConversation(id: string): Promise<ConversationDetail> {
  const { data } = await api.get<ConversationDetail>(`/conversations/${id}`)
  return data
}

export async function listConversations(agentId?: string): Promise<Conversation[]> {
  const { data } = await api.get<Conversation[]>('/conversations', {
    params: { agent_id: agentId || undefined, limit: 50 },
  })
  return data ?? []
}
