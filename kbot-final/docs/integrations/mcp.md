# MCP Tool 接入

kbot 的 `mcp_server` Tool Client 基于 `mark3labs/mcp-go v0.58.0` 和 Eino MCP Tool Adapter，实现 MCP 正式规范 `2025-11-25` 的两种标准传输协议：

- `streamable_http`：App/Worker 通过单一 MCP HTTP Endpoint 调用远端 Server；
- `stdio`：协议实现保留给单元测试和后续独立 Connector Runner，App/Worker 的控制面会拒绝该配置。

当前实现覆盖 Tool 调用需要的生命周期：

```text
initialize
  → notifications/initialized
  → tools/list（Eino `GetTools` 发现并筛选固定工具）
  → tools/call
  → transport close
```

Kbot 以 Tool Client 身份接入，当前没有注册 sampling、elicitation 等反向能力 Handler。

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

MCP SDK 与 Endpoint Policy 共同完成：

- 在初始化响应中读取 `MCP-Session-Id`；
- 在后续请求中发送 `MCP-Session-Id` 和 `MCP-Protocol-Version`；
- 处理 `application/json` 与请求级 `text/event-stream` 响应；
- 调用结束后关闭 transport/session；
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

- Eino MCP Tool Adapter 将 MCP Content 转换为模型可消费的 ToolMessage 文本；
- SDK 返回的协议错误会进入统一 Tool 错误回喂路径，Agent 可以据此调整调用。

参考：

- <https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle>
- <https://modelcontextprotocol.io/specification/2025-11-25/basic/transports>
- <https://modelcontextprotocol.io/specification/2025-11-25/server/tools>
