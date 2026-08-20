import { Alert, Button, Card, Descriptions, Result, Space, Spin, Table, Tag, Typography, message } from 'antd'
import { ArrowLeftOutlined, CloudUploadOutlined, SaveOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'
import { useLocation, useRoute } from 'wouter'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createSkillVersion, listSkills, listSkillVersions, publishSkill } from '@/api/skill'
import { SkillEditor, joinSkillMD } from '@/components/SkillEditor'
import type { SkillVersion } from '@/api/types'
import { fmtTime } from '@/lib/format'

const DEFAULT_FM = 'name: my-skill\ndescription: ""\nallowed-tools: []\nallowed-kbs: []\ndisable-model-invocation: false\nrequires_network: false'
const DEFAULT_BODY = '# 流程\n\n描述这个技能的步骤……'

function frontmatterOf(version: SkillVersion): string {
  try {
    const value = JSON.parse(version.frontmatter_json) as {
      name?: string
      description?: string
      allowed_tools?: string[]
      allowed_kbs?: string[]
      disable_model_invocation?: boolean
      requires_network?: boolean
    }
    const list = (key: string, values: string[] = []) => values.length
      ? `${key}:\n${values.map((item) => `  - ${JSON.stringify(item)}`).join('\n')}`
      : `${key}: []`
    return [
      `name: ${value.name ?? 'my-skill'}`,
      `description: ${JSON.stringify(value.description ?? '')}`,
      list('allowed-tools', value.allowed_tools),
      list('allowed-kbs', value.allowed_kbs),
      `disable-model-invocation: ${!!value.disable_model_invocation}`,
      `requires_network: ${!!value.requires_network}`,
    ].join('\n')
  } catch {
    return DEFAULT_FM
  }
}

export function SkillDetailPage() {
  const [, params] = useRoute('/skills/:id')
  const id = params?.id ?? ''
  const [, navigate] = useLocation()
  const qc = useQueryClient()
  const [fm, setFm] = useState(DEFAULT_FM)
  const [body, setBody] = useState(DEFAULT_BODY)
  const [initialized, setInitialized] = useState(false)

  const skillsQ = useQuery({ queryKey: ['skills'], queryFn: listSkills })
  const versionsQ = useQuery({ queryKey: ['skill-versions', id], queryFn: () => listSkillVersions(id), enabled: !!id })
  const skill = skillsQ.data?.find((item) => item.id === id)

  useEffect(() => {
    if (initialized || !versionsQ.data?.length) return
    const latest = versionsQ.data[versionsQ.data.length - 1]
    setFm(frontmatterOf(latest))
    setBody(latest.body_md)
    setInitialized(true)
  }, [initialized, versionsQ.data])

  const saveVersion = useMutation({
    mutationFn: () => createSkillVersion(id, joinSkillMD(fm, body)),
    onSuccess: (version) => {
      message.success(`已保存为 v${version.version}（draft）`)
      qc.invalidateQueries({ queryKey: ['skill-versions', id] })
    },
  })
  const publish = useMutation({
    mutationFn: (version: SkillVersion) => publishSkill(id, version.id),
    onSuccess: (_, version) => {
      message.success(`v${version.version} 已发布`)
      qc.invalidateQueries({ queryKey: ['skill-versions', id] })
      qc.invalidateQueries({ queryKey: ['published-skill-versions'] })
    },
  })

  if (skillsQ.isLoading || versionsQ.isLoading) return <Spin />
  if (!skill) return <Result status="404" title="Skill 不存在" />

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/skills')}>返回</Button>
        <Typography.Title level={4} style={{ margin: 0 }}>{skill.name}</Typography.Title>
        {skill.category && <Tag>{skill.category}</Tag>}
      </Space>

      <Card style={{ marginBottom: 16 }}>
        <Descriptions column={2} size="small">
          <Descriptions.Item label="ID"><Typography.Text copyable>{skill.id}</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="创建时间">{fmtTime(skill.created_at)}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="版本记录" style={{ marginBottom: 16 }}>
        <Table
          rowKey="id"
          dataSource={versionsQ.data ?? []}
          pagination={false}
          columns={[
            { title: '版本', dataIndex: 'version', render: (value: number) => <Tag color="blue">v{value}</Tag> },
            { title: '状态', dataIndex: 'status', render: (value: string) => <Tag color={value === 'published' ? 'green' : 'gold'}>{value}</Tag> },
            { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
            {
              title: '操作',
              render: (_, row: SkillVersion) => row.status === 'published'
                ? <Typography.Text type="secondary">已发布</Typography.Text>
                : <Button size="small" type="primary" icon={<CloudUploadOutlined />} loading={publish.isPending} onClick={() => publish.mutate(row)}>发布</Button>,
            },
          ]}
        />
      </Card>

      <Alert
        style={{ marginBottom: 12 }}
        type="info"
        showIcon
        message="allowed-tools 和 allowed-kbs 会在发布时校验；运行时继续执行 Agent 与 Skill 的双层 allowlist。"
      />
      <Card
        title="基于最新版本创建新版本"
        extra={<Button type="primary" icon={<SaveOutlined />} loading={saveVersion.isPending} onClick={() => saveVersion.mutate()}>保存为新版本</Button>}
      >
        <SkillEditor frontmatter={fm} body={body} onFrontmatter={setFm} onBody={setBody} />
      </Card>
    </div>
  )
}
