import { Table, Button, Modal, Form, Input, InputNumber, Space, Typography, Tag, Alert, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useLocation } from 'wouter'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listPrompts, createPrompt, type CreatePromptRequest } from '@/api/prompt'
import { CodeEditor } from '@/components/CodeEditor'
import { ModelProfileVersionSelect } from '@/components/ModelProfileVersionSelect'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'

export function PromptsPage() {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [template, setTemplate] = useState('你是一个有帮助的助手。\n\n用户问题：{{.question}}')
  const [schema, setSchema] = useState('{\n  "question": "string"\n}')
  const [form] = Form.useForm<{
    name: string
    category: string
    model_profile_version_id?: string
    temperature?: number
    top_p?: number
    max_output_tokens?: number
  }>()

  const { data = [], isLoading } = useQuery({
    queryKey: ['prompts', workspaceId],
    queryFn: () => listPrompts(),
    enabled: !!workspaceId,
  })

  const create = useMutation({
    mutationFn: (req: CreatePromptRequest) => createPrompt(req),
    onSuccess: ({ prompt, version }) => {
      message.success(`已创建 Prompt v${version.version}，模型路由已固化`)
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['prompts'] })
      navigate(`/prompts/${prompt.id}`)
    },
    onError: (error: Error) => message.error(error.message),
  })

  if (!workspaceId) return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          Prompts
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建 Prompt
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
            render: (v: string, row) => <a onClick={() => navigate(`/prompts/${row.id}`)}>{v}</a>,
          },
          { title: '分类', dataIndex: 'category', render: (v: string) => (v ? <Tag>{v}</Tag> : '-') },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          { title: '操作', render: (_, row) => <a onClick={() => navigate(`/prompts/${row.id}`)}>详情 / 版本</a> },
        ]}
      />

      <Modal
        title="新建 Prompt"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        width={860}
        destroyOnClose
      >
        <Form
          form={form}
          layout="vertical"
          initialValues={{ category: 'general' }}
          onFinish={({ temperature, top_p, max_output_tokens, ...values }) => create.mutate({
            ...values,
            template,
            variables_schema: schema,
            generation_config: {
              ...(temperature == null ? {} : { temperature }),
              ...(top_p == null ? {} : { top_p }),
              ...(max_output_tokens == null ? {} : { max_output_tokens }),
            },
          })}
        >
          <Space style={{ width: '100%' }} size="large">
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} style={{ flex: 1, minWidth: 240 }}>
              <Input placeholder="如:客服回复模板" />
            </Form.Item>
            <Form.Item name="category" label="分类">
              <Input placeholder="general" />
            </Form.Item>
          </Space>
          <Typography.Text type="secondary">模板（Go template，变量写法 {'{{.变量名}}'}）</Typography.Text>
          <CodeEditor value={template} onChange={setTemplate} language="handlebars" height={200} />
          <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
            变量定义(JSON Schema)
          </Typography.Text>
          <CodeEditor value={schema} onChange={setSchema} language="json" height={140} />
          <Typography.Title level={5} style={{ marginTop: 18 }}>模型路由</Typography.Title>
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message="Model Profile Version 会固化到 Prompt v1"
            description="留空时使用全局兼容配置；课堂全局配置默认指向火山方舟豆包。"
          />
          <Form.Item name="model_profile_version_id" label="Model Profile Version">
            <ModelProfileVersionSelect style={{ width: '100%' }} />
          </Form.Item>
          <Space wrap align="start">
            <Form.Item name="temperature" label="temperature">
              <InputNumber min={0} max={2} step={0.1} placeholder="Provider 默认值" />
            </Form.Item>
            <Form.Item name="top_p" label="top_p">
              <InputNumber min={0} max={1} step={0.1} placeholder="Provider 默认值" />
            </Form.Item>
            <Form.Item name="max_output_tokens" label="max tokens">
              <InputNumber min={1} placeholder="Provider 默认值" />
            </Form.Item>
          </Space>
        </Form>
      </Modal>
    </div>
  )
}
