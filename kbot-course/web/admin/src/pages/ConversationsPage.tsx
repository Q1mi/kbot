import { Alert, Avatar, Button, Card, Empty, Input, List, Popover, Radio, Select, Space, Tag, Tooltip, Typography, message } from 'antd'
import {
  FileTextOutlined,
  InfoCircleOutlined,
  LinkOutlined,
  PlusOutlined,
  ReloadOutlined,
  RobotOutlined,
  RocketOutlined,
  SafetyCertificateOutlined,
  SendOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getConversation,
  getUserPromptInputSpec,
  listAgents,
  listConversations,
  streamChat,
  type AgentStreamEvent,
  type ConversationDetail,
  type RunFinished,
  type RunStarted,
  type UserPromptSubmission,
} from '@/api/agent'
import { getObservability } from '@/api/observability'
import { sendA2UIAction } from '@/api/a2ui'
import { A2UIRenderer } from '@/components/A2UIRenderer'
import {
  applyA2UIMessage,
  updateA2UISurfaceData,
  type A2UIActionDefinition,
  type A2UIMessage,
  type A2UISurfaceModel,
  type A2UISurfaces,
} from '@/a2ui/protocol'
import { useAuthStore } from '@/store/authStore'
import './ConversationsPage.css'

interface TurnItem {
  kind: 'turn'
  id: string
  role: 'user' | 'assistant'
  content: string
}

interface SurfaceItem {
  kind: 'surface'
  id: string
  surfaceId: string
}

type ConversationItem = TurnItem | SurfaceItem

interface PromptSchemaProperty {
  type?: string
  title?: string
  description?: string
  enum?: string[]
  default?: unknown
}

interface PromptVariablesSchema {
  required: string[]
  properties: Record<string, PromptSchemaProperty>
}

function parseVariablesSchema(raw?: string): PromptVariablesSchema {
  if (!raw) return { required: [], properties: {} }
  try {
    const value = JSON.parse(raw) as { required?: string[]; properties?: Record<string, PromptSchemaProperty> }
    return { required: value.required ?? [], properties: value.properties ?? {} }
  } catch {
    return { required: [], properties: {} }
  }
}

function defaultVariables(schema: PromptVariablesSchema): Record<string, unknown> {
  return Object.fromEntries(Object.entries(schema.properties).map(([name, property]) => (
    [name, property.default ?? property.enum?.[0] ?? '']
  )))
}

const promptFieldLabels: Record<string, string> = {
  market: '目标市场',
  order_id: '跨境订单号',
  sku: '重点 SKU',
  objective: '运营目标',
  constraints: '业务约束',
  task_type: '任务类型',
  primary_resource_id: '主资源标识',
  claim_id: '理赔案件号',
  review_goal: '审核目标',
  operator_instruction: '操作员要求',
  additional_context: '补充背景',
  analysis_goal: '分析目标',
  investigation_scope: '调查范围',
  execution_mode: '执行策略',
}

const executionModeCopy: Record<string, { title: string; description: string }> = {
  analyze_only: { title: '仅分析', description: '查询事实并输出建议，不执行写操作' },
  prepare_action: { title: '准备操作', description: '生成操作参数，并提交人工审批' },
  execute_after_approval: { title: '审批后执行', description: '审批通过后执行受控业务操作' },
}

function isNarrativeField(name: string) {
  return /(objective|goal|constraints|context|instruction|scope)/i.test(name)
}

function fieldLabel(name: string, property: PromptSchemaProperty) {
  return property.title?.trim() || promptFieldLabels[name] || name
}

function taskSceneName(name?: string) {
  return name?.replace(/\s*·\s*User Prompt Template$/i, '').trim() || '业务'
}

interface PromptFieldProps {
  name: string
  property: PromptSchemaProperty
  required: boolean
  value: unknown
  multiline?: boolean
  onChange: (value: unknown) => void
}

function PromptField({ name, property, required, value, multiline, onChange }: PromptFieldProps) {
  const label = fieldLabel(name, property)
  return (
    <label className="task-field">
      <span className="task-field-label">
        <span>{label}{required && <span className="task-required">*</span>}</span>
        <Tooltip title={(
          <span>
            {property.description || label}
            <br />模板变量：{name}
          </span>
        )}>
          <InfoCircleOutlined className="task-field-help" />
        </Tooltip>
      </span>
      {property.enum?.length ? (
        <Select
          size="large"
          value={typeof value === 'string' ? value : undefined}
          options={property.enum.map((item) => ({ value: item, label: item }))}
          placeholder={property.description}
          onChange={onChange}
        />
      ) : multiline ? (
        <Input.TextArea
          rows={3}
          value={String(value ?? '')}
          placeholder={property.description}
          onChange={(event) => onChange(event.target.value)}
        />
      ) : (
        <Input
          size="large"
          value={String(value ?? '')}
          placeholder={property.description}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
    </label>
  )
}

interface RuntimeEvent {
  id: string
  type: string
  label: string
}

const eventColors: Record<string, string> = {
  started: 'blue',
  tool_call: 'gold',
  tool_result: 'green',
  skill_trigger: 'purple',
  await_approval: 'orange',
  'approval.approve': 'blue',
  'approval.reject': 'red',
  resume_completed: 'green',
  done: 'default',
}

export function ConversationsPage() {
  const queryClient = useQueryClient()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [agentId, setAgentId] = useState<string>()
  const [agentEnv, setAgentEnv] = useState('dev')
  const [conversationId, setConversationId] = useState<string>()
  const [pinnedVersionId, setPinnedVersionId] = useState<string>()
  const [latestTraceId, setLatestTraceId] = useState<string>()
  const [input, setInput] = useState('')
  const [items, setItems] = useState<ConversationItem[]>([])
  const [events, setEvents] = useState<RuntimeEvent[]>([])
  const [surfaces, setSurfaces] = useState<A2UISurfaces>({})
  const [actionLoading, setActionLoading] = useState<string>()
  const [sending, setSending] = useState(false)

  const { data: agents = [] } = useQuery({
    queryKey: ['agents', workspaceId],
    queryFn: () => listAgents(),
    enabled: !!workspaceId,
  })
  const historyQ = useQuery({
    queryKey: ['conversations', workspaceId],
    queryFn: () => listConversations(),
    enabled: !!workspaceId,
  })
  const userPromptSpecQ = useQuery({
    queryKey: ['agent-user-prompt-input', agentId, agentEnv],
    queryFn: () => getUserPromptInputSpec(agentId!, agentEnv),
    enabled: !!workspaceId && !!agentId && !conversationId,
  })
  const userPromptSchema = useMemo(
    () => parseVariablesSchema(userPromptSpecQ.data?.variables_schema),
    [userPromptSpecQ.data?.variables_schema],
  )
  const [userPromptVariables, setUserPromptVariables] = useState<Record<string, unknown>>({})
  useEffect(() => {
    setUserPromptVariables(defaultVariables(userPromptSchema))
  }, [userPromptSchema])
  useEffect(() => {
    setAgentId(undefined)
    setConversationId(undefined)
    setPinnedVersionId(undefined)
    setLatestTraceId(undefined)
    setInput('')
    setItems([])
    setEvents([])
    setSurfaces({})
    setUserPromptVariables({})
  }, [workspaceId])
  const promptFields = useMemo(
    () => Object.entries(userPromptSchema.properties),
    [userPromptSchema.properties],
  )
  const basicPromptFields = promptFields.filter(([name]) => name !== 'execution_mode' && !isNarrativeField(name))
  const narrativePromptFields = promptFields.filter(([name]) => name !== 'execution_mode' && isNarrativeField(name))
  const executionModeField = promptFields.find(([name]) => name === 'execution_mode')
  const { data: observability } = useQuery({
    queryKey: ['observability'],
    queryFn: getObservability,
  })

  const traceURL = latestTraceId && observability?.langfuse_ui_url && observability.langfuse_project_id
    ? `${observability.langfuse_ui_url.replace(/\/$/, '')}/project/${encodeURIComponent(observability.langfuse_project_id)}/traces/${latestTraceId}`
    : undefined
  const activeApproval = Object.values(surfaces).find((surface) => {
    const status = String(surface.dataModel.status ?? '')
    return status === 'pending' || status === 'approved'
  })
  const latestEvent = events[events.length - 1]
  const taskComposerVisible = !conversationId && !!userPromptSpecQ.data?.enabled

  function resetConversation() {
    setConversationId(undefined)
    setPinnedVersionId(undefined)
    setLatestTraceId(undefined)
    setItems([])
    setEvents([])
    setSurfaces({})
    setUserPromptVariables(defaultVariables(userPromptSchema))
  }

  function addRuntimeEvent(type: string, label: string) {
    setEvents((current) => [...current, { id: `${Date.now()}-${current.length}`, type, label }])
  }

  function handleStreamEvent(event: AgentStreamEvent, assistantTurnId: string, userTurnId: string) {
    switch (event.type) {
      case 'started': {
        const started = event.data as RunStarted
        setConversationId(started.conversation_id)
        setPinnedVersionId(started.agent_version_id)
        setLatestTraceId(started.trace_id)
        if (started.user_message) {
          setItems((current) => current.map((item) => item.kind === 'turn' && item.id === userTurnId
            ? { ...item, content: started.user_message! }
            : item))
        }
        addRuntimeEvent(event.type, `会话 ${started.conversation_id.slice(0, 8)}`)
        break
      }
      case 'answer_delta':
        setItems((current) => {
          const exists = current.some((item) => item.kind === 'turn' && item.id === assistantTurnId)
          return exists
            ? current.map((item) => item.kind === 'turn' && item.id === assistantTurnId
              ? { ...item, content: item.content + (event.text ?? '') }
              : item)
            : [...current, { kind: 'turn', id: assistantTurnId, role: 'assistant', content: event.text ?? '' }]
        })
        break
      case 'tool_call':
        addRuntimeEvent(event.type, `调用工具 ${event.text ?? ''}`)
        break
      case 'tool_result':
        addRuntimeEvent(event.type, `工具 ${String(event.data ?? '')} 已返回`)
        break
      case 'skill_trigger':
        addRuntimeEvent(event.type, `触发技能 ${event.text ?? ''}`)
        break
      case 'await_approval':
        addRuntimeEvent(event.type, `等待审批 ${event.text?.slice(0, 8) ?? ''}`)
        break
      case 'a2ui': {
        const a2uiMessage = event.data as A2UIMessage
        setSurfaces((current) => applyA2UIMessage(current, a2uiMessage))
        if (a2uiMessage.createSurface) {
          const surfaceId = a2uiMessage.createSurface.surfaceId
          setItems((current) => current.some((item) => item.kind === 'surface' && item.surfaceId === surfaceId)
            ? current
            : [...current, { kind: 'surface', id: `surface-${surfaceId}`, surfaceId }])
        }
        break
      }
      case 'error':
        throw new Error(event.text || 'Agent 执行失败')
      case 'done': {
        const finished = event.data as RunFinished | undefined
        addRuntimeEvent(
          event.type,
          finished?.status === 'awaiting_approval' ? '运行已暂停，等待审批' : '运行完成',
        )
        break
      }
    }
  }

  async function send() {
    if (!agentId) {
      message.warning('请先选择一个 Agent')
      return
    }
    const text = input.trim()
    const userPromptSpec = !conversationId && userPromptSpecQ.data?.enabled ? userPromptSpecQ.data : undefined
    if (!text && !userPromptSpec) return
    if (activeApproval) {
      message.warning('当前会话正在等待审批，请处理后再继续对话')
      return
    }
    let userPrompt: UserPromptSubmission | undefined
    if (userPromptSpec) {
      const missing = userPromptSchema.required.filter((name) => {
        const value = userPromptVariables[name]
        return value == null || (typeof value === 'string' && value.trim() === '')
      })
      if (missing.length > 0) {
        message.warning(`请填写必填项：${missing.map((name) => (
          fieldLabel(name, userPromptSchema.properties[name] ?? {})
        )).join('、')}`)
        return
      }
      userPrompt = {
        versionId: userPromptSpec.prompt_version_id!,
        variables: userPromptVariables,
      }
    }
    const turnKey = `${Date.now()}`
    const userTurnId = `user-${turnKey}`
    const assistantTurnId = `assistant-${turnKey}`
    setItems((current) => [...current, {
      kind: 'turn', id: userTurnId, role: 'user',
      content: userPrompt ? `正在使用「${userPromptSpec?.prompt_name}」生成标准任务…` : text,
    }])
    setInput('')
    setSending(true)
    try {
      await streamChat(
        agentId, text, conversationId, agentEnv,
        (event) => handleStreamEvent(event, assistantTurnId, userTurnId), userPrompt,
      )
    } catch (error) {
      message.error(error instanceof Error ? error.message : '流式对话失败')
    } finally {
      setSending(false)
      queryClient.invalidateQueries({ queryKey: ['conversations', workspaceId] })
    }
  }

  async function restoreConversation(id: string) {
    try {
      const detail = await getConversation(id)
      setAgentId(detail.conversation.agent_id)
      setConversationId(detail.conversation.id)
      setPinnedVersionId(detail.conversation.agent_version_id)
      setLatestTraceId(undefined)
      setEvents([])
      setSurfaces({})
      setItems(detail.messages
        .filter((item) => item.role === 'user' || item.role === 'assistant')
        .map((item, index) => ({
          kind: 'turn' as const,
          id: item.id ?? `history-${index}`,
          role: item.role as 'user' | 'assistant',
          content: item.content,
        })))
      message.success('已恢复历史会话，可继续多轮对话')
    } catch (error) {
      message.error(error instanceof Error ? error.message : '读取历史会话失败')
    }
  }

  async function waitForResume(id: string, before: ConversationDetail, surfaceId: string) {
    const knownMessageIDs = new Set(before.messages.map((item) => item.id).filter(Boolean))
    for (let attempt = 0; attempt < 45; attempt += 1) {
      const detail = await getConversation(id)
      if (detail.messages.length > before.messages.length) {
        const assistant = [...detail.messages].reverse().find((item) => (
          item.role === 'assistant' && (!item.id || !knownMessageIDs.has(item.id))
        ))
        if (assistant) {
          const itemId = `resume-${assistant.id ?? Date.now()}`
          setItems((current) => current.some((item) => item.id === itemId) ? current : [...current, {
            kind: 'turn', id: itemId, role: 'assistant', content: assistant.content,
          }])
          setSurfaces((current) => updateA2UISurfaceData(current, surfaceId, {
            status: 'completed',
            status_label: '操作执行完成',
            result_label: 'Agent 已完成业务操作并返回最终结果',
          }))
          addRuntimeEvent('resume_completed', '审批续跑完成')
          return
        }
      }
      await new Promise((resolve) => window.setTimeout(resolve, 1000))
    }
    throw new Error('审批已提交，等待 Agent 续跑超时')
  }

  async function handleA2UIAction(
    surface: A2UISurfaceModel,
    componentId: string,
    action: NonNullable<A2UIActionDefinition['event']>,
  ) {
    if (!conversationId) return
    setActionLoading(surface.surfaceId)
    try {
      const before = action.name === 'approval.approve' ? await getConversation(conversationId) : undefined
      const responses = await sendA2UIAction(conversationId, surface, componentId, action)
      setSurfaces((current) => responses.reduce(applyA2UIMessage, current))
      addRuntimeEvent(action.name, action.name === 'approval.approve' ? '审批已批准' : '审批已拒绝')
      if (action.name === 'approval.approve' && before) await waitForResume(conversationId, before, surface.surfaceId)
    } catch (error) {
      message.error(error instanceof Error ? error.message : 'A2UI 动作执行失败')
    } finally {
      setActionLoading(undefined)
    }
  }

  if (!workspaceId) {
    return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />
  }

  return (
    <div className="conversation-page-layout">
      <aside className="conversation-history-panel">
        <header className="conversation-history-header">
          <div>
            <Typography.Title level={5}>历史会话</Typography.Title>
            <Typography.Text type="secondary">当前工作空间最近 50 条</Typography.Text>
          </div>
          <Tooltip title="刷新列表">
            <Button
              type="text"
              size="small"
              icon={<ReloadOutlined />}
              loading={historyQ.isFetching}
              onClick={() => historyQ.refetch()}
            />
          </Tooltip>
        </header>

        <Button
          className="conversation-new-button"
          type="primary"
          block
          icon={<PlusOutlined />}
          disabled={!agentId}
          onClick={resetConversation}
        >
          新建会话
        </Button>

        <div className="conversation-history-list">
          <List
            size="small"
            loading={historyQ.isLoading}
            dataSource={historyQ.data}
            locale={{ emptyText: '还没有会话记录' }}
            renderItem={(conversation) => {
              const agent = agents.find((item) => item.id === conversation.agent_id)
              const when = conversation.updated_at ?? conversation.started_at
              const selected = conversation.id === conversationId
              return (
                <List.Item
                  className={`conversation-history-item${selected ? ' is-active' : ''}`}
                  role="button"
                  tabIndex={0}
                  aria-current={selected ? 'true' : undefined}
                  onClick={() => restoreConversation(conversation.id)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') restoreConversation(conversation.id)
                  }}
                >
                  <div className="conversation-history-content">
                    <div className="conversation-history-title">
                      <span className="conversation-history-agent">
                        <RobotOutlined />
                        <Typography.Text ellipsis title={agent?.name ?? conversation.agent_id}>
                          {agent?.name ?? `Agent ${conversation.agent_id.slice(0, 8)}`}
                        </Typography.Text>
                      </span>
                      <Tag color={selected ? 'blue' : undefined}>{conversation.status}</Tag>
                    </div>
                    <Typography.Text className="conversation-history-id" type="secondary">
                      #{conversation.id.slice(0, 8)}
                    </Typography.Text>
                    {when && (
                      <Typography.Text className="conversation-history-time" type="secondary">
                        {new Date(when).toLocaleString()}
                      </Typography.Text>
                    )}
                  </div>
                </List.Item>
              )
            }}
          />
        </div>
      </aside>

      <main className="conversation-main-panel">
      <Space style={{ marginBottom: 16 }} wrap>
        <Typography.Text strong>会话 Playground</Typography.Text>
        <Tag>Admin 内部演示</Tag>
        <Select
          style={{ minWidth: 240 }}
          placeholder="选择 Agent"
          value={agentId}
          onChange={(value) => { setAgentId(value); resetConversation() }}
          options={agents.map((agent) => ({ value: agent.id, label: `${agent.name} (${agent.template})` }))}
        />
        <Select
          style={{ width: 120 }}
          value={agentEnv}
          disabled={!!conversationId}
          onChange={(value) => { setAgentEnv(value); resetConversation() }}
          options={['dev', 'staging', 'prod'].map((value) => ({ value, label: value }))}
        />
        <Button onClick={resetConversation} disabled={items.length === 0}>新会话</Button>
        {conversationId && <Tag color="blue">Conversation {conversationId.slice(0, 8)}</Tag>}
        {pinnedVersionId && <Tag color="purple">Agent Version {pinnedVersionId.slice(0, 8)}</Tag>}
        {latestTraceId && (
          traceURL
            ? <Button type="link" href={traceURL} target="_blank" icon={<LinkOutlined />}>Langfuse Trace {latestTraceId.slice(0, 8)}</Button>
            : <Tag>Trace {latestTraceId.slice(0, 8)}</Tag>
        )}
      </Space>

      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="这里用于演示 Agent 运行、人工审批和 Langfuse 链路；C 端页面通常只保留聊天与业务状态。"
      />

      {!conversationId && userPromptSpecQ.isError && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message="读取 Agent 的 User Prompt Template 失败" />
      )}
      {taskComposerVisible && (
        <section className="task-composer">
          <header className="task-composer-header">
            <span className="task-composer-icon"><FileTextOutlined /></span>
            <div className="task-composer-heading">
              <Typography.Title level={4}>{taskSceneName(userPromptSpecQ.data?.prompt_name)}任务</Typography.Title>
              <Typography.Text type="secondary">
                填写业务信息，系统会生成标准化首轮指令并保存完整运行快照。
              </Typography.Text>
            </div>
            <div className="task-composer-meta">
              <Tag color="purple">模板 v{userPromptSpecQ.data?.prompt_version}</Tag>
              <Tag>{userPromptSpecQ.data?.prompt_env}</Tag>
              <Tooltip title="实际模板版本、变量和渲染结果均由服务端记录">
                <span className="task-audit-note"><SafetyCertificateOutlined /> 可审计</span>
              </Tooltip>
            </div>
          </header>

          <div className="task-composer-body">
            {basicPromptFields.length > 0 && (
              <section className="task-form-section">
                <div className="task-section-heading">
                  <span className="task-section-index">1</span>
                  <span>
                    <strong>业务对象</strong>
                    <small>定位本次任务涉及的订单、案件或业务资源</small>
                  </span>
                </div>
                <div className="task-fields task-fields-basic">
                  {basicPromptFields.map(([name, property]) => (
                    <PromptField
                      key={name}
                      name={name}
                      property={property}
                      required={userPromptSchema.required.includes(name)}
                      value={userPromptVariables[name]}
                      onChange={(next) => setUserPromptVariables((current) => ({ ...current, [name]: next }))}
                    />
                  ))}
                </div>
              </section>
            )}

            {narrativePromptFields.length > 0 && (
              <section className="task-form-section">
                <div className="task-section-heading">
                  <span className="task-section-index">2</span>
                  <span>
                    <strong>任务要求</strong>
                    <small>描述目标、约束和需要重点关注的业务背景</small>
                  </span>
                </div>
                <div className="task-fields task-fields-narrative">
                  {narrativePromptFields.map(([name, property]) => (
                    <PromptField
                      key={name}
                      name={name}
                      property={property}
                      required={userPromptSchema.required.includes(name)}
                      value={userPromptVariables[name]}
                      multiline
                      onChange={(next) => setUserPromptVariables((current) => ({ ...current, [name]: next }))}
                    />
                  ))}
                </div>
              </section>
            )}

            {executionModeField && (
              <section className="task-form-section">
                <div className="task-section-heading">
                  <span className="task-section-index">3</span>
                  <span>
                    <strong>执行策略</strong>
                    <small>控制 Agent 可以推进到哪一个业务阶段</small>
                  </span>
                </div>
                <Radio.Group
                  className="task-execution-options"
                  value={String(userPromptVariables.execution_mode ?? '')}
                  onChange={(event) => setUserPromptVariables((current) => ({
                    ...current, execution_mode: event.target.value,
                  }))}
                >
                  {(executionModeField[1].enum ?? []).map((mode) => {
                    const copy = executionModeCopy[mode] ?? { title: mode, description: executionModeField[1].description ?? '' }
                    return (
                      <Radio.Button key={mode} value={mode}>
                        <strong>{copy.title}</strong>
                        <small>{copy.description}</small>
                      </Radio.Button>
                    )
                  })}
                </Radio.Group>
              </section>
            )}

            <footer className="task-submit-panel">
              <label className="task-supplement">
                <span className="task-field-label">
                  <span>补充说明 <em>可选</em></span>
                  <Tooltip title="这里适合填写临时优先级、偏好或本次任务的特殊要求">
                    <InfoCircleOutlined className="task-field-help" />
                  </Tooltip>
                </span>
                <Input.TextArea
                  autoSize={{ minRows: 2, maxRows: 4 }}
                  value={input}
                  placeholder="补充其他限制、优先级或需要 Agent 关注的信息"
                  disabled={sending}
                  onChange={(event) => setInput(event.target.value)}
                />
              </label>
              <div className="task-submit-actions">
                <Button
                  size="large"
                  icon={<ReloadOutlined />}
                  disabled={sending}
                  onClick={() => {
                    setUserPromptVariables(defaultVariables(userPromptSchema))
                    setInput('')
                  }}
                >
                  重置
                </Button>
                <Button
                  type="primary"
                  size="large"
                  icon={<RocketOutlined />}
                  loading={sending}
                  onClick={send}
                >
                  发起任务
                </Button>
              </div>
            </footer>
          </div>
        </section>
      )}

      {(!taskComposerVisible || items.length > 0) && <Card
        style={{ minHeight: 420, marginBottom: 12 }}
        extra={latestEvent && (
          <Space size={4}>
            <Tag color={eventColors[latestEvent.type]}>{latestEvent.label}</Tag>
            <Popover
              title="运行事件（按发生顺序）"
              trigger="click"
              content={(
                <Space direction="vertical" size={6} style={{ maxWidth: 420 }}>
                  {events.map((event) => <Tag key={event.id} color={eventColors[event.type]}>{event.label}</Tag>)}
                </Space>
              )}
            >
              <Button type="link" size="small">运行明细</Button>
            </Popover>
          </Space>
        )}
      >
        {items.length === 0 ? (
          <Empty description={userPromptSpecQ.data?.enabled ? '填写上方业务任务后发起对话' : '发一条消息开始多轮对话'} />
        ) : (
          <Space direction="vertical" style={{ width: '100%' }} size="middle">
            {items.map((item) => {
              if (item.kind === 'turn') {
                return (
                  <div key={item.id} style={{ display: 'flex', justifyContent: item.role === 'user' ? 'flex-end' : 'flex-start' }}>
                    <Space align="start" style={{ maxWidth: '78%' }}>
                      {item.role === 'assistant' && <Avatar icon={<RobotOutlined />} style={{ background: '#1677ff' }} />}
                      <div style={{
                        background: item.role === 'user' ? '#e6f4ff' : '#f5f5f5',
                        padding: '10px 14px', borderRadius: 10, whiteSpace: 'pre-wrap', lineHeight: 1.7,
                      }}>
                        {item.content || '…'}
                      </div>
                      {item.role === 'user' && <Avatar icon={<UserOutlined />} />}
                    </Space>
                  </div>
                )
              }
              const surface = surfaces[item.surfaceId]
              if (!surface) return null
              return (
                <div key={item.id} style={{ display: 'flex', alignItems: 'flex-start', gap: 12 }}>
                  <Avatar icon={<RobotOutlined />} style={{ flex: '0 0 auto', background: '#1677ff' }} />
                  <div style={{ width: 'min(520px, calc(100% - 52px))' }}>
                    <A2UIRenderer
                      surface={surface}
                      loading={actionLoading === surface.surfaceId}
                      onAction={(componentId, action) => handleA2UIAction(surface, componentId, action)}
                    />
                  </div>
                </div>
              )
            })}
          </Space>
        )}
      </Card>}

      {!taskComposerVisible && <Space.Compact style={{ width: '100%' }}>
        <Input
          value={input}
          onChange={(event) => setInput(event.target.value)}
          onPressEnter={send}
          placeholder={activeApproval ? '当前运行已暂停，请先处理审批' : '输入消息，回车发送'}
          disabled={sending || !!activeApproval}
        />
        <Button type="primary" icon={<SendOutlined />} loading={sending} disabled={!!activeApproval} onClick={send}>
          发送
        </Button>
      </Space.Compact>}

      </main>
    </div>
  )
}
