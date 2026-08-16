import { Table, Button, Modal, Form, Input, Space, Typography, Tag, Alert, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useLocation } from 'wouter'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listKBs, createKB, type CreateKBRequest } from '@/api/kb'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'

const STATUS_COLOR: Record<string, string> = { active: 'green', indexing: 'blue', error: 'red' }

export function KBsPage() {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateKBRequest>()

  const { data = [], isLoading } = useQuery({
    queryKey: ['kbs', workspaceId],
    queryFn: () => listKBs(),
    enabled: !!workspaceId,
  })

  const create = useMutation({
    mutationFn: (req: CreateKBRequest) => createKB(req),
    onSuccess: () => {
      message.success('已创建知识库')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['kbs'] })
    },
  })

  if (!workspaceId) return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          知识库
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建知识库
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
            render: (v: string, row) => <a onClick={() => navigate(`/kbs/${row.id}`)}>{v}</a>,
          },
          { title: '嵌入模型', dataIndex: 'embedding_model', render: (v) => v || '-' },
          {
            title: '状态',
            dataIndex: 'status',
            render: (v: string) => <Tag color={STATUS_COLOR[v] ?? 'default'}>{v || '-'}</Tag>,
          },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          { title: '操作', render: (_, row) => <a onClick={() => navigate(`/kbs/${row.id}`)}>文档 / 检索</a> },
        ]}
      />

      <Modal
        title="新建知识库"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        destroyOnClose
      >
        <Form
          form={form}
          layout="vertical"
          initialValues={{ embedding_model: 'local' }}
          onFinish={(v) => create.mutate(v)}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如:售后知识库" />
          </Form.Item>
          <Form.Item name="embedding_model" label="嵌入模型">
            <Input placeholder="local / text-embedding-3-small" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
