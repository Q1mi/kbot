import {
  Alert, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Table, Tabs, Tag,
  Typography, message,
} from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import axios from 'axios'
import {
  type ModelDeployment,
  createModelAccount, createModelDeployment, createModelProfile, createModelProfileVersion,
  getProjectModelBinding, listModelAccounts, listModelDeployments, listModelProfiles, listModelProfileVersions,
  saveProjectModelBinding,
  rotateModelAccountAPIKey,
  updateModelDeploymentPricing,
} from '@/api/model'
import { getAPIErrorMessage } from '@/api/client'
import { useAuthStore } from '@/store/authStore'

export function ModelsPage() {
  const workspaceId = useAuthStore((s) => s.workspaceId)
  if (!workspaceId) return <Alert type="info" showIcon message="请先选择工作空间" />

  return (
    <div>
      <Typography.Title level={4}>模型控制面</Typography.Title>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="API Key 加密保存且不会回显；Prompt 只引用不可变的 Model Profile Version。"
      />
      <Tabs
        items={[
          { key: 'accounts', label: 'Provider Accounts', children: <AccountsTab /> },
          { key: 'deployments', label: 'Deployments', children: <DeploymentsTab /> },
          { key: 'profiles', label: 'Profiles', children: <ProfilesTab /> },
		  { key: 'bindings', label: '项目绑定与限额', children: <BindingsTab /> },
        ]}
      />
    </div>
  )
}

function AccountsTab() {
  const qc = useQueryClient()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [rotateID, setRotateID] = useState<string>()
  const [form] = Form.useForm()
  const [rotateForm] = Form.useForm()
  const query = useQuery({ queryKey: ['model-accounts', workspaceId], queryFn: listModelAccounts })
  const create = useMutation({
    mutationFn: createModelAccount,
    onSuccess: () => {
      message.success('Provider Account 已创建')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['model-accounts', workspaceId] })
    },
  })
  const rotate = useMutation({
    mutationFn: ({ id, apiKey }: { id: string; apiKey: string }) => rotateModelAccountAPIKey(id, apiKey),
    onSuccess: () => {
      message.success('API Key 已轮换')
      setRotateID(undefined)
      rotateForm.resetFields()
    },
  })
  return (
    <Card
      extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>新增账号</Button>}
    >
      <Table
        rowKey="id"
        loading={query.isLoading}
        dataSource={query.data}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '类型', dataIndex: 'kind', render: (v) => <Tag>{v}</Tag> },
          { title: 'Base URL', dataIndex: 'base_url' },
          { title: 'API Key', dataIndex: 'has_api_key', render: (v) => (v ? '••••••••' : '-') },
          { title: '状态', dataIndex: 'status' },
          {
            title: '操作',
            render: (_, row) => <Button type="link" onClick={() => setRotateID(row.id)}>轮换 API Key</Button>,
          },
        ]}
      />
      <Modal title="新增 Provider Account" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()}>
        <Form form={form} layout="vertical" onFinish={(v) => create.mutate(v)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="kind" label="Provider 类型" initialValue="openai-compatible" rules={[{ required: true }]}>
            <Select options={['openai-compatible', 'openai', 'deepseek', 'ollama'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="api_key" label="API Key" rules={[{ required: true }]}><Input.Password /></Form.Item>
        </Form>
      </Modal>
      <Modal
        title="轮换 API Key"
        open={!!rotateID}
        onCancel={() => setRotateID(undefined)}
        onOk={() => rotateForm.submit()}
      >
        <Form
          form={rotateForm}
          layout="vertical"
          onFinish={(v) => rotateID && rotate.mutate({ id: rotateID, apiKey: v.api_key })}
        >
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            message="保存后立即影响引用该账号的模型部署；旧 Key 不会保留或回显。"
          />
          <Form.Item name="api_key" label="新 API Key" rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}

function DeploymentsTab() {
  const qc = useQueryClient()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [pricingDeployment, setPricingDeployment] = useState<ModelDeployment>()
  const [form] = Form.useForm()
  const [pricingForm] = Form.useForm()
  const accounts = useQuery({ queryKey: ['model-accounts', workspaceId], queryFn: listModelAccounts })
  const query = useQuery({ queryKey: ['model-deployments', workspaceId], queryFn: listModelDeployments })
  const create = useMutation({
    mutationFn: createModelDeployment,
    onSuccess: () => {
      message.success('Model Deployment 已创建')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['model-deployments', workspaceId] })
    },
  })
  const updatePricing = useMutation({
    mutationFn: (value: {
      input_price_per_million: number
      output_price_per_million: number
      cached_input_price_per_million: number
    }) => updateModelDeploymentPricing(pricingDeployment!.id, value),
    onSuccess: () => {
      message.success('Deployment 计价已更新')
      setPricingDeployment(undefined)
      pricingForm.resetFields()
      qc.invalidateQueries({ queryKey: ['model-deployments', workspaceId] })
    },
  })
  return (
    <Card extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>新增部署</Button>}>
      <Table
        rowKey="id"
        loading={query.isLoading}
        dataSource={query.data}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '模型', dataIndex: 'model_name' },
          { title: '区域', dataIndex: 'region' },
          { title: '超时(ms)', dataIndex: 'timeout_ms' },
          { title: '重试', dataIndex: 'max_retries' },
		  { title: '输入价/百万 Token', dataIndex: 'input_price_per_million' },
		  { title: '输出价/百万 Token', dataIndex: 'output_price_per_million' },
          {
            title: '操作',
            render: (_, row) => (
              <Button
                type="link"
                onClick={() => {
                  setPricingDeployment(row)
                  pricingForm.setFieldsValue(row)
                }}
              >
                配置价格
              </Button>
            ),
          },
        ]}
      />
      <Modal title="新增 Model Deployment" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()}>
		<Form form={form} layout="vertical" initialValues={{ timeout_ms: 120000, max_retries: 0, input_price_per_million: 0, output_price_per_million: 0, cached_input_price_per_million: 0 }} onFinish={(v) => create.mutate(v)}>
          <Form.Item name="provider_account_id" label="Provider Account" rules={[{ required: true }]}>
            <Select options={(accounts.data ?? []).map((v) => ({ value: v.id, label: v.name }))} />
          </Form.Item>
          <Form.Item name="name" label="部署名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="model_name" label="厂商模型名" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="region" label="区域"><Input /></Form.Item>
          <Space>
            <Form.Item name="timeout_ms" label="单次超时（ms）" extra="推理模型建议至少 120000 ms">
              <InputNumber min={1000} step={10000} />
            </Form.Item>
            <Form.Item name="max_retries" label="失败重试" extra="课堂演示建议为 0，避免重复计费">
              <InputNumber min={0} max={5} />
            </Form.Item>
          </Space>
		  <Typography.Text type="secondary">价格单位与月预算保持一致，例如都使用人民币或美元。</Typography.Text>
		  <Space wrap style={{ marginTop: 12 }}>
			<Form.Item name="input_price_per_million" label="输入价 / 百万 Token"><InputNumber min={0} precision={6} /></Form.Item>
			<Form.Item name="cached_input_price_per_million" label="缓存输入价 / 百万 Token"><InputNumber min={0} precision={6} /></Form.Item>
			<Form.Item name="output_price_per_million" label="输出价 / 百万 Token"><InputNumber min={0} precision={6} /></Form.Item>
		  </Space>
        </Form>
      </Modal>
      <Modal
        title={`配置计价${pricingDeployment ? ` · ${pricingDeployment.name}` : ''}`}
        open={!!pricingDeployment}
        onCancel={() => setPricingDeployment(undefined)}
        onOk={() => pricingForm.submit()}
        confirmLoading={updatePricing.isPending}
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="价格单位需与项目月预算保持一致；保存后用于后续模型调用的实时成本结算。"
        />
        <Form form={pricingForm} layout="vertical" onFinish={(value) => updatePricing.mutate(value)}>
          <Form.Item name="input_price_per_million" label="输入价 / 百万 Token" rules={[{ required: true }]}>
            <InputNumber min={0} precision={6} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="cached_input_price_per_million" label="缓存输入价 / 百万 Token" rules={[{ required: true }]}>
            <InputNumber min={0} precision={6} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="output_price_per_million" label="输出价 / 百万 Token" rules={[{ required: true }]}>
            <InputNumber min={0} precision={6} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}

function BindingsTab() {
  const [env, setEnv] = useState('dev')
  const [form] = Form.useForm()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const versions = useQuery({ queryKey: ['model-profile-versions', workspaceId], queryFn: listModelProfileVersions })
  const profiles = useQuery({ queryKey: ['model-profiles', workspaceId], queryFn: listModelProfiles })
  const binding = useQuery({
    queryKey: ['model-binding', workspaceId, env],
    queryFn: () => getProjectModelBinding(env),
    retry: false,
  })
  const save = useMutation({
    mutationFn: saveProjectModelBinding,
    onSuccess: (value) => {
      message.success(`${value.env} 环境模型绑定与限额已生效`)
      binding.refetch()
    },
  })
  const profileNames = new Map((profiles.data ?? []).map((profile) => [profile.id, profile.name]))
  const options = (versions.data ?? []).map((version) => ({
    value: version.id,
    label: `${profileNames.get(version.profile_id) ?? version.profile_id} · v${version.version}`,
  }))

  return (
    <Card>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="Gateway 会在每次 Provider 调用前原子预留 RPM、TPM 和月预算，响应后再按真实 Token usage 与 Deployment 价格结算。0 表示不限额。"
      />
      <Form
        form={form}
        layout="vertical"
        key={`${env}-${binding.dataUpdatedAt}`}
        initialValues={binding.data ?? { env, monthly_budget: 0, rpm_limit: 0, tpm_limit: 0 }}
        onFinish={(value) => save.mutate({ ...value, env })}
      >
        <Form.Item label="运行环境">
          <Select value={env} onChange={setEnv} options={['dev', 'staging', 'prod'].map((value) => ({ value, label: value }))} />
        </Form.Item>
        <Form.Item name="model_profile_version_id" label="Model Profile Version" rules={[{ required: true }]}>
          <Select options={options} showSearch optionFilterProp="label" />
        </Form.Item>
        <Space wrap>
          <Form.Item name="monthly_budget" label="月预算"><InputNumber min={0} precision={4} /></Form.Item>
          <Form.Item name="rpm_limit" label="每分钟请求数 RPM"><InputNumber min={0} precision={0} /></Form.Item>
          <Form.Item name="tpm_limit" label="每分钟 Token 数 TPM"><InputNumber min={0} precision={0} /></Form.Item>
        </Space>
        <div><Button type="primary" htmlType="submit" loading={save.isPending}>保存并生效</Button></div>
      </Form>
    </Card>
  )
}

function ProfilesTab() {
  const qc = useQueryClient()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [open, setOpen] = useState(false)
  const [versionProfileID, setVersionProfileID] = useState<string>()
  const [form] = Form.useForm()
  const [versionForm] = Form.useForm()
  const deployments = useQuery({ queryKey: ['model-deployments', workspaceId], queryFn: listModelDeployments })
  const profiles = useQuery({ queryKey: ['model-profiles', workspaceId], queryFn: listModelProfiles })
  const versions = useQuery({ queryKey: ['model-profile-versions', workspaceId], queryFn: listModelProfileVersions })
  const create = useMutation({
    mutationFn: createModelProfile,
    onSuccess: () => {
      message.success('Model Profile v1 已创建')
      setOpen(false)
      form.resetFields()
      qc.invalidateQueries({ queryKey: ['model-profiles', workspaceId] })
      qc.invalidateQueries({ queryKey: ['model-profile-versions', workspaceId] })
    },
    onError: (error) => {
      if (axios.isAxiosError(error) && error.response?.status === 409) {
        form.setFields([{ name: 'name', errors: [getAPIErrorMessage(error, 'Profile 名称已存在')] }])
      }
    },
  })
  const createVersion = useMutation({
    mutationFn: (body: {
      primary_deployment_id: string
      fallback_deployment_ids?: string[]
      classification_max?: string
    }) => createModelProfileVersion(versionProfileID!, body),
    onSuccess: () => {
      message.success('Model Profile 新版本已创建')
      setVersionProfileID(undefined)
      versionForm.resetFields()
      qc.invalidateQueries({ queryKey: ['model-profile-versions', workspaceId] })
    },
  })
  const profileNames = new Map((profiles.data ?? []).map((p) => [p.id, p.name]))
  const deploymentNames = new Map((deployments.data ?? []).map((d) => [d.id, d.name]))
  return (
    <Card extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>新增 Profile</Button>}>
      <Table
        rowKey="id"
        loading={versions.isLoading}
        dataSource={versions.data}
        columns={[
          { title: 'Profile', dataIndex: 'profile_id', render: (v) => profileNames.get(v) ?? v },
          { title: '版本', dataIndex: 'version', render: (v) => <Tag color="blue">v{v}</Tag> },
          { title: '主部署', dataIndex: 'primary_deployment_id', render: (v) => deploymentNames.get(v) ?? v },
          { title: 'Fallback', dataIndex: 'fallback_deployment_ids', render: (v: string[]) => v?.map((id) => deploymentNames.get(id) ?? id).join(', ') || '-' },
          { title: '最高分级', dataIndex: 'classification_max' },
          {
            title: '操作',
            render: (_, row) => (
              <Button
                type="link"
                onClick={() => {
                  versionForm.setFieldsValue({
                    primary_deployment_id: row.primary_deployment_id,
                    fallback_deployment_ids: row.fallback_deployment_ids,
                    classification_max: row.classification_max,
                  })
                  setVersionProfileID(row.profile_id)
                }}
              >
                基于此版本新建
              </Button>
            ),
          },
        ]}
      />
      <Modal
        title="新增 Model Profile"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={create.isPending}
      >
        <Form form={form} layout="vertical" initialValues={{ classification_max: 'internal' }} onFinish={(v) => create.mutate(v)}>
          <Form.Item
            name="name"
            label="名称"
            rules={[
              { required: true, message: '请输入 Profile 名称' },
              {
                validator: (_, value: string | undefined) => {
                  const name = value?.trim()
                  if (name && (profiles.data ?? []).some((profile) => profile.name === name)) {
                    return Promise.reject(new Error('同一工作空间内已存在同名 Profile，请更换名称'))
                  }
                  return Promise.resolve()
                },
              },
            ]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="primary_deployment_id" label="主部署" rules={[{ required: true }]}>
            <Select options={(deployments.data ?? []).map((v) => ({ value: v.id, label: `${v.name} / ${v.model_name}` }))} />
          </Form.Item>
          <Form.Item name="fallback_deployment_ids" label="Fallback 部署">
            <Select mode="multiple" options={(deployments.data ?? []).map((v) => ({ value: v.id, label: v.name }))} />
          </Form.Item>
          <Form.Item name="classification_max" label="最高数据分级">
            <Select options={['public', 'internal', 'confidential', 'secret'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`新增 Profile 版本 · ${profileNames.get(versionProfileID ?? '') ?? ''}`}
        open={!!versionProfileID}
        onCancel={() => setVersionProfileID(undefined)}
        onOk={() => versionForm.submit()}
        confirmLoading={createVersion.isPending}
      >
        <Form form={versionForm} layout="vertical" onFinish={(v) => createVersion.mutate(v)}>
          <Alert type="info" showIcon style={{ marginBottom: 16 }} message="新版本会获得递增版本号，已有版本继续保持不可变。" />
          <Form.Item name="primary_deployment_id" label="主部署" rules={[{ required: true }]}>
            <Select options={(deployments.data ?? []).map((v) => ({ value: v.id, label: `${v.name} / ${v.model_name}` }))} />
          </Form.Item>
          <Form.Item name="fallback_deployment_ids" label="Fallback 部署">
            <Select mode="multiple" options={(deployments.data ?? []).map((v) => ({ value: v.id, label: v.name }))} />
          </Form.Item>
          <Form.Item name="classification_max" label="最高数据分级">
            <Select options={['public', 'internal', 'confidential', 'secret'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
