export const A2UI_VERSION = 'v0.9'
export const A2UI_BASIC_CATALOG = 'https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json'

export type DynamicValue = string | number | boolean | unknown[] | { path: string }

export interface A2UIActionDefinition {
  event?: {
    name: 'approval.approve' | 'approval.reject'
    context?: Record<string, DynamicValue>
  }
}

export interface A2UIComponent {
  id: string
  component: 'Text' | 'Card' | 'Column' | 'Row' | 'Button' | 'Divider'
  text?: DynamicValue
  variant?: string
  children?: string[]
  child?: string
  justify?: string
  align?: string
  axis?: string
  action?: A2UIActionDefinition
}

export interface A2UIMessage {
  version: string
  createSurface?: {
    surfaceId: string
    catalogId: string
    theme?: Record<string, unknown>
    sendDataModel?: boolean
  }
  updateComponents?: { surfaceId: string; components: A2UIComponent[] }
  updateDataModel?: { surfaceId: string; path?: string; value?: unknown }
  deleteSurface?: { surfaceId: string }
}

export interface A2UISurfaceModel {
  surfaceId: string
  catalogId: string
  sendDataModel: boolean
  components: Record<string, A2UIComponent>
  dataModel: Record<string, unknown>
}

export type A2UISurfaces = Record<string, A2UISurfaceModel>

const allowedComponents = new Set(['Text', 'Card', 'Column', 'Row', 'Button', 'Divider'])
const allowedActions = new Set(['approval.approve', 'approval.reject'])
const blockedPathParts = new Set(['__proto__', 'prototype', 'constructor'])

function assertMessage(message: A2UIMessage) {
  if (message.version !== A2UI_VERSION) throw new Error(`不支持的 A2UI 版本: ${message.version}`)
  const envelopes = [message.createSurface, message.updateComponents, message.updateDataModel, message.deleteSurface]
  if (envelopes.filter(Boolean).length !== 1) throw new Error('A2UI 消息必须且只能包含一个 envelope')
}

function validateComponents(components: A2UIComponent[]) {
  if (components.length === 0 || components.length > 64) throw new Error('A2UI 组件数量越界')
  for (const component of components) {
    if (!component.id || !allowedComponents.has(component.component)) {
      throw new Error(`A2UI 组件未在受信目录中: ${component.component}`)
    }
    const actionName = component.action?.event?.name
    if (actionName && !allowedActions.has(actionName)) throw new Error(`A2UI 动作未授权: ${actionName}`)
  }
}

function decodePointer(path: string): string[] {
  if (path === '' || path === '/') return []
  if (!path.startsWith('/')) throw new Error('A2UI data path 必须是 JSON Pointer')
  const parts = path
    .slice(1)
    .split('/')
    .map((part) => part.replace(/~1/g, '/').replace(/~0/g, '~'))
  if (parts.some((part) => blockedPathParts.has(part))) throw new Error('A2UI data path 包含禁止字段')
  return parts
}

function updateAtPath(current: Record<string, unknown>, path: string, value: unknown): Record<string, unknown> {
  const parts = decodePointer(path)
  if (parts.length === 0) {
    if (value === undefined || value === null) return {}
    if (typeof value !== 'object' || Array.isArray(value)) throw new Error('根 data model 必须是对象')
    return { ...(value as Record<string, unknown>) }
  }
  const root = { ...current }
  let cursor: Record<string, unknown> = root
  parts.forEach((part, index) => {
    if (index === parts.length - 1) {
      if (value === undefined || value === null) delete cursor[part]
      else cursor[part] = value
      return
    }
    const child = cursor[part]
    const next = typeof child === 'object' && child !== null && !Array.isArray(child)
      ? { ...(child as Record<string, unknown>) }
      : {}
    cursor[part] = next
    cursor = next
  })
  return root
}

export function applyA2UIMessage(surfaces: A2UISurfaces, message: A2UIMessage): A2UISurfaces {
  assertMessage(message)
  if (message.createSurface) {
    const payload = message.createSurface
    if (payload.catalogId !== A2UI_BASIC_CATALOG) throw new Error(`A2UI catalog 未授权: ${payload.catalogId}`)
    if (surfaces[payload.surfaceId]) throw new Error(`A2UI surface 已存在: ${payload.surfaceId}`)
    return {
      ...surfaces,
      [payload.surfaceId]: {
        surfaceId: payload.surfaceId,
        catalogId: payload.catalogId,
        sendDataModel: !!payload.sendDataModel,
        components: {},
        dataModel: {},
      },
    }
  }
  if (message.deleteSurface) {
    const next = { ...surfaces }
    delete next[message.deleteSurface.surfaceId]
    return next
  }

  const payload = message.updateComponents ?? message.updateDataModel
  if (!payload) return surfaces
  const current = surfaces[payload.surfaceId]
  if (!current) throw new Error(`A2UI surface 尚未创建: ${payload.surfaceId}`)

  if (message.updateComponents) {
    validateComponents(message.updateComponents.components)
    const components = { ...current.components }
    message.updateComponents.components.forEach((component) => { components[component.id] = component })
    return { ...surfaces, [current.surfaceId]: { ...current, components } }
  }
  const update = message.updateDataModel!
  return {
    ...surfaces,
    [current.surfaceId]: {
      ...current,
      dataModel: updateAtPath(current.dataModel, update.path ?? '/', update.value),
    },
  }
}

export function updateA2UISurfaceData(
  surfaces: A2UISurfaces,
  surfaceId: string,
  values: Record<string, unknown>,
): A2UISurfaces {
  return Object.entries(values).reduce(
    (current, [key, value]) => applyA2UIMessage(current, {
      version: A2UI_VERSION,
      updateDataModel: { surfaceId, path: `/${key}`, value },
    }),
    surfaces,
  )
}

export function resolveDynamic(value: DynamicValue | undefined, dataModel: Record<string, unknown>): unknown {
  if (value && typeof value === 'object' && !Array.isArray(value) && 'path' in value) {
    const parts = decodePointer(String(value.path))
    return parts.reduce<unknown>((cursor, part) => {
      if (typeof cursor !== 'object' || cursor === null) return undefined
      return (cursor as Record<string, unknown>)[part]
    }, dataModel)
  }
  return value
}
