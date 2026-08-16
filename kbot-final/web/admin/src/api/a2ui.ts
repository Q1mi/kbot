import { api } from './client'
import type { A2UIMessage, A2UIActionDefinition, A2UISurfaceModel, DynamicValue } from '@/a2ui/protocol'
import { A2UI_VERSION, resolveDynamic } from '@/a2ui/protocol'

export interface A2UIActionRequest {
  version: typeof A2UI_VERSION
  action: {
    name: string
    surfaceId: string
    sourceComponentId: string
    timestamp: string
    context: Record<string, unknown>
  }
}

function resolveContext(
  context: Record<string, DynamicValue> | undefined,
  surface: A2UISurfaceModel,
): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(context ?? {}).map(([key, value]) => [key, resolveDynamic(value, surface.dataModel)]),
  )
}

export async function sendA2UIAction(
  conversationId: string,
  surface: A2UISurfaceModel,
  sourceComponentId: string,
  action: NonNullable<A2UIActionDefinition['event']>,
): Promise<A2UIMessage[]> {
  const payload: A2UIActionRequest = {
    version: A2UI_VERSION,
    action: {
      name: action.name,
      surfaceId: surface.surfaceId,
      sourceComponentId,
      timestamp: new Date().toISOString(),
      context: resolveContext(action.context, surface),
    },
  }
  const response = await api.post<string>(`/conversations/${conversationId}/a2ui/actions`, payload, {
    headers: { 'Content-Type': 'application/a2ui+json', Accept: 'application/a2ui+json' },
    responseType: 'text',
  })
  const body = typeof response.data === 'string' ? response.data : JSON.stringify(response.data)
  return body
    .split(/\r?\n/)
    .filter((line) => line.trim())
    .map((line) => JSON.parse(line) as A2UIMessage)
}
