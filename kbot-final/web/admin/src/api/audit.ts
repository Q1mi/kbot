import { api } from './client'
import type { AuditLog, Approval } from './types'

export interface AuditFilter {
  conversation_id?: string
  actor?: string
  limit?: number
}

export async function listAuditLogs(filter: AuditFilter): Promise<AuditLog[]> {
  const { data } = await api.get<AuditLog[]>('/audit/logs', { params: filter })
  return data ?? []
}

export async function listInjectionLogs(): Promise<AuditLog[]> {
  const { data } = await api.get<AuditLog[]>('/guard/injection-logs')
  return data ?? []
}

export interface ExportResult {
  url: string
  key: string
  count: number
}

// 导出某会话审计轨迹为 CSV → 对象存储 → 返回预签名下载 URL(未配对象存储时后端 503)。
export async function exportAudit(conversationId: string): Promise<ExportResult> {
  const { data } = await api.post<ExportResult>('/audit/exports', { conversation_id: conversationId, format: 'csv' })
  return data
}

// ---- 审批队列 ----
export async function listApprovals(): Promise<Approval[]> {
  const { data } = await api.get<Approval[]>('/approvals')
  return data ?? []
}

export async function approveApproval(id: string): Promise<void> {
  await api.post(`/approvals/${id}/approve`)
}

export async function rejectApproval(id: string): Promise<void> {
  await api.post(`/approvals/${id}/reject`)
}
