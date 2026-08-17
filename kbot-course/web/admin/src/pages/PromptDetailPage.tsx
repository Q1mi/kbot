import {
  Alert, Tabs, Card, Button, Space, Typography, Select, Table, Tag, Modal, Input, InputNumber, Result, Spin, message,
} from 'antd'
import { ArrowLeftOutlined, SaveOutlined } from '@ant-design/icons'
import { Component, useMemo, useState, type ComponentType, type ErrorInfo, type ReactNode } from 'react'
import { useLocation, useRoute } from 'wouter'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import ReactDiffViewerImport, { type ReactDiffViewerProps } from 'react-diff-viewer-continued'
import {
  listPrompts, listVersions, createVersion, promote, render as renderPrompt,
  startRollout, updateRolloutTraffic, completeRollout, rollbackRollout,
} from '@/api/prompt'
import type { PromptVersion } from '@/api/types'
import { CodeEditor } from '@/components/CodeEditor'
import { ModelProfileVersionSelect } from '@/components/ModelProfileVersionSelect'
import { fmtTime } from '@/lib/format'

const ENVS = ['dev', 'staging', 'prod']

// react-diff-viewer-continued 3.x publishes a double-wrapped CommonJS default
// export. Vite 8 keeps that wrapper in production builds, so a direct default
// import becomes a module object and React rejects it as an element type.
const ReactDiffViewer = (
  (ReactDiffViewerImport as unknown as { default?: ComponentType<ReactDiffViewerProps> }).default
  ?? ReactDiffViewerImport
) as ComponentType<ReactDiffViewerProps>

class DiffErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Prompt diff render failed', error, info)
  }

  render() {
    if (this.state.failed) {
      return <Alert type="error" showIcon message="版本对比渲染失败，请刷新页面后重试" />
    }
    return this.props.children
  }
}

export function PromptDetailPage() {
  const [, params] = useRoute('/prompts/:id')
  const id = params?.id ?? ''
  const [, navigate] = useLocation()
  const qc = useQueryClient()

  const promptsQ = useQuery({ queryKey: ['prompts'], queryFn: () => listPrompts() })
  const versionsQ = useQuery({ queryKey: ['prompt-versions', id], queryFn: () => listVersions(id) })
  const prompt = promptsQ.data?.find((p) => p.id === id)
  const versions = useMemo(
    () => [...(versionsQ.data ?? [])].sort((a, b) => b.version - a.version),
    [versionsQ.data],
  )
  const latest = versions[0]

  if (versionsQ.isLoading) return <Spin />
  if (versionsQ.isError) return <Result status="404" title="Prompt 不存在或无版本" />

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/prompts')}>
          返回
        </Button>
        <Typography.Title level={4} style={{ margin: 0 }}>
          {prompt?.name ?? 'Prompt'}
        </Typography.Title>
        {prompt?.category && <Tag>{prompt.category}</Tag>}
      </Space>

      <Tabs
        defaultActiveKey="editor"
        items={[
          { key: 'editor', label: 'Editor', children: <EditorTab promptId={id} latest={latest} onSaved={() => qc.invalidateQueries({ queryKey: ['prompt-versions', id] })} /> },
          { key: 'versions', label: `Versions (${versions.length})`, children: <VersionsTab promptId={id} versions={versions} /> },
          { key: 'diff', label: 'Diff', children: <DiffTab versions={versions} /> },
          { key: 'render', label: 'Render', children: <RenderTab promptId={id} /> },
          { key: 'rollout', label: 'Rollout', children: <RolloutTab promptId={id} versions={versions} /> },
        ]}
      />
    </div>
  )
}

// --- Editor:看最新版本(只读),或新建一版 ---
function EditorTab({ promptId, latest, onSaved }: { promptId: string; latest?: PromptVersion; onSaved: () => void }) {
  const [template, setTemplate] = useState(latest?.template ?? '')
  const [schema, setSchema] = useState(latest?.variables_schema ?? '{}')
  const [modelProfileVersionId, setModelProfileVersionId] = useState(latest?.model_profile_version_id)
  const [temperature, setTemperature] = useState<number | null>(latest?.generation_config?.temperature ?? null)
  const [topP, setTopP] = useState<number | null>(latest?.generation_config?.top_p ?? null)
  const [maxTokens, setMaxTokens] = useState<number | null>(latest?.generation_config?.max_output_tokens ?? null)
  const save = useMutation({
    mutationFn: () => createVersion(promptId, template, schema, modelProfileVersionId, {
      ...(temperature == null ? {} : { temperature }),
      ...(topP == null ? {} : { top_p: topP }),
      ...(maxTokens == null ? {} : { max_output_tokens: maxTokens }),
    }),
    onSuccess: (v) => {
      message.success(`已保存为 v${v.version}(token≈${v.token_estimate})`)
      onSaved()
    },
    onError: (error: Error) => message.error(error.message),
  })

  return (
    <Card>
      <Space style={{ marginBottom: 8, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Text type="secondary">
          当前最新:{latest ? `v${latest.version} · token≈${latest.token_estimate} · ${latest.hash.slice(0, 8)}` : '无版本'}
        </Typography.Text>
        <Button type="primary" icon={<SaveOutlined />} loading={save.isPending} onClick={() => save.mutate()}>
          保存为新版本
        </Button>
      </Space>
      <Typography.Text type="secondary">模板</Typography.Text>
      <CodeEditor value={template} onChange={setTemplate} language="handlebars" height={260} />
      <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
        变量定义(JSON Schema)
      </Typography.Text>
      <CodeEditor value={schema} onChange={setSchema} language="json" height={140} />
      <Typography.Title level={5} style={{ marginTop: 18 }}>模型与生成参数</Typography.Title>
      {!modelProfileVersionId && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="当前版本会使用全局兼容配置"
          description="全局配置默认指向火山方舟豆包；如需切换模型，请选择 Model Profile Version 后保存为新版本。"
        />
      )}
      <Space wrap>
        <ModelProfileVersionSelect
          allowClear
          placeholder="Model Profile Version（空则使用全局兼容配置）"
          style={{ width: 360 }}
          value={modelProfileVersionId}
          onChange={setModelProfileVersionId}
        />
        <InputNumber placeholder="temperature" min={0} max={2} step={0.1} value={temperature} onChange={setTemperature} />
        <InputNumber placeholder="top_p" min={0} max={1} step={0.1} value={topP} onChange={setTopP} />
        <InputNumber placeholder="max tokens" min={1} value={maxTokens} onChange={setMaxTokens} />
      </Space>
    </Card>
  )
}

// --- Versions:列表 + Promote(带二次确认 + 选 env)---
function VersionsTab({ promptId, versions }: { promptId: string; versions: PromptVersion[] }) {
  const [target, setTarget] = useState<{ v: PromptVersion; env: string } | null>(null)

  const doPromote = useMutation({
    mutationFn: () => promote(promptId, target!.env, target!.v.id),
    onSuccess: () => {
      message.success(`v${target!.v.version} 已晋升到 ${target!.env}`)
      setTarget(null)
    },
  })

  return (
    <>
      <Table
        rowKey="id"
        dataSource={versions}
        pagination={false}
        columns={[
          { title: '版本', dataIndex: 'version', render: (v: number) => <Tag color="blue">v{v}</Tag> },
          { title: 'Hash', dataIndex: 'hash', render: (v: string) => <code>{v?.slice(0, 12)}</code> },
          { title: 'Token 估算', dataIndex: 'token_estimate' },
          { title: 'Model Profile', dataIndex: 'model_profile_version_id', render: (v: string) => v ? <code>{v.slice(0, 8)}</code> : '全局回退' },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          {
            title: '晋升',
            render: (_, row: PromptVersion) => (
              <Space>
                {ENVS.map((env) => (
                  <Button key={env} size="small" onClick={() => setTarget({ v: row, env })}>
                    → {env}
                  </Button>
                ))}
              </Space>
            ),
          },
        ]}
      />
      <Modal
        title="确认晋升"
        open={!!target}
        onCancel={() => setTarget(null)}
        onOk={() => doPromote.mutate()}
        confirmLoading={doPromote.isPending}
        okText="确认晋升"
      >
        {target && (
          <Typography.Text>
            将 <Tag color="blue">v{target.v.version}</Tag> 绑定到环境{' '}
            <Tag color={target.env === 'prod' ? 'red' : 'green'}>{target.env}</Tag>。
            {target.env === 'prod' && <Typography.Paragraph type="danger">这是生产环境,请确认!</Typography.Paragraph>}
          </Typography.Text>
        )}
      </Modal>
    </>
  )
}

function RolloutTab({ promptId, versions }: { promptId: string; versions: PromptVersion[] }) {
  const [env, setEnv] = useState('prod')
  const [candidate, setCandidate] = useState(versions[0]?.id)
  const [traffic, setTraffic] = useState(5)
  const action = useMutation({
    mutationFn: async (kind: 'start' | 'update' | 'complete' | 'rollback') => {
      if (kind === 'start') return startRollout(promptId, env, candidate!, traffic)
      if (kind === 'update') return updateRolloutTraffic(promptId, env, traffic)
      if (kind === 'complete') return completeRollout(promptId, env)
      return rollbackRollout(promptId, env)
    },
    onSuccess: (_, kind) => message.success(
      kind === 'complete' ? '候选版本已成为新基线' : kind === 'rollback' ? '灰度已回滚' : '灰度配置已更新',
    ),
    onError: (e: Error) => message.error(e.message),
  })
  return (
    <Card>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="未命中灰度流量的用户继续使用环境基线；同一用户按实验 ID 确定性分桶。"
      />
      <Space wrap>
        <Select value={env} onChange={setEnv} options={ENVS.map((v) => ({ value: v, label: v }))} />
        <Select
          value={candidate}
          onChange={setCandidate}
          style={{ width: 160 }}
          options={versions.map((v) => ({ value: v.id, label: `候选 v${v.version}` }))}
        />
        <InputNumber min={1} max={99} value={traffic} onChange={(v) => setTraffic(v ?? 5)} addonAfter="%" />
        <Button type="primary" loading={action.isPending} onClick={() => action.mutate('start')}>开始灰度</Button>
        <Button onClick={() => action.mutate('update')}>调整流量</Button>
        <Button type="primary" onClick={() => action.mutate('complete')}>转全并成为基线</Button>
        <Button danger onClick={() => action.mutate('rollback')}>回滚</Button>
      </Space>
    </Card>
  )
}

// --- Diff:选两版,用 react-diff-viewer 可视化对比模板 ---
function DiffTab({ versions }: { versions: PromptVersion[] }) {
  const [fromId, setFromId] = useState(versions[1]?.id ?? versions[0]?.id)
  const [toId, setToId] = useState(versions[0]?.id)
  const from = versions.find((v) => v.id === fromId)
  const to = versions.find((v) => v.id === toId)
  const opts = versions.map((v) => ({ value: v.id, label: `v${v.version}` }))

  return (
    <Card>
      <Space style={{ marginBottom: 12 }}>
        <span>从</span>
        <Select style={{ width: 120 }} value={fromId} onChange={setFromId} options={opts} />
        <span>到</span>
        <Select style={{ width: 120 }} value={toId} onChange={setToId} options={opts} />
      </Space>
      {from && to ? (
        <DiffErrorBoundary key={`${from.id}-${to.id}`}>
          <ReactDiffViewer oldValue={from.template} newValue={to.template} splitView leftTitle={`v${from.version}`} rightTitle={`v${to.version}`} />
        </DiffErrorBoundary>
      ) : (
        <Typography.Text type="secondary">至少需要两个版本才能对比</Typography.Text>
      )}
    </Card>
  )
}

// --- Render:env + vars(JSON)→ 渲染结果 ---
function RenderTab({ promptId }: { promptId: string }) {
  const [env, setEnv] = useState('dev')
  const [vars, setVars] = useState('{\n  "question": "怎么退款?"\n}')
  const [result, setResult] = useState('')

  const run = useMutation({
    mutationFn: async () => {
      let parsed: Record<string, unknown> = {}
      try {
        parsed = JSON.parse(vars || '{}')
      } catch {
        throw new Error('变量 JSON 无效')
      }
      return renderPrompt(promptId, env, parsed)
    },
    onSuccess: setResult,
    onError: (e: Error) => message.error(e.message),
  })

  return (
    <Card>
      <Space style={{ marginBottom: 12 }}>
        <span>环境</span>
        <Select style={{ width: 140 }} value={env} onChange={setEnv} options={ENVS.map((e) => ({ value: e, label: e }))} />
        <Button type="primary" loading={run.isPending} onClick={() => run.mutate()}>
          渲染
        </Button>
      </Space>
      <Typography.Text type="secondary">变量(JSON)</Typography.Text>
      <CodeEditor value={vars} onChange={setVars} language="json" height={140} />
      <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
        渲染结果
      </Typography.Text>
      <Input.TextArea value={result} readOnly rows={8} placeholder="点「渲染」查看解析 env 指针 / A-B 后的最终文本" />
    </Card>
  )
}
