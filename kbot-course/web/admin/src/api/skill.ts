import { api } from './client'
import type { Skill, SkillVersion } from './types'

export async function listSkills(): Promise<Skill[]> {
  const { data } = await api.get<Skill[]>('/skills')
  return data ?? []
}

export interface CreateSkillRequest {
  category: string
  skill_md: string
}

export async function createSkill(req: CreateSkillRequest): Promise<{ skill: Skill; version: SkillVersion }> {
  const { data } = await api.post('/skills', req)
  return data
}

export async function createSkillVersion(skillId: string, skillMD: string): Promise<SkillVersion> {
  const { data } = await api.post<SkillVersion>(`/skills/${skillId}/versions`, { skill_md: skillMD })
  return data
}

export async function listSkillVersions(skillId: string): Promise<SkillVersion[]> {
  const { data } = await api.get<SkillVersion[]>(`/skills/${skillId}/versions`)
  return data ?? []
}

// 发布技能版本(走强制校验门禁)。路由是 /skills/{skill_id}/publish,
// handler 只用 body.version_id,skill_id 仅用于路由匹配。
export async function publishSkill(skillId: string, versionId: string): Promise<void> {
  await api.post(`/skills/${skillId}/publish`, { version_id: versionId })
}
