import { api } from './client'
import type { Team, CreateTeamRequest, TeamMember, TeamVersion } from './types'

export async function listTeams(): Promise<Team[]> {
  const { data } = await api.get<Team[]>('/teams')
  return data ?? []
}

export interface CreateTeamResult {
  team: Team
  version: unknown
}

export async function createTeam(req: CreateTeamRequest): Promise<CreateTeamResult> {
  const { data } = await api.post<CreateTeamResult>('/teams', req)
  return data
}

export interface RunByEnvRequest {
  team_id: string
  env: string
  input: string
}

export interface TeamRunResult {
  final: string
  steps: { agent_id?: string; output?: string; [k: string]: unknown }[]
}

// 按持久化团队 + env 跑一次协作。
export async function runTeam(req: RunByEnvRequest): Promise<TeamRunResult> {
  const { data } = await api.post<TeamRunResult>('/teams/runs', req)
  return data
}

export async function listTeamVersions(teamId: string): Promise<TeamVersion[]> {
  const { data } = await api.get<TeamVersion[]>(`/teams/${teamId}/versions`)
  return data ?? []
}

export async function createTeamVersion(
  teamId: string,
  body: { members: TeamMember[]; agent_env: string },
): Promise<TeamVersion> {
  const { data } = await api.post<TeamVersion>(`/teams/${teamId}/versions`, body)
  return data
}

export async function promoteTeamVersion(teamId: string, versionId: string, env: string): Promise<void> {
  await api.post(`/teams/${teamId}/promote`, { version_id: versionId, env })
}
