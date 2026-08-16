import { Table, Button, Modal, Form, Input, Space, Typography, Tag, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listUsers } from '@/api/iam'
import { register, type CreateUserRequest } from '@/api/auth'
import { fmtTime } from '@/lib/format'

export function UsersPage() {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateUserRequest>()

  const { data = [], isLoading } = useQuery({ queryKey: ['users'], queryFn: () => listUsers() })

  const create = useMutation({
    mutationFn: (req: CreateUserRequest) => register(req),
    onSuccess: () => {
      message.success('已创建用户')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          用户
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建用户
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: '姓名', dataIndex: 'name', render: (v) => v || '-' },
          { title: '邮箱', dataIndex: 'email' },
          {
            title: '状态',
            dataIndex: 'status',
            render: (v: string) => <Tag color={v === 'active' ? 'green' : 'default'}>{v || '-'}</Tag>,
          },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
        ]}
      />

      <Modal
        title="新建用户"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={(v) => create.mutate(v)}>
          <Form.Item name="name" label="姓名" rules={[{ required: true, message: '请输入姓名' }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="email"
            label="邮箱"
            rules={[{ required: true }, { type: 'email', message: '邮箱格式不对' }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, min: 8, message: '至少 8 位' }]}>
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
