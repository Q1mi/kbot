import { api } from './client'
import type { KnowledgeBase, KbDocument, KbIngestJob, ConnectorInstance, Passage } from './types'

export async function listKBs(): Promise<KnowledgeBase[]> {
  const { data } = await api.get<KnowledgeBase[]>('/kbs')
  return data ?? []
}

export interface CreateKBRequest {
  name: string
  embedding_model: string
}

export async function createKB(req: CreateKBRequest): Promise<KnowledgeBase> {
  const { data } = await api.post<KnowledgeBase>('/kbs', req)
  return data
}

export interface SyncResult {
  listed: number
  ingested: number
  skipped: number
}

// 触发本地 Markdown 文件夹同步并 ingest。
export async function syncMarkdown(kbId: string, rootPath: string): Promise<SyncResult> {
  const { data } = await api.post<SyncResult>(`/kbs/${kbId}/connectors/markdown/sync`, { root_path: rootPath })
  return data
}

export async function listDocuments(kbId: string): Promise<KbDocument[]> {
  const { data } = await api.get<KbDocument[]>(`/kbs/${kbId}/documents`)
  return data ?? []
}

export async function listConnectors(kbId: string): Promise<ConnectorInstance[]> {
  const { data } = await api.get<ConnectorInstance[]>(`/kbs/${kbId}/connectors`)
  return data ?? []
}

export async function listJobs(kbId: string): Promise<KbIngestJob[]> {
  const { data } = await api.get<KbIngestJob[]>(`/kbs/${kbId}/jobs`)
  return data ?? []
}

export type SearchMode = 'bm25' | 'vector' | 'hybrid'

// 检索 Playground:mode = bm25 / vector / hybrid。
export async function search(kbId: string, query: string, mode: SearchMode, topK = 5): Promise<Passage[]> {
  const { data } = await api.post<Passage[]>(`/kbs/${kbId}/search`, { query, mode, top_k: topK })
  return data ?? []
}
