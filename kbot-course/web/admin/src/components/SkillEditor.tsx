import { Row, Col, Typography, Card } from 'antd'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { CodeEditor } from './CodeEditor'

export function joinSkillMD(frontmatter: string, body: string): string {
  return `---\n${frontmatter.trim()}\n---\n${body}`
}

// 左 YAML frontmatter,右 Markdown body + 实时预览。
export function SkillEditor({
  frontmatter,
  body,
  onFrontmatter,
  onBody,
}: {
  frontmatter: string
  body: string
  onFrontmatter: (v: string) => void
  onBody: (v: string) => void
}) {
  return (
    <Row gutter={12}>
      <Col span={12}>
        <Typography.Text type="secondary">Frontmatter (YAML)</Typography.Text>
        <CodeEditor value={frontmatter} onChange={onFrontmatter} language="yaml" height={180} />
        <Typography.Text type="secondary" style={{ display: 'block', marginTop: 8 }}>
          Body (Markdown)
        </Typography.Text>
        <CodeEditor value={body} onChange={onBody} language="markdown" height={240} />
      </Col>
      <Col span={12}>
        <Typography.Text type="secondary">预览</Typography.Text>
        <Card size="small" style={{ height: 436, overflow: 'auto' }}>
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{body}</ReactMarkdown>
        </Card>
      </Col>
    </Row>
  )
}
