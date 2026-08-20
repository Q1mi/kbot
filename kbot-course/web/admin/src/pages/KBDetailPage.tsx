import {
  Tabs, Card, Button, Space, Typography, Table, Tag, Input, Row, Col, List, Empty, Spin, Result, Statistic, message,
} from 'antd'
import { ArrowLeftOutlined, SyncOutlined, SearchOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useLocation, useRoute } from 'wouter'
import { useQuery, useMutation } from '@tanstack/react-query'
import {
  listKBs, listDocuments, listConnectors, listJobs, syncMarkdown, search, type SearchMode,
} from '@/api/kb'
import type { Passage } from '@/api/types'
import { fmtTime } from '@/lib/format'

export function KBDetailPage() {
  const [, params] = useRoute('/kbs/:id')
  const id = params?.id ?? ''
  const [, navigate] = useLocation()
  const kbsQ = useQuery({ queryKey: ['kbs'], queryFn: () => listKBs() })
  const kb = kbsQ.data?.find((k) => k.id === id)

  if (kbsQ.isLoading) return <Spin />
  if (!kb) return <Result status="404" title="知识库不存在" />

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/kbs')}>
          返回
        </Button>
        <Typography.Title level={4} style={{ margin: 0 }}>
          {kb.name}
        </Typography.Title>
        <Tag>{kb.embedding_model || 'local'}</Tag>
        <Tag color="green">{kb.status}</Tag>
      </Space>

      <Tabs
        defaultActiveKey="search"
        items={[
          { key: 'search', label: '检索 Playground', children: <SearchPlayground kbId={id} /> },
          { key: 'documents', label: 'Documents', children: <DocumentsTab kbId={id} /> },
          { key: 'connectors', label: 'Connectors', children: <ConnectorsTab kbId={id} /> },
          { key: 'jobs', label: 'Ingest Jobs', children: <JobsTab kbId={id} /> },
        ]}
      />
    </div>
  )
}

// 三列对比:BM25 / 向量 / 混合,各展示 top-k chunk 与分数。
function SearchPlayground({ kbId }: { kbId: string }) {
  const [query, setQuery] = useState('怎么申请退款?')
  const MODES: { mode: SearchMode; title: string }[] = [
    { mode: 'bm25', title: 'BM25(关键词)' },
    { mode: 'vector', title: '向量(语义)' },
    { mode: 'hybrid', title: '混合 + Rerank' },
  ]
  const [results, setResults] = useState<Record<string, Passage[]>>({})
  const [loading, setLoading] = useState(false)

  async function run() {
    if (!query.trim()) return
    setLoading(true)
    try {
      const entries = await Promise.all(
        MODES.map(async ({ mode }) => [mode, await search(kbId, query, mode, 5)] as const),
      )
      setResults(Object.fromEntries(entries))
    } catch {
      // 拦截器已提示
    } finally {
      setLoading(false)
    }
  }

  return (
    <Card>
      <Space.Compact style={{ width: '100%', marginBottom: 16 }}>
        <Input value={query} onChange={(e) => setQuery(e.target.value)} onPressEnter={run} placeholder="输入查询,回车对比三档检索" />
        <Button type="primary" icon={<SearchOutlined />} loading={loading} onClick={run}>
          对比检索
        </Button>
      </Space.Compact>
      <Row gutter={12}>
        {MODES.map(({ mode, title }) => (
          <Col span={8} key={mode}>
            <Card size="small" title={title} styles={{ body: { maxHeight: 420, overflow: 'auto' } }}>
              {(results[mode] ?? []).length === 0 ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无结果" />
              ) : (
                <List
                  size="small"
                  dataSource={results[mode]}
                  renderItem={(p, i) => (
                    <List.Item>
                      <Space direction="vertical" size={2} style={{ width: '100%' }}>
                        <Space>
                          <Tag color="blue">#{i + 1}</Tag>
                          <Typography.Text type="secondary">score {p.score.toFixed(4)}</Typography.Text>
                        </Space>
                        <Typography.Paragraph style={{ margin: 0 }} ellipsis={{ rows: 4, expandable: true }}>
                          {p.text}
                        </Typography.Paragraph>
                      </Space>
                    </List.Item>
                  )}
                />
              )}
            </Card>
          </Col>
        ))}
      </Row>
    </Card>
  )
}

function DocumentsTab({ kbId }: { kbId: string }) {
  const { data = [], isLoading, refetch } = useQuery({ queryKey: ['kb-docs', kbId], queryFn: () => listDocuments(kbId) })
  return (
    <Table
      rowKey="id"
      loading={isLoading}
      dataSource={data}
      pagination={{ pageSize: 10 }}
      title={() => <Button size="small" icon={<SyncOutlined />} onClick={() => refetch()}>刷新</Button>}
      columns={[
        { title: '来源', dataIndex: 'source_uri', ellipsis: true },
        { title: '类型', dataIndex: 'source_type', render: (v: string) => <Tag>{v}</Tag> },
        { title: '分级', dataIndex: 'classification', render: (v: string) => <Tag>{v}</Tag> },
        {
          title: '状态',
          dataIndex: 'status',
          render: (v: string) => <Tag color={v === 'processed' ? 'green' : v === 'error' ? 'red' : 'default'}>{v}</Tag>,
        },
        { title: 'Ingest 时间', dataIndex: 'ingested_at', render: fmtTime },
      ]}
    />
  )
}

function ConnectorsTab({ kbId }: { kbId: string }) {
  const [path, setPath] = useState('/app/kb-demo')
  const connsQ = useQuery({ queryKey: ['kb-conns', kbId], queryFn: () => listConnectors(kbId) })

  const sync = useMutation({
    mutationFn: () => syncMarkdown(kbId, path),
    onSuccess: (r) => {
      message.success(`同步完成:列出 ${r.listed},ingest ${r.ingested},跳过 ${r.skipped}`)
      connsQ.refetch()
    },
  })

  return (
    <>
      <Card size="small" style={{ marginBottom: 16 }} title="Markdown 文件夹同步">
        <Space.Compact style={{ width: '100%' }}>
          <Input value={path} onChange={(e) => setPath(e.target.value)} addonBefore="root_path" placeholder="服务端可见的目录路径" />
          <Button type="primary" icon={<SyncOutlined />} loading={sync.isPending} onClick={() => sync.mutate()}>
            同步并 Ingest
          </Button>
        </Space.Compact>
        <Typography.Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
          路径是服务端进程可见的目录(容器内,如 /app/kb-demo)。
        </Typography.Text>
      </Card>
      <Table
        rowKey="id"
        loading={connsQ.isLoading}
        dataSource={connsQ.data ?? []}
        pagination={false}
        columns={[
          { title: '类型', dataIndex: 'connector_kind', render: (v: string) => <Tag>{v}</Tag> },
          { title: '配置', dataIndex: 'config_json', ellipsis: true },
          { title: '上次同步', dataIndex: 'last_sync_at', render: fmtTime },
        ]}
      />
    </>
  )
}

function JobsTab({ kbId }: { kbId: string }) {
  const { data = [], isLoading, refetch } = useQuery({ queryKey: ['kb-jobs', kbId], queryFn: () => listJobs(kbId) })
  const done = data.filter((j) => j.stage === 'done').length
  return (
    <>
      <Space style={{ marginBottom: 12 }}>
        <Statistic title="任务总数" value={data.length} />
        <Statistic title="已完成" value={done} />
        <Button size="small" icon={<SyncOutlined />} onClick={() => refetch()}>刷新</Button>
      </Space>
      <Table
        rowKey="id"
        loading={isLoading}
        dataSource={data}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: 'Doc', dataIndex: 'doc_id', ellipsis: true },
          {
            title: '阶段',
            dataIndex: 'stage',
            render: (v: string) => <Tag color={v === 'done' ? 'green' : 'blue'}>{v}</Tag>,
          },
          { title: '重试', dataIndex: 'retries' },
          { title: '错误', dataIndex: 'error', render: (v?: string) => (v ? <Typography.Text type="danger">{v}</Typography.Text> : '-') },
          { title: '开始', dataIndex: 'started_at', render: fmtTime },
          { title: '结束', dataIndex: 'finished_at', render: fmtTime },
        ]}
      />
    </>
  )
}
