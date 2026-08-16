# 飞书机器人接入

kbot 支持飞书事件订阅触发 Agent，并把最终文本回复到原会话；同一出站客户端也支持主动发送文本与卡片。配置留空时，飞书端点会失败关闭，其余课堂流程照常运行。

## 1. 建应用 + 开权限

1. 到[飞书开放平台](https://open.feishu.cn/)创建「企业自建应用」,拿 **App ID** / **App Secret**。
2. 开通权限:`im:message`(发消息)、`im:message:send_as_bot`;事件订阅再加 `im:message.receive_v1`。
3. 把机器人加进目标群,或拿到用户 `open_id`。

## 2. 配 .env

```bash
KBOT_LARK_APP_ID=cli_xxx
KBOT_LARK_APP_SECRET=xxx
KBOT_LARK_VERIFY_TOKEN=xxx        # 入站事件订阅校验 token(仅入站需要)
KBOT_LARK_ENCRYPT_KEY=xxx         # 推荐；签名校验 + 加密载荷解密
KBOT_LARK_AGENT_ID=<agent-uuid>   # 收到消息后运行的默认 Agent
```

`KBOT_LARK_APP_ID` 为空 → 出站自动禁用(`Enabled()==false`,`Send*` 返回 `ErrNotConfigured`),
`make seed` / 启动都不强制飞书。

## 3. 用法

### 出站(发消息)
```go
ob := lark.NewOutbound(cfg.LarkAppID, cfg.LarkAppSecret)
if ob.Enabled() {
    _ = ob.SendText(ctx, "open_id", "ou_xxx", "你的退款已处理 ✅")
    _ = ob.SendCard(ctx, "chat_id", "oc_xxx", lark.SimpleCard("退款审批", "**金额** 100 元\n点击批准"))
}
```
- `receiveIDType`:`open_id` / `user_id` / `union_id` / `email` / `chat_id`。
- `SendText` 内部把文本包成 `{"text":...}`(已正确 JSON 转义);`SimpleCard` 生成最简交互卡片 JSON,也可自己拼卡片传给 `SendCard`。

### 入站(事件 → Agent → 回复)

事件订阅回调指向：

```text
POST https://<你的域名>/integrations/lark/events
```

处理链路如下：

```text
飞书 URL challenge / im.message.receive_v1
  → Verification Token + X-Lark-Signature 校验
  → 配置 Encrypt Key 时解密 {"encrypt":"..."}
  → Redis event_id 去重
  → 写入 Asynq 持久化队列后返回 202
  → Worker 异步运行 KBOT_LARK_AGENT_ID
  → 缓存模型回复，失败任务自动重试
  → 使用 event_id 派生 UUID 回复原会话并防止重复发送
```

飞书官方要求事件回调及时响应，并建议使用 `event_id` 去重；加密策略开启后，HTTP body 会变为加密载荷。实现与说明可对照[飞书接收事件文档](https://open.feishu.cn/document/server-docs/event-subscription-guide/event-subscription-configure-/encrypt-key-encryption-configuration-case?lang=zh-CN)。

## 国内网络
飞书 API 在国内直连即可(`open.feishu.cn`);若用 Lark 国际版改 base 域名(SDK 选项)。

## 当前范围

- 入站覆盖 `im.message.receive_v1` 文本消息；图片、文件和富文本消息可继续扩展 `Translate`。
- 自动回复使用文本消息；卡片可通过 `SendCard` 主动发送，卡片交互回调仍走现有 A2UI/审批接口。
- Redis `event_id` 去重窗口为 24 小时；模型回复同样缓存 24 小时，出站重试会复用结果。
- Asynq 最多重试 8 次，最终失败任务进入归档队列，便于管理员检查和人工重放。
- 真实收发需要有效 App 凭证、已发布应用和已加入目标会话的机器人。
