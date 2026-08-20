import { Alert, Form, Input, InputNumber, Radio, Select, Space, Switch, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { listPrompts } from '@/api/prompt'
import { listTools } from '@/api/tool'
import { listKBs } from '@/api/kb'
import { listSkills, listSkillVersions } from '@/api/skill'
import type { Skill, SkillVersion } from '@/api/types'
import { useAuthStore } from '@/store/authStore'

interface VersionOption extends SkillVersion {
  skill: Skill
  requiresNetwork: boolean
  allowedTools: string[]
  allowedKBs: string[]
}

function metadataOf(version: SkillVersion) {
  try {
    const value = JSON.parse(version.frontmatter_json) as {
      allowed_tools?: string[]
      allowed_kbs?: string[]
      requires_network?: boolean
    }
    return {
      requiresNetwork: !!value.requires_network,
      allowedTools: value.allowed_tools ?? [],
      allowedKBs: value.allowed_kbs ?? [],
    }
  } catch {
    return { requiresNetwork: false, allowedTools: [], allowedKBs: [] }
  }
}

export function AgentConfigFields() {
	const form = Form.useFormInstance()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const prompts = useQuery({ queryKey: ['prompts', workspaceId], queryFn: listPrompts, enabled: !!workspaceId })
  const tools = useQuery({ queryKey: ['tools', workspaceId], queryFn: listTools, enabled: !!workspaceId })
  const kbs = useQuery({ queryKey: ['kbs', workspaceId], queryFn: listKBs, enabled: !!workspaceId })
  const skills = useQuery({ queryKey: ['skills', workspaceId], queryFn: listSkills, enabled: !!workspaceId })
  const versions = useQuery({
    queryKey: ['published-skill-versions', workspaceId, (skills.data ?? []).map((v) => v.id).join(',')],
    enabled: !!workspaceId && !!skills.data,
    queryFn: async (): Promise<VersionOption[]> => {
      const rows = await Promise.all(
        (skills.data ?? []).map(async (skill) =>
          (await listSkillVersions(skill.id)).map((version) => ({ ...version, skill, ...metadataOf(version) })),
        ),
      )
      return rows.flat().filter((version) => version.status === 'published')
    },
  })
  const selectedVersionIDs = Form.useWatch<string[]>('skill_version_ids') ?? []
  const selectedVersions = (versions.data ?? []).filter((version) => selectedVersionIDs.includes(version.id))
  const networkRequired = selectedVersions.some((version) => version.requiresNetwork)
  const [systemSource, setSystemSource] = useState<'prompt-center' | 'literal'>(() => (
    form.getFieldValue('system_prompt') && !form.getFieldValue('system_prompt_id') ? 'literal' : 'prompt-center'
  ))
  const systemPrompts = (prompts.data ?? []).filter((item) => !item.category.endsWith('-user-template'))
  const userPromptTemplates = (prompts.data ?? []).filter((item) => item.category.endsWith('-user-template'))

  const changeSystemSource = (source: 'prompt-center' | 'literal') => {
    setSystemSource(source)
    if (source === 'prompt-center') form.setFieldValue('system_prompt', undefined)
    else form.setFieldValue('system_prompt_id', undefined)
  }

  return (
    <>
      <Form.Item label="System Prompt 来源">
        <Radio.Group
          value={systemSource}
          optionType="button"
          buttonStyle="solid"
          onChange={(event) => changeSystemSource(event.target.value as 'prompt-center' | 'literal')}
          options={[
            { value: 'prompt-center', label: '绑定 Prompt Center' },
            { value: 'literal', label: '使用字面量' },
          ]}
        />
      </Form.Item>
      {systemSource === 'literal' ? (
        <Form.Item name="system_prompt" label="System Prompt（字面量）">
          <Input.TextArea rows={4} placeholder="适合原型和简单 Agent" />
        </Form.Item>
      ) : (
        <Form.Item name="system_prompt_id" label="绑定 System Prompt">
          <Select
            allowClear
            showSearch
            optionFilterProp="label"
            loading={prompts.isLoading}
            options={systemPrompts.map((prompt) => ({ value: prompt.id, label: `${prompt.name} · ${prompt.category}` }))}
            placeholder="创建会话时按环境解析并固化 System Prompt 版本"
          />
        </Form.Item>
      )}
      <Form.Item name="user_prompt_id" label="User Prompt Template（首轮任务，可选）">
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          loading={prompts.isLoading}
          options={userPromptTemplates.map((prompt) => ({ value: prompt.id, label: prompt.name }))}
          placeholder="会话 Playground 将根据变量 Schema 生成业务任务表单"
        />
      </Form.Item>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="System Prompt 定义角色和运行边界；User Prompt Template 只用于渲染首轮业务任务，后续追问继续使用普通消息。"
      />
      <Form.Item name="prompt_env" label="Prompt 环境">
        <Select options={['dev', 'staging', 'prod'].map((value) => ({ value, label: value }))} />
      </Form.Item>
      <Form.Item name="tool_ids" label="Tools">
        <Select
          mode="multiple"
          showSearch
          optionFilterProp="label"
          loading={tools.isLoading}
          options={(tools.data ?? []).map((tool) => ({
            value: tool.id,
            label: `${tool.name} · ${tool.source_type}${tool.sensitive ? ' · 需审批' : ''}`,
          }))}
          placeholder="选择 Agent 可调用的工具"
        />
      </Form.Item>
      <Form.Item name="kb_ids" label="知识库">
        <Select
          mode="multiple"
          showSearch
          optionFilterProp="label"
          loading={kbs.isLoading}
          options={(kbs.data ?? []).map((kb) => ({ value: kb.id, label: `${kb.name} · ${kb.status}` }))}
          placeholder="选择 Agent 可检索的知识库"
        />
      </Form.Item>
      <Form.Item name="skill_version_ids" label="已发布 Skills">
        <Select
          mode="multiple"
          showSearch
          optionFilterProp="label"
          loading={versions.isLoading}
          options={(versions.data ?? []).map((version) => ({
            value: version.id,
            label: `${version.skill.name} · v${version.version}${version.requiresNetwork ? ' · 需要网络' : ''}`,
          }))}
          placeholder="Skill 引用的 Tool 和 KB 也需要挂载到 Agent"
        />
      </Form.Item>
      {selectedVersions.length > 0 && (
        <Alert
          type={networkRequired ? 'warning' : 'info'}
          showIcon
          style={{ marginBottom: 16 }}
          message={
            <Space wrap>
              <Typography.Text>Skill 依赖</Typography.Text>
              {[...new Set(selectedVersions.flatMap((v) => v.allowedTools))].map((name) => <Tag key={name}>Tool: {name}</Tag>)}
              {[...new Set(selectedVersions.flatMap((v) => v.allowedKBs))].map((id) => <Tag key={id}>KB: {id.slice(0, 8)}</Tag>)}
              {networkRequired && <Tag color="orange">需要开启网络</Tag>}
            </Space>
          }
        />
      )}
      <Form.Item name="allow_network" label="允许网络工具" valuePropName="checked">
        <Switch checkedChildren="已授权" unCheckedChildren="已关闭" />
      </Form.Item>
      <Typography.Paragraph type="secondary">
        关闭后，REST、MCP 和 A2A 工具会在审批及执行前被 Runtime 拒绝。
      </Typography.Paragraph>
      <Form.Item name="max_steps" label="最大步数">
        <InputNumber min={1} max={50} style={{ width: '100%' }} />
      </Form.Item>
    </>
  )
}
