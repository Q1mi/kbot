import { Table, Button, Modal, Form, Input, Select, Switch, Space, Typography, Tag, Alert, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useLocation } from 'wouter'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listTools, createTool, type CreateToolRequest } from '@/api/tool'
import { CodeEditor } from '@/components/CodeEditor'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'

const SOURCE_TYPES = ['rest_api', 'mcp_server', 'internal_sdk', 'code_execution', 'a2a']
const DEFAULT_SCHEMA = '{\n  "type": "object",\n  "properties": {\n    "order_id": { "type": "string" }\n  },\n  "required": ["order_id"]\n}'
const ENDPOINT_TEMPLATES: Record<string, string> = {
  rest_api: '{\n  "url": "https://api.example.com/refund",\n  "method": "POST"\n}',
  mcp_server:
    '{\n  "transport": "streamable_http",\n  "url": "http://127.0.0.1:3000/mcp",\n  "tool_name": "get_weather",\n  "protocol_version": "2025-11-25"\n}',
  internal_sdk: '{\n  "sdk_name": "search_knowledge_base"\n}',
  code_execution: '{\n  "language": "python"\n}',
  a2a: '{\n  "card_url": "https://agent.example.com/.well-known/agent-card.json"\n}',
}

export function ToolsPage() {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [schema, setSchema] = useState(DEFAULT_SCHEMA)
  const [endpoint, setEndpoint] = useState(ENDPOINT_TEMPLATES.rest_api)
  const [auth, setAuth] = useState('{}')
  const [form] = Form.useForm<{ name: string; source_type: string; description: string; sensitive: boolean }>()

  const { data = [], isLoading } = useQuery({
    queryKey: ['tools', workspaceId],
    queryFn: () => listTools(),
    enabled: !!workspaceId,
  })

  const create = useMutation({
    mutationFn: (req: CreateToolRequest) => createTool(req),
    onSuccess: () => {
      message.success('已注册工具')
      setOpen(false)
      form.resetFields()
      setSchema(DEFAULT_SCHEMA)
      setEndpoint(ENDPOINT_TEMPLATES.rest_api)
      setAuth('{}')
      qc.invalidateQueries({ queryKey: ['tools'] })
    },
  })

  if (!workspaceId) return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          Tools
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          注册工具
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        pagination={{ pageSize: 10 }}
        columns={[
          {
            title: '名称',
            dataIndex: 'name',
            render: (v: string, row) => <a onClick={() => navigate(`/tools/${row.id}`)}>{v}</a>,
          },
          { title: '类型', dataIndex: 'source_type', render: (v: string) => <Tag>{v}</Tag> },
          {
            title: '敏感',
            dataIndex: 'sensitive',
            render: (v: boolean) => (v ? <Tag color="red">需审批</Tag> : <Tag>普通</Tag>),
          },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          { title: '操作', render: (_, row) => <a onClick={() => navigate(`/tools/${row.id}`)}>Sandbox / 发布</a> },
        ]}
      />

      <Modal
        title="注册工具"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        width={760}
        destroyOnClose
      >
        <Form
          form={form}
          layout="vertical"
          initialValues={{ source_type: 'rest_api', sensitive: false }}
          onValuesChange={(changed) => {
            if (typeof changed.source_type === 'string') {
              setEndpoint(ENDPOINT_TEMPLATES[changed.source_type] ?? '{}')
            }
          }}
          onFinish={(v) =>
            create.mutate({
              ...v,
              schema_json: schema,
              endpoint_config: endpoint,
              auth_config: auth,
            })
          }
        >
          <Space style={{ width: '100%' }} size="large" align="end">
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} style={{ flex: 1, minWidth: 220 }}>
              <Input placeholder="如:refund_api" />
            </Form.Item>
            <Form.Item name="source_type" label="类型">
              <Select style={{ width: 160 }} options={SOURCE_TYPES.map((t) => ({ value: t, label: t }))} />
            </Form.Item>
            <Form.Item name="sensitive" label="敏感(需审批)" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Typography.Text type="secondary">params_schema(JSON Schema)</Typography.Text>
          <CodeEditor value={schema} onChange={setSchema} language="json" height={160} />
          <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
            endpoint_config(JSON)
          </Typography.Text>
          <CodeEditor value={endpoint} onChange={setEndpoint} language="json" height={120} />
          <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
            auth_config(JSON，可配置 header/value 或 headers 对象)
          </Typography.Text>
          <CodeEditor value={auth} onChange={setAuth} language="json" height={100} />
        </Form>
      </Modal>
    </div>
  )
}
