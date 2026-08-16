# MCP Tool 接入

kbot 的 `mcp_server` Tool Client 实现 MCP 正式规范 `2025-11-25` 的两种标准传输协议：

- `streamable_http`：App/Worker 通过单一 MCP HTTP Endpoint 调用远端 Server；
- `stdio`：协议实现用于单元测试和独立 Connector Runner 边界，App/Worker 的控制面会拒绝该配置。

当前实现覆盖 Tool 调用需要的生命周期：

```text
initialize
  → notifications/initialized
  → tools/call
  → 关闭 HTTP session
```

客户端声明空 capabilities，因此不支持 MCP Server 反向发起 sampling、elicitation 等请求。

## stdio 安全边界

`stdio` 配置包含可执行文件、参数和环境变量。让 App 或 Worker 直接执行这类配置会把控制面写权限提升为宿主进程执行权限。当前 Endpoint Policy 会返回明确错误，要求改用 Streamable HTTP。

需要 stdio Server 时，应部署独立 Connector Runner，并至少落实命令 allowlist、固定镜像、非 root、只读文件系统、网络策略、CPU/内存/PID/超时、凭据注入和完整审计。Runner 再把能力以内部 Streamable HTTP 暴露给 kbot。

## Streamable HTTP 配置

```json
{
  "transport": "streamable_http",
  "url": "https://mcp.example.com/mcp",
  "tool_name": "get_weather",
  "protocol_version": "2025-11-25"
}
```

HTTP 客户端会：

- 在初始化响应中读取 `MCP-Session-Id`；
- 在后续请求中发送 `MCP-Session-Id` 和 `MCP-Protocol-Version`；
- 同时接受 `application/json` 和请求级 `text/event-stream` 响应；
- 调用结束后尝试用 `DELETE` 关闭 session。
- 校验 URL scheme、userinfo、DNS 解析和最终连接 IP；默认拒绝 loopback、私网、link-local、multicast 等地址，课堂内部服务需加入显式 host allowlist。

## 鉴权

Tool 的 `auth_config` 支持单 Header：

```json
{
  "header": "Authorization",
  "value": "Bearer token"
}
```

或多个 Header：

```json
{
  "headers": {
    "Authorization": "Bearer token",
    "X-Tenant-ID": "tenant-1"
  }
}
```

`auth_config` 在数据库中使用 AES-GCM 密文保存，列表与版本接口只返回 `has_auth`，不会回显明文。生产环境仍建议把根密钥交给外部 Secret Manager/KMS，并定期轮换。

## 返回值

- Text Content 会按顺序拼接；
- `structuredContent` 会序列化为 JSON 一并返回；
- `isError: true` 会转换为工具执行错误，让 Agent 有机会根据错误信息自愈。

参考：

- <https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle>
- <https://modelcontextprotocol.io/specification/2025-11-25/basic/transports>
- <https://modelcontextprotocol.io/specification/2025-11-25/server/tools>
