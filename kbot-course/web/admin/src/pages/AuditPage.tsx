import { Table, Form, Input, InputNumber, Button, Space, Typography, Tag, Drawer, Descriptions, message } from 'antd'
import { SearchOutlined, ExportOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { listAuditLogs, exportAudit, type AuditFilter } from '@/api/audit'
import type { AuditLog } from '@/api/types'
import { fmtTime } from '@/lib/format'

export function AuditPage() {
  const [filter, setFilter] = useState<AuditFilter>({ limit: 100 })
  const [detail, setDetail] = useState<AuditLog | null>(null)
  const [form] = Form.useForm<AuditFilter>()

  const { data = [], isLoading } = useQuery({
    queryKey: ['audit-logs', filter],
    queryFn: () => listAuditLogs(filter),
  })

  const exportM = useMutation({
    mutationFn: (conversationId: string) => exportAudit(conversationId),
    onSuccess: (r) => {
      message.success(`已导出 ${r.count} 条`)
      window.open(r.url, '_blank')
    },
  })

  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        审计日志
      </Typography.Title>

      <Form
        form={form}
        layout="inline"
        initialValues={{ limit: 100 }}
        style={{ marginBottom: 16 }}
        onFinish={(v) => setFilter(v)}
      >
        <Form.Item name="conversation_id" label="会话 ID">
          <Input allowClear placeholder="conversation_id" style={{ width: 220 }} />
        </Form.Item>
        <Form.Item name="actor" label="操作者">
          <Input allowClear placeholder="user_id / system" style={{ width: 160 }} />
        </Form.Item>
        <Form.Item name="limit" label="条数">
          <InputNumber min={1} max={1000} />
        </Form.Item>
        <Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" icon={<SearchOutlined />}>
              查询
            </Button>
            <Button
              icon={<ExportOutlined />}
              disabled={!filter.conversation_id}
              loading={exportM.isPending}
              onClick={() => filter.conversation_id && exportM.mutate(filter.conversation_id)}
            >
              导出该会话 CSV
            </Button>
          </Space>
        </Form.Item>
      </Form>

      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        pagination={{ pageSize: 15 }}
        onRow={(row) => ({ onClick: () => setDetail(row), style: { cursor: 'pointer' } })}
        columns={[
          { title: '操作者', dataIndex: 'actor', render: (v) => v || 'system' },
          { title: '动作', dataIndex: 'action', render: (v: string) => <Tag>{v}</Tag> },
          { title: '资源类型', dataIndex: 'resource_type', render: (v: string) => v || '-' },
          { title: '资源 ID', dataIndex: 'resource_id', ellipsis: true },
          { title: '时间', dataIndex: 'created_at', render: fmtTime },
        ]}
      />

      <Drawer title="审计详情" width={560} open={!!detail} onClose={() => setDetail(null)}>
        {detail && (
          <Descriptions column={1} bordered size="small">
            <Descriptions.Item label="ID">{detail.id}</Descriptions.Item>
            <Descriptions.Item label="操作者">{detail.actor || 'system'}</Descriptions.Item>
            <Descriptions.Item label="动作">{detail.action}</Descriptions.Item>
            <Descriptions.Item label="资源">{detail.resource_type} / {detail.resource_id}</Descriptions.Item>
            <Descriptions.Item label="IP">{detail.ip || '-'}</Descriptions.Item>
            <Descriptions.Item label="时间">{fmtTime(detail.created_at)}</Descriptions.Item>
            <Descriptions.Item label="Before">
              <pre style={preStyle}>{pretty(detail.before_json)}</pre>
            </Descriptions.Item>
            <Descriptions.Item label="After">
              <pre style={preStyle}>{pretty(detail.after_json)}</pre>
            </Descriptions.Item>
          </Descriptions>
        )}
      </Drawer>
    </div>
  )
}

const preStyle: React.CSSProperties = {
  background: '#f5f5f5',
  padding: 8,
  borderRadius: 4,
  whiteSpace: 'pre-wrap',
  maxHeight: 240,
  overflow: 'auto',
  margin: 0,
}

function pretty(s?: string | null): string {
  if (!s) return '-'
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}
