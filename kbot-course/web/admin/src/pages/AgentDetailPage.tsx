import {
  Alert, Button, Card, Descriptions, Form, message, Modal, Result, Space, Spin, Table, Tag, Typography,
} from 'antd'
import { ArrowLeftOutlined, MessageOutlined, PlusOutlined } from '@ant-design/icons'
import { useLocation, useRoute } from 'wouter'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createAgentVersion, getAgent, listAgentVersions, promoteAgentVersion } from '@/api/agent'
import type { AgentVersion, AgentVersionConfig } from '@/api/types'
import { fmtTime } from '@/lib/format'
import { AgentConfigFields } from '@/components/AgentConfigFields'
import { useState } from 'react'

const EMPTY_CONFIG: AgentVersionConfig = {
  prompt_env: 'dev', tool_ids: [], skill_version_ids: [], kb_ids: [], allow_network: false, max_steps: 8,
}

export function AgentDetailPage() {
  const [, params] = useRoute('/agents/:id')
  const id = params?.id ?? ''
  const [, navigate] = useLocation()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<AgentVersionConfig>()
  const agentQ = useQuery({ queryKey: ['agent', id], queryFn: () => getAgent(id) })
  const versionsQ = useQuery({ queryKey: ['agent-versions', id], queryFn: () => listAgentVersions(id), enabled: !!id })

  const create = useMutation({
    mutationFn: (config: AgentVersionConfig) => createAgentVersion(id, config),
    onSuccess: (version) => {
      message.success(`Agent v${version.version} 已创建并绑定到 dev`)
      setOpen(false)
      qc.invalidateQueries({ queryKey: ['agent-versions', id] })
    },
  })
  const promote = useMutation({
    mutationFn: ({ version, env }: { version: AgentVersion; env: string }) => promoteAgentVersion(id, version.id, env),
    onSuccess: (_, values) => {
      message.success(`v${values.version.version} 已晋升到 ${values.env}`)
      qc.invalidateQueries({ queryKey: ['agent-versions', id] })
    },
  })

  const openNewVersion = () => {
    const latest = versionsQ.data?.[0]
    form.setFieldsValue({ ...EMPTY_CONFIG, ...(latest?.config ?? {}) })
    setOpen(true)
  }
  const confirmPromote = (version: AgentVersion, env: string) => {
    Modal.confirm({
      title: `将 v${version.version} 晋升到 ${env}`,
      content: env === 'prod' ? '新会话和引用 prod 环境的 Team 将使用该版本。' : '环境指针会立即更新。',
      okText: '确认晋升',
      okButtonProps: env === 'prod' ? { danger: true } : undefined,
      onOk: () => promote.mutateAsync({ version, env }),
    })
  }

  if (agentQ.isLoading) return <Spin />
  if (agentQ.isError || !agentQ.data) return <Result status="404" title="Agent 不存在" />
  const agent = agentQ.data

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/agents')}>返回</Button>
        <Typography.Title level={4} style={{ margin: 0 }}>{agent.name}</Typography.Title>
      </Space>

      <Card
        style={{ marginBottom: 16 }}
        extra={<Button type="primary" icon={<MessageOutlined />} onClick={() => navigate('/conversations')}>去测试对话</Button>}
      >
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label="ID"><Typography.Text copyable>{agent.id}</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="模板">{agent.template}</Descriptions.Item>
          <Descriptions.Item label="工作空间">{agent.workspace_id}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{fmtTime(agent.created_at)}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card
        title="不可变版本与环境发布"
        extra={<Button type="primary" icon={<PlusOutlined />} onClick={openNewVersion}>新建版本</Button>}
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="新版本会固化 System Prompt、User Prompt Template、Tool Version、Skill Version、KB 与网络权限；旧会话继续使用原版本。"
        />
        <Table
          rowKey="id"
          loading={versionsQ.isLoading}
          dataSource={versionsQ.data ?? []}
          pagination={false}
          columns={[
            { title: '版本', dataIndex: 'version', render: (value: number) => <Tag color="blue">v{value}</Tag> },
            {
              title: '配置',
              render: (_, row: AgentVersion) => (
                <Space wrap>
                  <Tag>{row.config.system_prompt_id ? 'Prompt 中心' : '字面量 Prompt'}</Tag>
                  <Tag color={row.config.user_prompt_id ? 'purple' : 'default'}>
                    {row.config.user_prompt_id ? 'User Template' : '自由输入'}
                  </Tag>
                  <Tag>{row.config.tool_ids?.length ?? 0} Tools</Tag>
                  <Tag>{row.config.skill_version_ids?.length ?? 0} Skills</Tag>
                  <Tag>{row.config.kb_ids?.length ?? 0} KBs</Tag>
                  <Tag color={row.config.allow_network ? 'orange' : 'default'}>网络 {row.config.allow_network ? '开' : '关'}</Tag>
                </Space>
              ),
            },
            {
              title: '环境', dataIndex: 'environments',
              render: (envs: string[]) => envs?.length ? <Space>{envs.map((env) => <Tag color={env === 'prod' ? 'red' : 'green'} key={env}>{env}</Tag>)}</Space> : '-',
            },
            { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
            {
              title: '晋升',
              render: (_, row: AgentVersion) => (
                <Space>
                  {['dev', 'staging', 'prod'].map((env) => (
                    <Button key={env} size="small" disabled={row.environments?.includes(env)} onClick={() => confirmPromote(row, env)}>
                      → {env}
                    </Button>
                  ))}
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        title="创建 Agent 新版本"
        open={open}
        width={760}
        destroyOnClose
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
      >
        <Form form={form} layout="vertical" onFinish={(values) => create.mutate(values)}>
          <AgentConfigFields />
        </Form>
      </Modal>
    </div>
  )
}
