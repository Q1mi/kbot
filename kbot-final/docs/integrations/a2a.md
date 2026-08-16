# A2A Agent 接入

kbot 将远程 Agent 注册为 `source_type=a2a` 的 Tool，由 ReAct Engine 像调用普通工具一样调用它。

当前实现锁定 A2A `v1.0.1` 的 JSON-RPC binding：

- 从 AgentCard 的有序 `supportedInterfaces` 中选择第一个 `JSONRPC` 接口；
- 使用 PascalCase 方法名 `SendMessage`；
- 请求使用 `SendMessageRequest` 和 A2A v1 Message；
- 如果接口声明了 `tenant`，会写入每次请求；
- 如果接口声明了 `protocolVersion`，会发送 `A2A-Version` Header。

## Tool 配置

```json
{
  "card_url": "https://agent.example.com/.well-known/agent-card.json"
}
```

远端 AgentCard 至少需要：

```json
{
  "name": "logistics-agent",
  "supportedInterfaces": [
    {
      "url": "https://agent.example.com/a2a/v1",
      "protocolBinding": "JSONRPC",
      "protocolVersion": "1.0"
    }
  ]
}
```

## Tool 入参

简单文本调用：

```json
{
  "message": "查询订单 ORD-1001 的物流状态"
}
```

kbot 会转换为：

```json
{
  "message": {
    "role": "ROLE_USER",
    "parts": [
      {
        "text": "查询订单 ORD-1001 的物流状态"
      }
    ],
    "messageId": "<uuid>"
  }
}
```

高级调用方也可以直接传完整 Message：

```json
{
  "message": {
    "role": "ROLE_USER",
    "parts": [
      {
        "text": "继续处理"
      }
    ],
    "taskId": "task-123"
  },
  "configuration": {
    "returnImmediately": false
  }
}
```

缺少 `messageId` 时 kbot 会自动生成。

## 当前边界

- 只实现 JSON-RPC `SendMessage`；
- 当前交付范围不包含 `SendStreamingMessage`、`GetTask`、`SubscribeToTask` 和 Push Notification；
- 当前交付范围不包含 AgentCard JWS 签名验证；
- 认证 Header 使用 Tool 的 `auth_config` 配置。

参考：

- <https://a2a-protocol.org/latest/specification/>
- <https://a2a-protocol.org/latest/definitions/>
