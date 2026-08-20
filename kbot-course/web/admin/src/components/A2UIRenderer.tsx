import {
  CheckCircleFilled,
  ClockCircleOutlined,
  CloseCircleFilled,
  CodeOutlined,
  LoadingOutlined,
  SafetyCertificateFilled,
} from '@ant-design/icons'
import { Alert, Button, Card, Divider, Space, Tag, Typography } from 'antd'
import type { ReactNode } from 'react'
import type { A2UIActionDefinition, A2UIComponent, A2UISurfaceModel } from '@/a2ui/protocol'
import { resolveDynamic } from '@/a2ui/protocol'

interface Props {
  surface: A2UISurfaceModel
  loading?: boolean
  onAction: (componentId: string, action: NonNullable<A2UIActionDefinition['event']>) => void
}

export function A2UIRenderer({ surface, loading, onAction }: Props) {
  const root = surface.components.root
  if (!root) return <Alert type="warning" message="A2UI surface 等待 root 组件" />
  const status = String(surface.dataModel.status ?? '')
  const statusStyle = {
    pending: {
      color: '#ad6800', border: '#ffd591', background: '#fffaf0', icon: <ClockCircleOutlined />,
    },
    approved: {
      color: '#0958d9', border: '#91caff', background: '#f0f7ff', icon: <LoadingOutlined spin />,
    },
    completed: {
      color: '#237804', border: '#b7eb8f', background: '#f6ffed', icon: <CheckCircleFilled />,
    },
    rejected: {
      color: '#a8071a', border: '#ffa39e', background: '#fff1f0', icon: <CloseCircleFilled />,
    },
  }[status] ?? {
    color: '#595959', border: '#d9d9d9', background: '#fafafa', icon: <ClockCircleOutlined />,
  }

  const render = (component: A2UIComponent, ancestors: Set<string>): ReactNode => {
    if (ancestors.has(component.id) || ancestors.size > 32) {
      return <Alert key={component.id} type="error" message="A2UI 组件树存在循环" />
    }
    const nextAncestors = new Set(ancestors).add(component.id)
    const renderChild = (id: string) => {
      const child = surface.components[id]
      return child ? render(child, nextAncestors) : <Alert key={id} type="warning" message={`缺少组件 ${id}`} />
    }

    switch (component.component) {
      case 'Text': {
        const content = String(resolveDynamic(component.text, surface.dataModel) ?? '')
        if (!content && (component.id === 'resolution' || component.id === 'result')) return null
        if (component.id === 'risk') {
          return <Tag key={component.id} color="red" icon={<SafetyCertificateFilled />}>{content}</Tag>
        }
        if (component.id === 'status') {
          return (
            <div
              key={component.id}
              aria-live="polite"
              style={{
                color: statusStyle.color,
                background: statusStyle.background,
                border: `1px solid ${statusStyle.border}`,
                borderRadius: 8,
                padding: '9px 12px',
              }}
            >
              <Space size={8}>{statusStyle.icon}<Typography.Text strong style={{ color: 'inherit' }}>{content}</Typography.Text></Space>
            </div>
          )
        }
        if (component.id === 'summary-value') {
          return (
            <div key={component.id} style={{ display: 'grid', gap: 7 }}>
              {content.split('\n').map((line) => (
                <Typography.Text key={line} style={{ fontSize: 15 }}>{line}</Typography.Text>
              ))}
            </div>
          )
        }
        if (component.id === 'tool-value') {
          return <Typography.Text key={component.id} code>{content}</Typography.Text>
        }
        if (component.id === 'arguments-value') {
          return (
            <details key={component.id} style={{ color: '#8c8c8c' }}>
              <summary style={{ cursor: 'pointer', userSelect: 'none' }}><CodeOutlined /> 查看技术参数</summary>
              <pre style={{
                margin: '10px 0 0', padding: 12, borderRadius: 8, overflow: 'auto',
                color: '#434343', background: '#f5f5f5', fontSize: 12, lineHeight: 1.6,
              }}>{content}</pre>
            </details>
          )
        }
        if (component.id === 'result') {
          return <Alert key={component.id} type="success" showIcon message={content} />
        }
        if (/^h[1-5]$/.test(component.variant ?? '')) {
          const level = Number(component.variant?.slice(1)) as 1 | 2 | 3 | 4 | 5
          return <Typography.Title key={component.id} level={level} style={{ margin: 0 }}>{content}</Typography.Title>
        }
        return (
          <Typography.Text key={component.id} type={component.variant === 'caption' ? 'secondary' : undefined}>
            <span style={{ whiteSpace: 'pre-wrap' }}>{content}</span>
          </Typography.Text>
        )
      }
      case 'Card':
        return (
          <Card
            key={component.id}
            size="small"
            styles={{ body: { padding: 20 } }}
            style={{
              overflow: 'hidden', borderColor: statusStyle.border, borderRadius: 12,
              boxShadow: '0 8px 24px rgba(0, 0, 0, 0.06)',
            }}
          >
            {component.child && renderChild(component.child)}
          </Card>
        )
      case 'Column':
        return <Space key={component.id} direction="vertical" size={12} style={{ width: '100%' }}>{component.children?.map(renderChild)}</Space>
      case 'Row':
        return (
          <Space
            key={component.id}
            wrap
            style={{
              width: '100%',
              justifyContent: component.id === 'title-row' ? 'space-between' : component.justify === 'end' ? 'flex-end' : undefined,
            }}
          >
            {component.children?.map(renderChild)}
          </Space>
        )
      case 'Divider':
        return <Divider key={component.id} style={{ margin: '8px 0' }} />
      case 'Button': {
        const action = component.action?.event
        if (status !== '' && status !== 'pending') return null
        return (
          <Button
            key={component.id}
            type={component.variant === 'primary' ? 'primary' : 'default'}
            danger={action?.name === 'approval.reject'}
            loading={loading}
            disabled={!action}
            onClick={() => action && onAction(component.id, action)}
          >
            {component.child ? renderChild(component.child) : component.id}
          </Button>
        )
      }
    }
  }

  return <div data-a2ui-surface={surface.surfaceId} data-a2ui-status={status}>{render(root, new Set())}</div>
}
