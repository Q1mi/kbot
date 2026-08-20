import { Table, Button, Modal, Form, Input, Select, Space, Typography, Tag, Alert, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useLocation } from 'wouter'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listAgents, createAgent } from '@/api/agent'
import type { CreateAgentRequest } from '@/api/types'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'
import { AgentConfigFields } from '@/components/AgentConfigFields'

const TEMPLATES = ['customer_support', 'internal_copilot', 'data_analyst', 'custom']

export function AgentsPage() {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateAgentRequest>()

  const { data = [], isLoading } = useQuery({
    queryKey: ['agents', workspaceId],
    queryFn: () => listAgents(),
    enabled: !!workspaceId,
  })
  const create = useMutation({
    mutationFn: (req: CreateAgentRequest) => createAgent(req),
    onSuccess: () => {
      message.success('已创建 Agent')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['agents'] })
    },
  })

  if (!workspaceId) {
    return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />
  }

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          Agents
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建 Agent
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
            render: (v: string, row) => <a onClick={() => navigate(`/agents/${row.id}`)}>{v}</a>,
          },
          { title: '模板', dataIndex: 'template', render: (v: string) => <Tag>{v}</Tag> },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          {
            title: '操作',
            render: (_, row) => (
              <Space>
                <a onClick={() => navigate(`/agents/${row.id}`)}>详情</a>
                <a onClick={() => navigate('/conversations')}>测试</a>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title="新建 Agent"
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
          initialValues={{
            template: 'customer_support', max_steps: 8, prompt_env: 'dev',
            tool_ids: [], skill_version_ids: [], kb_ids: [], allow_network: false,
          }}
          onFinish={(v) => create.mutate(v)}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如:退款助手" />
          </Form.Item>
          <Form.Item name="template" label="模板" rules={[{ required: true }]}>
            <Select options={TEMPLATES.map((t) => ({ value: t, label: t }))} />
          </Form.Item>
          <AgentConfigFields />
        </Form>
      </Modal>
    </div>
  )
}
