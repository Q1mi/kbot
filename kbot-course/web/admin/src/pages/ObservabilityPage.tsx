import { Card, Typography, Space, Button, Tag, Descriptions, Alert, Spin, Row, Col } from 'antd'
import { LineChartOutlined, HeartOutlined, NodeIndexOutlined, LinkOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { getObservability } from '@/api/observability'

export function ObservabilityPage() {
  const { data, isLoading, isError } = useQuery({ queryKey: ['observability'], queryFn: () => getObservability() })

  if (isLoading) return <Spin />
  if (isError || !data) return <Alert type="error" showIcon message="无法获取可观测配置" />

  // 这些是后端进程暴露的运维端点(同源),新窗打开。
  const links = [
    { url: data.metrics_url, label: 'Prometheus 指标', icon: <LineChartOutlined />, desc: '/metrics 抓取端点' },
    { url: data.healthz_url, label: '存活检查', icon: <HeartOutlined />, desc: 'liveness' },
    { url: data.readyz_url, label: '就绪检查', icon: <NodeIndexOutlined />, desc: 'readiness' },
  ]

  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        可观测
      </Typography.Title>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        {links.map((l) => (
          <Col span={8} key={l.url}>
            <Card>
              <Space direction="vertical">
                <Space>
                  {l.icon}
                  <Typography.Text strong>{l.label}</Typography.Text>
                </Space>
                <Typography.Text type="secondary">{l.desc}</Typography.Text>
                <Button type="link" href={l.url} target="_blank" icon={<LinkOutlined />} style={{ paddingLeft: 0 }}>
                  {l.url}
                </Button>
              </Space>
            </Card>
          </Col>
        ))}
      </Row>

      <Card title="链路追踪(OpenTelemetry GenAI)">
        <Descriptions column={1} size="small" bordered>
          <Descriptions.Item label="状态">
            {data.traces_enabled ? <Tag color="green">已启用</Tag> : <Tag>未配置</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="OTLP Endpoint">{data.otlp_endpoint || '—(设 KBOT_OTLP_ENDPOINT 启用)'}</Descriptions.Item>
        </Descriptions>

        {data.traces_enabled ? (
          <Alert
            style={{ marginTop: 16 }}
            type="success"
            showIcon
            message="Trace 已导出到 OTLP collector"
            description={
              <Space direction="vertical">
                <Typography.Text>
                  Span 导出至 <code>{data.otlp_endpoint}</code>。在 Langfuse / Jaeger / Grafana Tempo 里按 trace_id 查看
                  LLM 调用、Tool Call、Skill 触发的完整链路。
                </Typography.Text>
                {/iframe-friendly|langfuse|grafana/i.test(data.otlp_endpoint) && (
                  <Button type="primary" href={data.otlp_endpoint} target="_blank" icon={<LinkOutlined />}>
                    打开追踪后端
                  </Button>
                )}
              </Space>
            }
          />
        ) : (
          <Alert
            style={{ marginTop: 16 }}
            type="info"
            showIcon
            message="链路追踪未配置"
            description="设置 KBOT_OTLP_ENDPOINT(如指向 Langfuse / Jaeger / Tempo 的 OTLP 端点)后重启即可导出 GenAI span。"
          />
        )}
      </Card>
    </div>
  )
}
