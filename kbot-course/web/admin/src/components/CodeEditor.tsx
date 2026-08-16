import Editor from '@monaco-editor/react'
import { Spin } from 'antd'

// Monaco 薄封装(Prompt 模板 / Skill YAML+MD / Tool schema JSON 共用)。
// 注意:@monaco-editor/react 默认从 CDN 加载 monaco 运行时;离线环境编辑器不渲染但不影响构建。
export function CodeEditor({
  value,
  onChange,
  language = 'plaintext',
  height = 320,
  readOnly = false,
}: {
  value: string
  onChange?: (v: string) => void
  language?: string
  height?: number | string
  readOnly?: boolean
}) {
  return (
    <div style={{ border: '1px solid #f0f0f0', borderRadius: 6, overflow: 'hidden' }}>
      <Editor
        height={height}
        language={language}
        value={value}
        onChange={(v) => onChange?.(v ?? '')}
        loading={<Spin />}
        options={{
          readOnly,
          minimap: { enabled: false },
          fontSize: 13,
          scrollBeyondLastLine: false,
          automaticLayout: true,
          wordWrap: 'on',
        }}
      />
    </div>
  )
}
