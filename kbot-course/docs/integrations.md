# 第 21 课集成契约

## MCP

`source_type=mcp_server` 的 `endpoint_config`：

```json
{
  "transport": "streamable_http",
  "url": "https://mcp.example.com/mcp",
  "tool_name": "get_weather",
  "protocol_version": "2025-11-25"
}
```

Runtime 按 `initialize → notifications/initialized → tools/call` 执行，传递 `MCP-Session-Id`，同时接受 JSON 与 SSE 响应。

## A2A

`source_type=a2a` 的 `endpoint_config`：

```json
{
  "card_url": "https://agent.example.com/.well-known/agent-card.json"
}
```

Runtime 从 AgentCard 的有序 `supportedInterfaces` 中选择首个 `JSONRPC` 接口，再发送 A2A v1.0.1 `SendMessage`。请求 Message 使用 `ROLE_USER`、`parts` 和 `messageId`。

MCP、A2A、REST Tool 的目标 Host 均需出现在 `KBOT_TOOL_ALLOWED_HOSTS`，跳转后的目标也会重新校验。

## 入站 Webhook

请求头：

```text
X-Webhook-Timestamp: <Unix seconds>
X-Webhook-Nonce: <unique nonce>
X-Signature: HMAC-SHA256(secret, timestamp + "." + nonce + "." + body)
```

入口为 `POST /api/v1/integrations/webhook`。时间戳有效窗口为 5 分钟，同一签名与 nonce 只接收一次。

飞书入口为 `POST /api/v1/integrations/lark/events`，使用 `X-Lark-Request-Timestamp`、`X-Lark-Request-Nonce`、`X-Lark-Signature`，并执行相同的时效与防重放检查。
