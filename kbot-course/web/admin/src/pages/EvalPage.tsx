import {
  Table, Button, Modal, Form, Input, Select, InputNumber, Space, Typography, Tag, Alert, Card, Progress, Statistic, Row, Col, message,
} from 'antd'
import { PlusOutlined, PlayCircleOutlined, FileAddOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  listDatasets, createDataset, addCase, runEval, listEvalCases, listEvalRuns, type RunEvalRequest,
} from '@/api/eval'
import { listAgents, listAgentVersions } from '@/api/agent'
import type { EvalDataset, EvalRunResult } from '@/api/types'
import { fmtTime } from '@/lib/format'
import { useAuthStore } from '@/store/authStore'

export function EvalPage() {
  const qc = useQueryClient()
  const workspaceId = useAuthStore((s) => s.workspaceId)
  const [dsOpen, setDsOpen] = useState(false)
  const [caseFor, setCaseFor] = useState<EvalDataset | null>(null)
  const [runFor, setRunFor] = useState<EvalDataset | null>(null)
  const [inspectFor, setInspectFor] = useState<EvalDataset | null>(null)
  const [result, setResult] = useState<EvalRunResult | null>(null)
  const [dsForm] = Form.useForm()
  const [caseForm] = Form.useForm()
  const [runForm] = Form.useForm()
  const judgeMethod = Form.useWatch('judge_method', runForm)
  const targetAgentId = Form.useWatch('agent_id', runForm)
  const judgeAgentId = Form.useWatch('judge_agent_id', runForm)

  const { data = [], isLoading } = useQuery({
    queryKey: ['eval-datasets', workspaceId],
    queryFn: () => listDatasets(),
    enabled: !!workspaceId,
  })
  const { data: agents = [] } = useQuery({
    queryKey: ['agents', workspaceId],
    queryFn: () => listAgents(),
    enabled: !!workspaceId,
  })
  const targetVersionsQ = useQuery({
    queryKey: ['agent-versions', targetAgentId],
    queryFn: () => listAgentVersions(targetAgentId!),
    enabled: !!targetAgentId,
  })
  const judgeVersionsQ = useQuery({
    queryKey: ['agent-versions', judgeAgentId],
    queryFn: () => listAgentVersions(judgeAgentId!),
    enabled: !!judgeAgentId,
  })
  const casesQ = useQuery({
    queryKey: ['eval-cases', inspectFor?.id],
    queryFn: () => listEvalCases(inspectFor!.id),
    enabled: !!inspectFor,
  })
  const runsQ = useQuery({
    queryKey: ['eval-runs', inspectFor?.id],
    queryFn: () => listEvalRuns(inspectFor!.id),
    enabled: !!inspectFor,
  })

  const createDs = useMutation({
    mutationFn: (v: { name: string; target_kind: string }) => createDataset(v.name, v.target_kind),
    onSuccess: () => {
      message.success('已创建评估集')
      setDsOpen(false)
      dsForm.resetFields()
      qc.invalidateQueries({ queryKey: ['eval-datasets'] })
    },
  })

  const addCaseM = useMutation({
    mutationFn: (v: { input: string; expected: string; conversation_id?: string }) =>
      addCase(caseFor!.id, v.input, v.expected, v.conversation_id),
    onSuccess: () => {
      message.success('已添加用例')
      qc.invalidateQueries({ queryKey: ['eval-cases', caseFor?.id] })
      setCaseFor(null)
      caseForm.resetFields()
    },
  })

  const run = useMutation({
    mutationFn: (v: Omit<RunEvalRequest, 'dataset_id'>) => runEval({ ...v, dataset_id: runFor!.id }),
    onSuccess: (res) => {
      setResult(res)
      qc.invalidateQueries({ queryKey: ['eval-runs', runFor?.id] })
      setRunFor(null)
      runForm.resetFields()
    },
  })

  if (!workspaceId) return <Alert type="info" showIcon message="请先在右上角选择一个工作空间" />

  return (
    <div>
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          评测门禁
        </Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setDsOpen(true)}>
          新建评估集
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '目标类型', dataIndex: 'target_kind', render: (v: string) => <Tag>{v}</Tag> },
          { title: '创建时间', dataIndex: 'created_at', render: fmtTime },
          {
            title: '操作',
            render: (_, row: EvalDataset) => (
              <Space>
                <Button size="small" onClick={() => setInspectFor(row)}>详情 / 历史</Button>
                <Button size="small" icon={<FileAddOutlined />} onClick={() => setCaseFor(row)}>加用例</Button>
                <Button size="small" type="primary" icon={<PlayCircleOutlined />} onClick={() => { setRunFor(row); setResult(null) }}>
                  跑评估
                </Button>
              </Space>
            ),
          },
        ]}
      />

      {result && (
        <Card title={`评估结果 · run ${result.run_id.slice(0, 8)}`} style={{ marginTop: 16 }}>
          <Row gutter={24} align="middle" style={{ marginBottom: 16 }}>
            <Col>
              <Progress type="dashboard" percent={Math.round(result.pass_rate * 100)} size={120} status={result.passed ? 'success' : 'exception'} />
            </Col>
            <Col>
              <Statistic title="结果" valueRender={() => (result.passed ? <Tag color="green">通过门禁</Tag> : <Tag color="red">未达阈值</Tag>)} value=" " />
              <Statistic title="用例总数" value={result.total} />
            </Col>
          </Row>
          <Table
            rowKey={(r) => r.case_id + r.dimension}
            size="small"
            dataSource={result.scores}
            pagination={false}
            columns={[
              { title: '用例', dataIndex: 'case_id', ellipsis: true },
              { title: '维度', dataIndex: 'dimension', render: (v: string) => <Tag>{v || 'default'}</Tag> },
              {
                title: '分数',
                dataIndex: 'score',
                render: (v: number) => <Tag color={v >= 0.5 ? 'green' : 'red'}>{v.toFixed(2)}</Tag>,
              },
              { title: '判定理由', dataIndex: 'reason', ellipsis: true },
            ]}
          />
        </Card>
      )}

      {/* 新建评估集 */}
      <Modal title="新建评估集" open={dsOpen} onCancel={() => setDsOpen(false)} onOk={() => dsForm.submit()} confirmLoading={createDs.isPending} destroyOnClose>
        <Form form={dsForm} layout="vertical" initialValues={{ target_kind: 'agent' }} onFinish={(v) => createDs.mutate(v)}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如:退款回答质量集" />
          </Form.Item>
          <Form.Item name="target_kind" label="目标类型">
            <Select options={[{ value: 'agent', label: 'agent' }]} />
          </Form.Item>
          <Alert type="info" showIcon message="当前可执行评测目标为不可变 Agent Version。" />
        </Form>
      </Modal>

      <Modal
        title={`评估集详情 · ${inspectFor?.name ?? ''}`}
        open={!!inspectFor}
        onCancel={() => setInspectFor(null)}
        footer={null}
        width={980}
        destroyOnClose
      >
        <Typography.Title level={5}>测试用例</Typography.Title>
        <Table
          rowKey="id"
          size="small"
          loading={casesQ.isLoading}
          dataSource={casesQ.data}
          pagination={{ pageSize: 5 }}
          columns={[
            { title: '输入', dataIndex: 'input', ellipsis: true },
            { title: '期望', dataIndex: 'expected', ellipsis: true },
            { title: '来源元数据', dataIndex: 'metadata', ellipsis: true },
          ]}
        />
        <Typography.Title level={5} style={{ marginTop: 20 }}>运行历史</Typography.Title>
        <Table
          rowKey={(item) => item.run.id}
          size="small"
          loading={runsQ.isLoading}
          dataSource={runsQ.data}
          expandable={{
            expandedRowRender: (item) => (
              <Table
                rowKey={(score) => `${score.case_id}-${score.dimension}`}
                size="small"
                pagination={false}
                dataSource={item.scores}
                columns={[
                  { title: '用例', dataIndex: 'case_id', ellipsis: true },
                  { title: '得分', dataIndex: 'score', render: (v: number) => v.toFixed(2) },
                  { title: '理由', dataIndex: 'reason' },
                ]}
              />
            ),
          }}
          columns={[
            { title: '运行时间', dataIndex: ['run', 'created_at'], render: fmtTime },
            { title: 'Judge', dataIndex: ['run', 'judge_id'], render: (v) => <Tag>{v}</Tag> },
            { title: '通过率', dataIndex: ['run', 'pass_rate'], render: (v: number) => `${Math.round(v * 100)}%` },
            { title: '阈值', dataIndex: ['run', 'threshold'] },
            { title: '结果', dataIndex: ['run', 'status'], render: (v) => <Tag color={v === 'passed' ? 'green' : 'red'}>{v}</Tag> },
          ]}
        />
      </Modal>

      {/* 加用例 */}
      <Modal title={`加用例 · ${caseFor?.name ?? ''}`} open={!!caseFor} onCancel={() => setCaseFor(null)} onOk={() => caseForm.submit()} confirmLoading={addCaseM.isPending} destroyOnClose>
        <Form form={caseForm} layout="vertical" onFinish={(v) => addCaseM.mutate(v)}>
          <Form.Item name="input" label="输入" rules={[{ required: true, message: '请输入' }]}>
            <Input.TextArea rows={2} placeholder="用户问题" />
          </Form.Item>
          <Form.Item name="expected" label="期望(按 Judge 解释:关键词/答案)" rules={[{ required: true }]}>
            <Input.TextArea rows={2} placeholder="期望命中的关键词或答案" />
          </Form.Item>
          <Form.Item name="conversation_id" label="来源 Conversation ID（可选）">
            <Input placeholder="填写后记录为坏样本回流来源" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 跑评估 */}
      <Modal title={`跑评估 · ${runFor?.name ?? ''}`} open={!!runFor} onCancel={() => setRunFor(null)} onOk={() => runForm.submit()} okText="运行" confirmLoading={run.isPending} destroyOnClose>
        <Form form={runForm} layout="vertical" initialValues={{ judge_method: 'contains', threshold: 0.85 }} onFinish={(v) => run.mutate(v)}>
          <Form.Item name="agent_id" label="目标 Agent" rules={[{ required: true, message: '请选 Agent' }]}>
            <Select
              placeholder="选择 Agent"
              options={agents.map((a) => ({ value: a.id, label: a.name }))}
              onChange={() => runForm.setFieldValue('agent_version_id', undefined)}
            />
          </Form.Item>
          <Form.Item name="agent_version_id" label="目标 Agent Version" rules={[{ required: true, message: '请选择固定版本' }]}>
            <Select
              loading={targetVersionsQ.isLoading}
              placeholder="选择不可变版本"
              options={(targetVersionsQ.data ?? []).map((version) => ({
                value: version.id,
                label: `v${version.version}${version.environments.length ? ` · ${version.environments.join('/')}` : ''}`,
              }))}
            />
          </Form.Item>
          <Form.Item name="judge_method" label="Judge 方法">
            <Select options={[
              { value: 'contains', label: '关键词完整命中（零成本）' },
              { value: 'exact', label: '精确匹配（零成本）' },
              { value: 'regex', label: '正则匹配（零成本）' },
              { value: 'llm-light', label: 'LLM Judge · light' },
              { value: 'llm-full', label: 'LLM Judge · full' },
            ]} />
          </Form.Item>
          {judgeMethod?.startsWith('llm-') && (
            <Form.Item name="judge_agent_id" label="Judge Agent" rules={[{ required: true, message: '请选择 Judge Agent' }]}>
              <Select
                placeholder="选择一个独立的评判 Agent"
                options={agents.map((a) => ({ value: a.id, label: a.name }))}
                onChange={() => runForm.setFieldValue('judge_agent_version_id', undefined)}
              />
            </Form.Item>
          )}
          {judgeMethod?.startsWith('llm-') && (
            <Form.Item name="judge_agent_version_id" label="Judge Agent Version" rules={[{ required: true, message: '请选择固定 Judge 版本' }]}>
              <Select
                loading={judgeVersionsQ.isLoading}
                placeholder="选择不可变 Judge 版本"
                options={(judgeVersionsQ.data ?? []).map((version) => ({
                  value: version.id,
                  label: `v${version.version}${version.environments.length ? ` · ${version.environments.join('/')}` : ''}`,
                }))}
              />
            </Form.Item>
          )}
          <Form.Item name="threshold" label="通过阈值">
            <InputNumber min={0} max={1} step={0.05} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
