import { Table, Button, Modal, Form, Input, Space, Typography, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listWorkspaces, createWorkspace, type CreateWorkspaceRequest } from '@/api/iam'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'

export function WorkspacesPage() {
  const qc = useQueryClient()
  const setWorkspace = useAuthStore((s) => s.setWorkspace)
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateWorkspaceRequest>()

  const { data = [], isLoading } = useQuery({ queryKey: ['workspaces'], queryFn: () => listWorkspaces() })

  const create = useMutation({
    mutationFn: (req: CreateWorkspaceRequest) => createWorkspace(req),
    onSuccess: (ws) => {
      message.success('已创建工作空间')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['workspaces'] })
      setWorkspace(ws.id) // 新建后顺手切过去
    },
  })

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          工作空间
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '描述', dataIndex: 'description', render: (v) => v || '-' },
          { title: 'ID', dataIndex: 'id', render: (v: string) => <Typography.Text copyable>{v}</Typography.Text> },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
        ]}
      />

      <Modal
        title="新建工作空间"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={(v) => create.mutate(v)}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如:客服部" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
