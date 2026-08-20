import { api } from './client'
import type { GuardQuota, GuardRule } from './types'

export async function listGuardRules(): Promise<GuardRule[]> {
  const { data } = await api.get<GuardRule[]>('/guard/rules')
  return data ?? []
}

export type GuardRuleInput = Pick<GuardRule, 'kind' | 'hook' | 'pattern_or_model' | 'action' | 'enabled'>

export async function createGuardRule(body: GuardRuleInput): Promise<GuardRule> {
  const { data } = await api.post<GuardRule>('/guard/rules', body)
  return data
}

export async function updateGuardRule(id: string, body: GuardRuleInput): Promise<GuardRule> {
  const { data } = await api.put<GuardRule>(`/guard/rules/${id}`, body)
  return data
}

export async function listGuardQuotas(): Promise<GuardQuota[]> {
  const { data } = await api.get<GuardQuota[]>('/guard/quotas')
  return data ?? []
}

export async function setGuardQuota(metric: string, limit: number): Promise<GuardQuota> {
  const { data } = await api.put<GuardQuota>(`/guard/quotas/${metric}`, { limit })
  return data
}
