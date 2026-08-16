import { api } from './client'

export interface ProviderAccount {
  id: string
  name: string
  kind: string
  base_url: string
  status: string
  has_api_key: boolean
}

export interface ModelDeployment {
  id: string
  provider_account_id: string
  name: string
  model_name: string
  region: string
  timeout_ms: number
  max_retries: number
	input_price_per_million: number
	output_price_per_million: number
	cached_input_price_per_million: number
  status: string
}

export interface ModelProfile {
  id: string
  name: string
  description: string
}

export interface ModelProfileVersion {
  id: string
  profile_id: string
  version: number
  primary_deployment_id: string
  fallback_deployment_ids: string[]
  classification_max: string
}

export const listModelAccounts = async () =>
  (await api.get<ProviderAccount[]>('/model-accounts')).data ?? []

export const createModelAccount = async (body: {
  name: string
  kind: string
  base_url: string
  api_key: string
}) => (await api.post<ProviderAccount>('/model-accounts', body)).data

export const rotateModelAccountAPIKey = async (id: string, apiKey: string) =>
  api.put(`/model-accounts/${id}/api-key`, { api_key: apiKey })

export const listModelDeployments = async () =>
  (await api.get<ModelDeployment[]>('/model-deployments')).data ?? []

export const createModelDeployment = async (body: {
  provider_account_id: string
  name: string
  model_name: string
  region?: string
  timeout_ms?: number
  max_retries?: number
	input_price_per_million?: number
	output_price_per_million?: number
	cached_input_price_per_million?: number
}) => (await api.post<ModelDeployment>('/model-deployments', body)).data

export const updateModelDeploymentPricing = async (id: string, body: {
  input_price_per_million: number
  output_price_per_million: number
  cached_input_price_per_million: number
}) => (await api.put<ModelDeployment>(`/model-deployments/${id}/pricing`, body)).data

export interface ProjectModelBinding {
	workspace_id: string
	env: string
	model_profile_version_id: string
	monthly_budget: number
	rpm_limit: number
	tpm_limit: number
}

export const getProjectModelBinding = async (env: string) => {
  const response = await api.get<ProjectModelBinding>('/model-bindings', {
    params: { env },
    validateStatus: (status) => status === 200 || status === 404,
  })
  return response.status === 404 ? undefined : response.data
}

export const saveProjectModelBinding = async (body: Omit<ProjectModelBinding, 'workspace_id'>) =>
	(await api.put<ProjectModelBinding>('/model-bindings', body)).data

export const listModelProfiles = async () =>
  (await api.get<ModelProfile[]>('/model-profiles')).data ?? []

export const listModelProfileVersions = async () =>
  (await api.get<ModelProfileVersion[]>('/model-profile-versions')).data ?? []

export const createModelProfile = async (body: {
  name: string
  description?: string
  primary_deployment_id: string
  fallback_deployment_ids?: string[]
  classification_max?: string
}) => (await api.post('/model-profiles', body)).data

export const createModelProfileVersion = async (profileId: string, body: {
  primary_deployment_id: string
  fallback_deployment_ids?: string[]
  classification_max?: string
}) => (await api.post<ModelProfileVersion>(`/model-profiles/${profileId}/versions`, body)).data
