import { AxiosError } from 'axios'
import { api } from './client'
import type { EvalCase, EvalDataset, EvalRunHistory, EvalRunResult } from './types'

export async function listDatasets(): Promise<EvalDataset[]> {
  const { data } = await api.get<EvalDataset[]>('/eval/datasets')
  return data ?? []
}

export async function createDataset(name: string, targetKind: string): Promise<EvalDataset> {
  const { data } = await api.post<EvalDataset>('/eval/datasets', { name, target_kind: targetKind })
  return data
}

export async function addCase(datasetId: string, input: string, expected: string, conversationId?: string): Promise<unknown> {
  const { data } = await api.post(`/eval/datasets/${datasetId}/cases`, {
    input, expected, conversation_id: conversationId,
  })
  return data
}

export interface RunEvalRequest {
  dataset_id: string
  agent_id: string
  agent_version_id?: string
  judge_method: string
  judge_agent_id?: string
  judge_agent_version_id?: string
  threshold: number
}

export async function listEvalCases(datasetId: string): Promise<EvalCase[]> {
  const { data } = await api.get<EvalCase[]>(`/eval/datasets/${datasetId}/cases`)
  return data ?? []
}

export async function listEvalRuns(datasetId: string): Promise<EvalRunHistory[]> {
  const { data } = await api.get<EvalRunHistory[]>(`/eval/datasets/${datasetId}/runs`)
  return data ?? []
}

// 跑评估门禁。后端在未达阈值时返回 422 + 合法结果体(表示业务门禁未通过);
// 这里放宽 validateStatus,把 422 也当成正常结果返回。
export async function runEval(req: RunEvalRequest): Promise<EvalRunResult> {
  try {
    const { data } = await api.post<EvalRunResult>('/eval/runs', req, {
      validateStatus: (s) => s === 200 || s === 422,
    })
    return data
  } catch (e) {
    const err = e as AxiosError<EvalRunResult>
    if (err.response?.status === 422 && err.response.data) return err.response.data
    throw e
  }
}
