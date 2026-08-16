# Webhook Agent 接入

通用入口为 `POST /integrations/webhook`。该入口不使用用户 JWT，通过共享密钥签名、五分钟时间窗和 Redis nonce 去重保护。

请求头：

```text
Content-Type: application/json
X-Kbot-Timestamp: 1786550400
X-Kbot-Nonce: order-event-20260813-0001
X-Signature: <hex hmac sha256>
```

请求体必须显式指定工作空间，Runtime 会继续校验 Agent 与 Workspace 的归属：

```json
{
  "workspace_id": "workspace-id",
  "agent_id": "agent-id",
  "input": "请分析订单 TTS-20260813-1001 的履约异常",
  "user_id": "erp:operator-42"
}
```

签名原文按字节拼接：

```text
timestamp + "." + nonce + "." + raw_request_body
```

签名值为 `hex(HMAC-SHA256(KBOT_WEBHOOK_SECRET, 原文))`。时间戳使用 Unix 秒，服务端允许前后五分钟偏差。服务端会为 nonce 保存十分钟状态：执行失败会释放 nonce，调用方可使用原请求重试；执行成功会缓存回复，相同 nonce 会返回原结果并附带 `X-Idempotent-Replay: true`；同一 nonce 仍在执行时返回 409。签名失败返回 401，去重存储不可用返回 503。

生产环境使用至少 32 字符的随机 `KBOT_WEBHOOK_SECRET`，并在网关层增加 TLS、来源网络策略、请求速率限制与密钥轮换。
