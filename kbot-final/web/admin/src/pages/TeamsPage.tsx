import {
  Table, Button, Modal, Form, Input, Select, Space, Typography, Tag, Alert, Card, Divider, message,
} from 'antd'
import { PlusOutlined, MinusCircleOutlined, PlayCircleOutlined, BranchesOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  listTeams, createTeam, runTeam, listTeamVersions, createTeamVersion, promoteTeamVersion,
  type TeamRunResult,
} from '@/api/team'
import { listAgents } from '@/api/agent'
import type { CreateTeamRequest, Team, TeamMember } from '@/api/types'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'

export function TeamsPage() {
  const qc = useQueryClient()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [runOpen, setRunOpen] = useState<Team | null>(null)
  const [versionOpen, setVersionOpen] = useState<Team | null>(null)
  const [runResult, setRunResult] = useState<TeamRunResult | null>(null)
  const [form] = Form.useForm<CreateTeamRequest>()
  const [runForm] = Form.useForm<{ input: string; env: string }>()
  const [versionForm] = Form.useForm<{ members: TeamMember[]; agent_env: string }>()

  const { data = [], isLoading } = useQuery({
    queryKey: ['teams', workspaceId],
    queryFn: () => listTeams(),
    enabled: !!workspaceId,
  })
  const { data: agents = [] } = useQuery({
    queryKey: ['agents', workspaceId],
    queryFn: () => listAgents(),
    enabled: !!workspaceId,
  })
  const agentOptions = agents.map((a) => ({ value: a.id, label: a.name }))
  const versions = useQuery({
    queryKey: ['team-versions', versionOpen?.id],
    queryFn: () => listTeamVersions(versionOpen!.id),
    enabled: !!versionOpen,
  })

  const create = useMutation({
    mutationFn: (req: CreateTeamRequest) => createTeam(req),
    onSuccess: () => {
      message.success('已创建团队')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['teams'] })
    },
  })

  const run = useMutation({
    mutationFn: (vars: { team_id: string; input: string; env: string }) =>
      runTeam({ team_id: vars.team_id, env: vars.env, input: vars.input }),
    onSuccess: (res) => setRunResult(res),
  })

  const newVersion = useMutation({
    mutationFn: (body: { members: TeamMember[]; agent_env: string }) => createTeamVersion(versionOpen!.id, body),
    onSuccess: () => {
      message.success('团队新版本已创建并绑定 dev')
      versionForm.resetFields()
      qc.invalidateQueries({ queryKey: ['team-versions', versionOpen?.id] })
    },
  })

  const promote = useMutation({
    mutationFn: ({ versionId, env }: { versionId: string; env: string }) =>
      promoteTeamVersion(versionOpen!.id, versionId, env),
    onSuccess: (_, vars) => {
      message.success(`已将版本绑定到 ${vars.env}`)
      qc.invalidateQueries({ queryKey: ['team-versions', versionOpen?.id] })
    },
  })

  if (!workspaceId) {
    return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />
  }

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          团队
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建团队
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '模式', dataIndex: 'mode', render: (v: string) => <Tag color="blue">{v}</Tag> },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          {
            title: '操作',
            render: (_, row) => (
              <Space>
                <Button
                  size="small"
                  icon={<BranchesOutlined />}
                  onClick={() => {
                    setVersionOpen(row)
                    versionForm.resetFields()
                  }}
                >
                  版本
                </Button>
                <Button
                  size="small"
                  icon={<PlayCircleOutlined />}
                  onClick={() => {
                    setRunOpen(row)
                    setRunResult(null)
                    runForm.setFieldsValue({ env: 'dev', input: '' })
                  }}
                >
                  运行
                </Button>
              </Space>
            ),
          },
        ]}
      />

      {/* 新建团队 */}
      <Modal
        title="新建团队"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
        destroyOnClose
        width={600}
      >
        <Form
          form={form}
          layout="vertical"
          initialValues={{ mode: 'supervisor', members: [{ role: 'leader' }, { role: 'worker' }] }}
          onFinish={(v) => create.mutate(v)}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如:售后协作组" />
          </Form.Item>
          <Form.Item name="mode" label="模式" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'supervisor', label: 'supervisor(主管调度,首位为 leader)' },
                { value: 'pipeline', label: 'pipeline(按顺序流水)' },
              ]}
            />
          </Form.Item>
          <Divider>成员(Agent + 角色)</Divider>
          <Form.List name="members">
            {(fields, { add, remove }) => (
              <>
                {fields.map((field) => (
                  <Space key={field.key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                    <Form.Item
                      name={[field.name, 'agent_id']}
                      rules={[{ required: true, message: '选 Agent' }]}
                      style={{ marginBottom: 0 }}
                    >
                      <Select style={{ width: 280 }} placeholder="Agent" options={agentOptions} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'role']} style={{ marginBottom: 0 }}>
                      <Input placeholder="角色,如 leader / worker" />
                    </Form.Item>
                    <MinusCircleOutlined onClick={() => remove(field.name)} />
                  </Space>
                ))}
                <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ role: 'worker' })}>
                  添加成员
                </Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>

      <Modal
        title={`版本治理:${versionOpen?.name ?? ''}`}
        open={!!versionOpen}
        onCancel={() => setVersionOpen(null)}
        footer={null}
        width={820}
        destroyOnClose
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="每个版本会 pin 住所选环境中的 Agent Version；新版本自动绑定 dev，可继续提升到 staging 或 prod。"
        />
        <Form
          form={versionForm}
          layout="vertical"
          initialValues={{ agent_env: 'dev', members: [{ role: 'leader' }, { role: 'worker' }] }}
          onFinish={(v) => newVersion.mutate(v)}
        >
          <Form.Item name="agent_env" label="成员 Agent 来源环境" rules={[{ required: true }]}>
            <Select style={{ width: 220 }} options={['dev', 'staging', 'prod'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.List name="members">
            {(fields, { add, remove }) => (
              <>
                {fields.map((field) => (
                  <Space key={field.key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                    <Form.Item name={[field.name, 'agent_id']} rules={[{ required: true, message: '选 Agent' }]} style={{ marginBottom: 0 }}>
                      <Select style={{ width: 280 }} placeholder="Agent" options={agentOptions} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'role']} rules={[{ required: true }]} style={{ marginBottom: 0 }}>
                      <Input placeholder="角色" />
                    </Form.Item>
                    <MinusCircleOutlined onClick={() => remove(field.name)} />
                  </Space>
                ))}
                <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ role: 'worker' })}>添加成员</Button>
              </>
            )}
          </Form.List>
          <Button type="primary" htmlType="submit" loading={newVersion.isPending} style={{ marginTop: 12 }}>创建新版本</Button>
        </Form>
        <Divider>不可变版本</Divider>
        <Table
          rowKey="id"
          size="small"
          loading={versions.isLoading}
          dataSource={versions.data}
          pagination={false}
          columns={[
            { title: '版本', dataIndex: 'version', render: (v) => <Tag color="blue">v{v}</Tag> },
            { title: '成员数', dataIndex: 'members', render: (v: TeamMember[]) => v.length },
            { title: '当前环境', dataIndex: 'environments', render: (v: string[]) => v?.length ? v.map((env) => <Tag key={env} color="green">{env}</Tag>) : '-' },
            { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
            {
              title: '环境提升',
              render: (_, row) => (
                <Space size={4}>
                  {['dev', 'staging', 'prod'].map((env) => (
                    <Button key={env} size="small" loading={promote.isPending} onClick={() => promote.mutate({ versionId: row.id, env })}>
                      {env}
                    </Button>
                  ))}
                </Space>
              ),
            },
          ]}
        />
      </Modal>

      {/* 运行团队 */}
      <Modal
        title={`运行团队:${runOpen?.name ?? ''}`}
        open={!!runOpen}
        onCancel={() => setRunOpen(null)}
        onOk={() => runForm.submit()}
        okText="运行"
        confirmLoading={run.isPending}
        width={640}
        destroyOnClose
      >
        <Form
          form={runForm}
          layout="vertical"
          initialValues={{ env: 'dev' }}
          onFinish={(v) => runOpen && run.mutate({ team_id: runOpen.id, input: v.input, env: v.env })}
        >
          <Form.Item name="env" label="环境" rules={[{ required: true }]}>
            <Select options={['dev', 'staging', 'prod'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.Item name="input" label="输入" rules={[{ required: true, message: '请输入任务' }]}>
            <Input.TextArea rows={3} placeholder="给团队的任务,如:帮我处理这个退款工单……" />
          </Form.Item>
        </Form>
        {runResult && (
          <Card size="small" title="结果" style={{ marginTop: 8 }}>
            <Typography.Paragraph style={{ whiteSpace: 'pre-wrap' }}>
              <Typography.Text strong>最终输出:</Typography.Text>
              <br />
              {runResult.final}
            </Typography.Paragraph>
            {runResult.steps?.length > 0 && (
              <>
                <Divider style={{ margin: '8px 0' }} />
                <Typography.Text type="secondary">步骤数:{runResult.steps.length}</Typography.Text>
              </>
            )}
          </Card>
        )}
      </Modal>
    </div>
  )
}
