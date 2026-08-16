// 把后端 RFC3339 时间串格式化为本地可读串;空/非法返回 '-'。
export function fmtTime(s?: string): string {
  if (!s) return '-'
  const d = new Date(s)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN', { hour12: false })
}
