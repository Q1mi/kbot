import { Tabs, Table, Typography, Tag, Alert, Button, Space, List, Popconfirm, Empty, message, Modal, Form, Input, Select, Switch, InputNumber, Card, Progress } from 'antd'
import { SyncOutlined, CheckOutlined, CloseOutlined, PlusOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { listInjectionLogs, listApprovals, approveApproval, rejectApproval } from '@/api/audit'
import { fmtTime } from '@/lib/format'
import {
  createGuardRule, listGuardQuotas, listGuardRules, setGuardQuota, updateGuardRule, type GuardRuleInput,
} from '@/api/guard'
import type { GuardRule } from '@/api/types'
import { useAuthStore } from '@/store/authStore'

export function GuardPage() {
  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        护栏
      </Typography.Title>
      <Tabs
        defaultActiveKey="injections"
        items={[
          { key: 'injections', label: '注入日志', children: <InjectionLogsTab /> },
          { key: 'approvals', label: '审批队列', children: <ApprovalsTab /> },
          { key: 'rules', label: '规则', children: <RulesTab /> },
          { key: 'quotas', label: '配额', children: <QuotasTab /> },
        ]}
      />
    </div>
  )
}

function InjectionLogsTab() {
  const { data = [], isLoading, refetch } = useQuery({ queryKey: ['injection-logs'], queryFn: () => listInjectionLogs() })
  return (
    <Table
      rowKey="id"
      loading={isLoading}
      dataSource={data}
      pagination={{ pageSize: 10 }}
      title={() => <Button size="small" icon={<SyncOutlined />} onClick={() => refetch()}>刷新</Button>}
      columns={[
        { title: '动作', dataIndex: 'action', render: (v: string) => <Tag color="orange">{v}</Tag> },
        { title: '主体', dataIndex: 'actor' },
        { title: '资源', dataIndex: 'resource_id', ellipsis: true },
        { title: '详情', dataIndex: 'after_json', ellipsis: true, render: (v?: string) => v || '-' },
        { title: '时间', dataIndex: 'created_at', render: fmtTime },
      ]}
    />
  )
}

function ApprovalsTab() {
  const qc = useQueryClient()
  const { data = [], isLoading, refetch } = useQuery({ queryKey: ['approvals'], queryFn: () => listApprovals() })

  const approve = useMutation({
    mutationFn: (id: string) => approveApproval(id),
    onSuccess: () => {
      message.success('已批准,触发续跑')
      qc.invalidateQueries({ queryKey: ['approvals'] })
    },
  })
  const reject = useMutation({
    mutationFn: (id: string) => rejectApproval(id),
    onSuccess: () => {
      message.success('已否决')
      qc.invalidateQueries({ queryKey: ['approvals'] })
    },
  })

  return (
    <>
      <Space style={{ marginBottom: 12 }}>
        <Button size="small" icon={<SyncOutlined />} onClick={() => refetch()}>刷新</Button>
        <Typography.Text type="secondary">待审批 {data.length} 条</Typography.Text>
      </Space>
      {data.length === 0 ? (
        <Empty description="没有待审批项" />
      ) : (
        <List
          loading={isLoading}
          bordered
          dataSource={data}
          renderItem={(a) => (
            <List.Item
              actions={[
                <Popconfirm key="ap" title="批准并续跑?" onConfirm={() => approve.mutate(a.id)}>
                  <Button type="primary" size="small" icon={<CheckOutlined />} loading={approve.isPending}>批准</Button>
                </Popconfirm>,
                <Popconfirm key="rj" title="否决?" onConfirm={() => reject.mutate(a.id)}>
                  <Button danger size="small" icon={<CloseOutlined />} loading={reject.isPending}>否决</Button>
                </Popconfirm>,
              ]}
            >
              <List.Item.Meta
                title={<Space><Tag color="red">{a.action}</Tag><Typography.Text type="secondary">{fmtTime(a.created_at)}</Typography.Text></Space>}
                description={
                  <Space direction="vertical" size={2}>
                    <Typography.Text type="secondary">会话:{a.conversation_id || '-'}</Typography.Text>
                    <Typography.Text code style={{ whiteSpace: 'pre-wrap' }}>{a.payload}</Typography.Text>
                  </Space>
                }
              />
            </List.Item>
          )}
        />
      )}
    </>
  )
}

function RulesTab() {
  const qc = useQueryClient()
  const workspaceId = useAuthStore((state) => state.workspaceId)
  const [editing, setEditing] = useState<GuardRule | 'new'>()
  const [form] = Form.useForm<GuardRuleInput>()
  const query = useQuery({ queryKey: ['guard-rules', workspaceId], queryFn: listGuardRules, enabled: !!workspaceId })
  const save = useMutation({
    mutationFn: (body: GuardRuleInput) => editing === 'new'
      ? createGuardRule(body)
      : updateGuardRule(editing!.id, body),
    onSuccess: () => {
      message.success('Guard 规则已保存并立即作用于新请求')
      setEditing(undefined)
      qc.invalidateQueries({ queryKey: ['guard-rules', workspaceId] })
    },
    onError: (error: Error) => message.error(error.message),
  })

  function openEditor(rule?: GuardRule) {
    setEditing(rule ?? 'new')
    form.setFieldsValue(rule ?? {
      kind: 'pattern', hook: 'on_input', pattern_or_model: '', action: 'block', enabled: true,
    })
  }

  const toggle = useMutation({
    mutationFn: ({ rule, enabled }: { rule: GuardRule; enabled: boolean }) => updateGuardRule(rule.id, { ...rule, enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['guard-rules', workspaceId] }),
  })

  return (
    <>
      <Alert type="info" showIcon style={{ marginBottom: 12 }} message="数据库规则按工作空间加载，正则会在保存时校验；运行时存储故障采用 fail-open 并保留内置安全规则。" />
      <Table
        rowKey="id"
        loading={query.isLoading}
        dataSource={query.data}
        title={() => <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新增规则</Button>}
        columns={[
          { title: '类型', dataIndex: 'kind', render: (v) => <Tag>{v}</Tag> },
          { title: 'Hook', dataIndex: 'hook', render: (v) => <Tag color="blue">{v}</Tag> },
          { title: '正则', dataIndex: 'pattern_or_model', ellipsis: true },
          { title: '动作', dataIndex: 'action', render: (v) => <Tag color={v === 'block' ? 'red' : v === 'redact' ? 'purple' : 'gold'}>{v}</Tag> },
          { title: '启用', dataIndex: 'enabled', render: (enabled, rule: GuardRule) => <Switch checked={enabled} loading={toggle.isPending} onChange={(value) => toggle.mutate({ rule, enabled: value })} /> },
          { title: '操作', render: (_, rule: GuardRule) => <Button type="link" onClick={() => openEditor(rule)}>编辑</Button> },
        ]}
      />
      <Modal title={editing === 'new' ? '新增 Guard 规则' : '编辑 Guard 规则'} open={!!editing} onCancel={() => setEditing(undefined)} onOk={() => form.submit()} confirmLoading={save.isPending}>
        <Form form={form} layout="vertical" onFinish={(value) => save.mutate(value)}>
          <Form.Item name="kind" label="规则类型" rules={[{ required: true }]}><Input placeholder="如 injection / pii / business_policy" /></Form.Item>
          <Form.Item name="hook" label="运行 Hook" rules={[{ required: true }]}>
            <Select options={['on_input', 'on_output', 'on_tool_call', 'on_llm_call'].map((value) => ({ value, label: value }))} />
          </Form.Item>
          <Form.Item name="pattern_or_model" label="Go 正则" rules={[{ required: true }]}><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="action" label="动作" rules={[{ required: true }]}>
            <Select options={['block', 'warn', 'redact'].map((value) => ({ value, label: value }))} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Modal>
    </>
  )
}

function QuotasTab() {
  const qc = useQueryClient()
  const workspaceId = useAuthStore((state) => state.workspaceId)
  const [form] = Form.useForm<{ limit: number }>()
  const query = useQuery({ queryKey: ['guard-quotas', workspaceId], queryFn: listGuardQuotas, enabled: !!workspaceId })
  const quota = query.data?.find((item) => item.metric === 'llm_calls')
  useEffect(() => {
    form.setFieldsValue({ limit: quota?.limit ?? 0 })
  }, [form, quota?.limit])
  const save = useMutation({
    mutationFn: (value: { limit: number }) => setGuardQuota('llm_calls', value.limit),
    onSuccess: () => {
      message.success('月度 LLM 调用配额已更新')
      qc.invalidateQueries({ queryKey: ['guard-quotas', workspaceId] })
    },
  })
  const percent = quota?.limit ? Math.min(100, Math.round(quota.used / quota.limit * 100)) : 0
  return (
    <Card loading={query.isLoading} title="工作空间月度 LLM 调用配额">
      <Space direction="vertical" size="large" style={{ width: '100%' }}>
        <Alert type="info" showIcon message="limit=0 表示不限额；每次模型调用前执行原子扣减，超额时 Guard 会阻断请求。" />
        <Space size="large">
          <Progress type="dashboard" percent={percent} format={() => quota?.limit ? `${quota.used}/${quota.limit}` : `${quota?.used ?? 0}/∞`} />
          <Form form={form} layout="vertical" initialValues={{ limit: quota?.limit ?? 0 }} onFinish={(value) => save.mutate(value)}>
            <Form.Item name="limit" label={`周期 ${quota?.period ?? '当前月份'}`} rules={[{ required: true }]}>
              <InputNumber min={0} step={100} style={{ width: 240 }} />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={save.isPending}>保存配额</Button>
          </Form>
        </Space>
      </Space>
    </Card>
  )
}
