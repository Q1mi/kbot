import { Table, Button, Modal, Form, Input, Space, Typography, Tag, Alert, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useLocation } from 'wouter'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listSkills, createSkill } from '@/api/skill'
import { SkillEditor, joinSkillMD } from '@/components/SkillEditor'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'

const DEFAULT_FM = 'name: refund-order\ndescription: 处理退款申请和退款进度查询\nallowed-tools:\n  - refund_order\nallowed-kbs: []\ndisable-model-invocation: false\nrequires_network: false'
const DEFAULT_BODY = '# 退款流程\n\n1. 校验订单状态与必要参数\n2. 调用 `refund_order`\n3. 根据工具真实返回通知用户'

export function SkillsPage() {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [fm, setFm] = useState(DEFAULT_FM)
  const [body, setBody] = useState(DEFAULT_BODY)
  const [form] = Form.useForm<{ category: string }>()

  const { data = [], isLoading } = useQuery({
    queryKey: ['skills', workspaceId],
    queryFn: () => listSkills(),
    enabled: !!workspaceId,
  })

  const create = useMutation({
    mutationFn: (category: string) => createSkill({ category, skill_md: joinSkillMD(fm, body) }),
    onSuccess: ({ skill }) => {
      message.success('已创建 Skill')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['skills'] })
      navigate(`/skills/${skill.id}`)
    },
  })

  if (!workspaceId) return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          Skills
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建 Skill
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
            render: (v: string, row) => <a onClick={() => navigate(`/skills/${row.id}`)}>{v}</a>,
          },
          { title: '分类', dataIndex: 'category', render: (v: string) => (v ? <Tag>{v}</Tag> : '-') },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          { title: '操作', render: (_, row) => <a onClick={() => navigate(`/skills/${row.id}`)}>详情 / 版本</a> },
        ]}
      />

      <Modal
        title="新建 Skill(SKILL.md)"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        width={920}
        destroyOnClose
      >
        <Form form={form} layout="vertical" initialValues={{ category: 'general' }} onFinish={(v) => create.mutate(v.category)}>
          <Form.Item name="category" label="分类" style={{ maxWidth: 240 }}>
            <Input placeholder="general" />
          </Form.Item>
          <SkillEditor frontmatter={fm} body={body} onFrontmatter={setFm} onBody={setBody} />
        </Form>
      </Modal>
    </div>
  )
}
