import {
  Alert, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Table, Tabs, Tag,
  Typography, message,
} from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import axios from 'axios'
import {
  createModelAccount, createModelDeployment, createModelProfile, createModelProfileVersion,
  listModelAccounts, listModelDeployments, listModelProfiles, listModelProfileVersions,
  rotateModelAccountAPIKey,
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
  const [form] = Form.useForm()
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
        ]}
      />
      <Modal title="新增 Model Deployment" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()}>
        <Form form={form} layout="vertical" initialValues={{ timeout_ms: 120000, max_retries: 0 }} onFinish={(v) => create.mutate(v)}>
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
        </Form>
      </Modal>
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
