import { Card, Button, Space, Typography, Descriptions, Tag, Result, Spin, Statistic, Row, Col, Alert, message, Table, Modal, Form, Input } from 'antd'
import { ArrowLeftOutlined, PlayCircleOutlined, CloudUploadOutlined, PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useLocation, useRoute } from 'wouter'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { createToolVersion, listTools, listToolVersions, publishToolVersion, testTool } from '@/api/tool'
import type { ToolTestRun, ToolVersion } from '@/api/types'
import { CodeEditor } from '@/components/CodeEditor'
import { fmtTime } from '@/lib/format'

export function ToolDetailPage() {
  const [, params] = useRoute('/tools/:id')
  const id = params?.id ?? ''
  const [, navigate] = useLocation()
  const qc = useQueryClient()
  const [input, setInput] = useState('{\n  "order_id": "ord_123"\n}')
  const [run, setRun] = useState<ToolTestRun | null>(null)
  const [versionOpen, setVersionOpen] = useState(false)
  const [versionForm] = Form.useForm<Pick<ToolVersion, 'schema_json' | 'endpoint_config' | 'auth_config' | 'retry_policy'>>()

  const toolsQ = useQuery({ queryKey: ['tools'], queryFn: () => listTools() })
  const versionsQ = useQuery({ queryKey: ['tool-versions', id], queryFn: () => listToolVersions(id), enabled: !!id })
  const tool = toolsQ.data?.find((t) => t.id === id)
  const latest = versionsQ.data?.[0]

  const test = useMutation({
    mutationFn: async () => {
      let parsed: unknown
      try {
        parsed = JSON.parse(input || '{}')
      } catch {
        throw new Error('输入 JSON 无效')
      }
      return testTool(id, parsed)
    },
    onSuccess: (result) => {
      setRun(result)
      qc.invalidateQueries({ queryKey: ['tool-versions', id] })
    },
    onError: (e: Error) => message.error(e.message),
  })

  const createVersion = useMutation({
    mutationFn: (body: Pick<ToolVersion, 'schema_json' | 'endpoint_config' | 'auth_config' | 'retry_policy'>) =>
      createToolVersion(id, body),
    onSuccess: () => {
      message.success('已创建新的草稿版本')
      setVersionOpen(false)
      qc.invalidateQueries({ queryKey: ['tool-versions', id] })
    },
    onError: (e: Error) => message.error(e.message),
  })

  const pub = useMutation({
    mutationFn: (versionId: string) => publishToolVersion(id, versionId),
    onSuccess: () => {
      message.success('工具版本已发布')
      qc.invalidateQueries({ queryKey: ['tool-versions', id] })
    },
    onError: (e: Error) => message.error(e.message),
  })

  const openVersionEditor = () => {
    versionForm.setFieldsValue({
      schema_json: latest?.schema_json ?? '{}',
      endpoint_config: latest?.endpoint_config ?? '{}',
      auth_config: latest?.auth_config ?? '{}',
      retry_policy: latest?.retry_policy ?? '{"max_attempts":3,"backoff_ms":1000}',
    })
    setVersionOpen(true)
  }

  if (toolsQ.isLoading) return <Spin />
  if (!tool) return <Result status="404" title="工具不存在" />

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/tools')}>
          返回
        </Button>
        <Typography.Title level={4} style={{ margin: 0 }}>
          {tool.name}
        </Typography.Title>
        <Tag>{tool.source_type}</Tag>
        {tool.sensitive && <Tag color="red">敏感 · 需审批</Tag>}
      </Space>

      <Card style={{ marginBottom: 16 }} extra={<Button icon={<PlusOutlined />} onClick={openVersionEditor}>新建版本</Button>}>
        <Descriptions column={2} size="small">
          <Descriptions.Item label="ID"><Typography.Text copyable>{tool.id}</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="描述">{tool.description || '-'}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{fmtTime(tool.created_at)}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="版本治理" style={{ marginBottom: 16 }}>
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message="新版本创建后自动成为 Sandbox 的试调目标；发布门禁只认该版本自己的成功试调记录。"
        />
        <Table
          rowKey="id"
          size="small"
          loading={versionsQ.isLoading}
          dataSource={versionsQ.data}
          pagination={false}
          columns={[
            { title: '版本', dataIndex: 'version', render: (v) => <Tag color="blue">v{v}</Tag> },
            { title: '状态', dataIndex: 'status', render: (v) => <Tag color={v === 'published' ? 'green' : 'gold'}>{v}</Tag> },
            { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
            {
              title: '操作',
              render: (_, row: ToolVersion) => row.status === 'draft' ? (
                <Button
                  size="small"
                  type="primary"
                  icon={<CloudUploadOutlined />}
                  loading={pub.isPending && pub.variables === row.id}
                  disabled={row.id !== latest?.id}
                  onClick={() => pub.mutate(row.id)}
                >
                  发布
                </Button>
              ) : null,
            },
          ]}
        />
      </Card>

      <Card title="Sandbox 测试场">
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message={`填入 JSON 入参后真实调用并落账。当前试调目标：${latest ? `v${latest.version}` : '加载中'}。`}
        />
        <Typography.Text type="secondary">输入(JSON)</Typography.Text>
        <CodeEditor value={input} onChange={setInput} language="json" height={160} />
        <Button
          type="primary"
          icon={<PlayCircleOutlined />}
          style={{ marginTop: 12 }}
          loading={test.isPending}
          onClick={() => test.mutate()}
        >
          调用
        </Button>

        {run && (
          <Card size="small" style={{ marginTop: 16 }} title="结果">
            <Row gutter={16} style={{ marginBottom: 12 }}>
              <Col>
                <Statistic title="状态" value={run.status} valueStyle={{ color: run.status === 'success' ? '#3f8600' : '#cf1322' }} />
              </Col>
              <Col>
                <Statistic title="延迟" value={run.latency_ms} suffix="ms" />
              </Col>
            </Row>
            <Typography.Text type="secondary">输出</Typography.Text>
            <pre style={{ background: '#f5f5f5', padding: 12, borderRadius: 6, whiteSpace: 'pre-wrap', maxHeight: 240, overflow: 'auto' }}>
              {run.output || '(空)'}
            </pre>
            {run.error && <Typography.Text type="danger">错误:{run.error}</Typography.Text>}
          </Card>
        )}
      </Card>


      <Modal
        title="新建 Tool 版本"
        open={versionOpen}
        onCancel={() => setVersionOpen(false)}
        onOk={() => versionForm.submit()}
        confirmLoading={createVersion.isPending}
        width={760}
        destroyOnClose
      >
        <Form form={versionForm} layout="vertical" onFinish={(v) => createVersion.mutate(v)}>
          <Alert style={{ marginBottom: 12 }} type="info" showIcon message="表单已复制最新版本配置，可直接修改并保存为新的不可变草稿。" />
          {(['schema_json', 'endpoint_config', 'auth_config', 'retry_policy'] as const).map((name) => (
            <Form.Item key={name} name={name} label={name} rules={[{ required: true }]}>
              <Input.TextArea rows={name === 'schema_json' ? 8 : 5} style={{ fontFamily: 'monospace' }} />
            </Form.Item>
          ))}
        </Form>
      </Modal>
    </div>
  )
}
