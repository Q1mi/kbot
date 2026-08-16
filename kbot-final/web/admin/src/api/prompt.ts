import { api } from './client'
import type { Prompt, PromptVersion } from './types'

export async function listPrompts(): Promise<Prompt[]> {
  const { data } = await api.get<Prompt[]>('/prompts')
  return data ?? []
}

export interface CreatePromptRequest {
  name: string
  category: string
  template: string
  variables_schema: string
  model_profile_version_id?: string
  generation_config?: {
    temperature?: number
    top_p?: number
    max_output_tokens?: number
  }
}

export async function createPrompt(req: CreatePromptRequest): Promise<{ prompt: Prompt; version: PromptVersion }> {
  const { data } = await api.post('/prompts', req)
  return data
}

export async function listVersions(promptId: string): Promise<PromptVersion[]> {
  const { data } = await api.get<PromptVersion[]>(`/prompts/${promptId}/versions`)
  return data ?? []
}

export async function createVersion(
  promptId: string,
  template: string,
  variablesSchema: string,
  modelProfileVersionId?: string,
  generationConfig?: Record<string, unknown>,
): Promise<PromptVersion> {
  const { data } = await api.post<PromptVersion>(`/prompts/${promptId}/versions`, {
    template,
    variables_schema: variablesSchema,
    model_profile_version_id: modelProfileVersionId,
    generation_config: generationConfig ?? {},
  })
  return data
}

export async function startRollout(
  promptId: string, env: string, candidateVersionId: string, trafficPercent: number,
): Promise<void> {
  await api.post(`/prompts/${promptId}/rollouts`, {
    env, candidate_version_id: candidateVersionId, traffic_percent: trafficPercent,
  })
}

export async function updateRolloutTraffic(promptId: string, env: string, trafficPercent: number): Promise<void> {
  await api.put(`/prompts/${promptId}/rollouts/traffic`, { env, traffic_percent: trafficPercent })
}

export async function completeRollout(promptId: string, env: string): Promise<void> {
  await api.post(`/prompts/${promptId}/rollouts/complete`, { env })
}

export async function rollbackRollout(promptId: string, env: string): Promise<void> {
  await api.post(`/prompts/${promptId}/rollouts/rollback`, { env })
}

// 晋升 / 回滚:改 env 指针(dev/staging/prod)。
export async function promote(promptId: string, env: string, versionId: string): Promise<void> {
  await api.post(`/prompts/${promptId}/promote`, { env, version_id: versionId })
}

export async function rollback(promptId: string, env: string, versionId: string): Promise<void> {
  await api.post(`/prompts/${promptId}/rollback`, { env, version_id: versionId })
}

export async function render(promptId: string, env: string, vars: Record<string, unknown>): Promise<string> {
  const { data } = await api.post<{ rendered: string }>(`/prompts/${promptId}/render`, { env, vars })
  return data.rendered
}
