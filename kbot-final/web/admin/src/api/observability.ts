import { api } from './client'

export interface ObservabilityConfig {
  metrics_url: string
  healthz_url: string
  readyz_url: string
  otlp_endpoint: string
  traces_enabled: boolean
  langfuse_ui_url?: string
  langfuse_project_id?: string
}

export async function getObservability(): Promise<ObservabilityConfig> {
  const { data } = await api.get<ObservabilityConfig>('/observability')
  return data
}
