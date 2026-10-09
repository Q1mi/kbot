# 第 22 课：飞书创建应用与接入 kbot

> 课件来源：[22-end](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/docs/lesson-22-feishu-kbot.md)。 本课实验对应 `22-end`；执行文中的命令前，请保存当前工作并切换到该标签，再进入 `kbot-course/`。

本课创建飞书企业自建应用，使用官方 Go SDK 长连接接收消息，经统一 Runtime 调用第 21 课的物流助手，再回复原消息。重点包括应用配置、消息适配、会话身份、版本固定、重复投递和回复重试。

> 本课入口为 `cmd/lesson22-demo`，核心适配器为 `internal/integration/lark/bot.go` 和 `internal/integration/dialogue.go`。第 21 课的命令只演示 A2A 调用，本课单独承载飞书接入。第 23 课保留原有最终交付内容与代码快照。

## 1. 学习目标与前置准备

完成本课后，应能够创建和发布飞书机器人，配置 App ID 与 App Secret，追踪“消息接收 → 会话解析 → Runtime → 回复”的完整流程，并解释每个阶段的身份与完成状态。

先完成[第 21 课 A2A 实验](lesson-21-a2a-logistics.md)，保持 `make logistics-mock-run` 运行。飞书实验默认复用确定性物流模型，无需模型 Key；使用 `-live-model` 可切换到真实模型。账号、权限和网络连接需要在自己的飞书应用中验收。

## 2. 课堂实验：配置飞书自建应用并与 kbot 对话

### 2.1 选择运行入口

| 项目 | 独立演示 `make feishu-demo` | 完整主服务 `make run` |
| --- | --- | --- |
| 飞书接入实现 | `lark.Bot` + `Dialogue` | 同一套实现 |
| 控制面 | 自动创建内存 Workspace、Agent 和版本 | 使用已有 PostgreSQL Agent 与发布记录 |
| 物流工具 | 启动时自动注册 | 按目标 Agent 的工具版本执行 |
| 默认模型 | 确定性 `DemoModel` | 配置的真实大模型 |
| 真实模型切换 | `ARGS='-live-model'` | 主服务原有模型配置 |
| Runtime 装配 | 工具、长度/注入 Guard、内存审计 | 工具、Prompt/Model 配置、Skill、Guard、审批、审计 |
| 本次入门配置 | App ID、App Secret，启动本机 Mock | 原有主服务配置，加应用凭据和固定目标 |

先使用独立演示验证账号、订阅和消息链路，再按第 2.6 节接入已有主服务。

### 2.2 飞书后台准备

在飞书开放平台完成以下准备：

1. 创建**企业自建应用**，在应用能力中添加机器人。
2. 从“凭证与基础信息”取得 **App ID 和 App Secret**。
3. 开通下表中的应用身份权限，并将演示账号加入应用可用范围。

| 权限 | 用途 | 演示要求 |
| --- | --- | --- |
| `im:message.p2p_msg:readonly` | 接收用户发给机器人的单聊消息 | 单聊需要 |
| `im:message:send_as_bot` | 以机器人身份发送和回复消息 | 需要 |
| `im:message.group_at_msg:readonly` | 接收群聊中 @ 当前机器人的消息 | 群聊按需开通 |

权限与消息推送范围以[官方接收消息文档](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)为准，回复权限见[回复消息文档](https://open.feishu.cn/document/server-docs/im-v1/message/reply)。

本项目使用官方 Go SDK 长连接，运行电脑主动连接飞书即可接收事件，无需公网回调地址、Verification Token 或 Encrypt Key。保存长连接订阅方式前，应先运行客户端建立连接。[官方 Go SDK 事件接入](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-events)

### 2.3 在项目中配置凭据

以下命令均在 `kbot-course` 目录（含 Makefile）执行。首次创建本地配置文件：

```bash
cp examples/lesson22/.env.example examples/lesson22/.env
```

已有该文件时直接编辑，保留原配置。填写两项凭据：

```dotenv
KBOT_LARK_APP_ID='cli_你的应用ID'
KBOT_LARK_APP_SECRET='你的应用Secret'
```

`.env` 已被 Git 忽略；程序从环境变量加载配置，需要在运行终端中执行：

```bash
set -a
source examples/lesson22/.env
set +a
```

配置字段与代码中的作用如下：

| 环境变量 | 必填范围 | 作用 |
| --- | --- | --- |
| `KBOT_LARK_APP_ID` | 飞书入口 | 应用标识、SDK 建连和事件归属检查 |
| `KBOT_LARK_APP_SECRET` | 飞书入口 | 应用认证与 Token 获取 |
| `KBOT_LARK_TENANT_KEY` | 可选 | 将接收租户限定为指定值 |
| `KBOT_LLM_API_KEY` 或 `ARK_API_KEY` | 真实模型模式 | 模型凭据；前者优先 |
| `KBOT_LLM_BASE_URL`、`KBOT_LLM_MODEL` | 可覆盖默认值 | 模型地址与模型名称 |
| `KBOT_CHANNEL_WORKSPACE_ID` | 完整主服务开启渠道时 | 固定内部 Workspace |
| `KBOT_CHANNEL_AGENT_ID` | 完整主服务开启渠道时 | 固定内部 Agent |
| `KBOT_CHANNEL_AGENT_ENV` | 完整主服务可配置，默认 `prod` | 新会话使用的 Agent 环境 |

独立演示自行固定 Workspace、Agent 和 `dev`，无需填写最后三项。源码见 [config.go](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/config/config.go) 和 [main.go](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/cmd/lesson22-demo/main.go)。

### 2.4 启动、订阅、发布与收发验证

终端 A 保持物流服务运行：

```bash
make logistics-mock-run
```

终端 B 加载凭据后启动：

```bash
make feishu-demo
```

等待以下就绪日志：

```text
Feishu long connection ready; send a text message to the bot
```

然后回到飞书开放平台：

1. 在“事件与回调”选择**使用长连接接收事件**并保存。
2. 添加**接收消息 `im.message.receive_v1`** 事件。
3. 创建版本并发布，按企业要求完成管理员审核，确认权限及可用范围生效。
4. 在飞书客户端打开机器人单聊，发送 `查询订单 CB-1001 的物流信息`。

机器人应回复“演示物流数据”与“清关中”。终端可看到关联信息：

```text
Feishu Runtime finished message_id=om_... conversation_id=conversation-... agent_version_id=lesson21-agent-v1 status=completed
```

继续发送 `查询 CB-1002`，同一用户、同一聊天应复用 `conversation_id`。发送 `/new` 后，新问题使用新的 Conversation。

群聊实验需要开通群聊 @ 权限、将机器人加入群，再发送 `@机器人 查询 CB-1003`。代码只支持文本输入和文本回复；附件、卡片和流式消息更新留作扩展。

### 2.5 切换到真实模型

默认 `DemoModel` 会将每条普通文本转换为物流工具调用，所以发送“你好”会得到订单号提示。需要普通对话与按需工具选择时，在本地 `.env` 中配置模型 Key，重新加载环境变量后启动：

```bash
set -a
source examples/lesson22/.env
set +a
make feishu-demo ARGS='-live-model'
```

命令行 A2A 示例同样支持：

```bash
make a2a-demo ARGS='-live-model -message "查询 CB-1001"'
```

真实模式由配置的大模型决定是否调用工具。模型凭据只配置在本地环境中，App Secret 用于飞书认证，两组凭据分别服务于各自的调用链。

### 2.6 接到完整 kbot 主服务

先按前面课程准备 PostgreSQL、迁移、JWT、模型及 Sandbox Runner Token 等配置。创建目标 Agent，发布版本并提升到 `dev`。若首次准备 Agent，可先保持飞书凭据未配置，使用管理台或 API 完成这些步骤。

随后配置固定目标并重启主服务：

```bash
export KBOT_LARK_APP_ID='cli_你的应用ID'
export KBOT_LARK_APP_SECRET='你的应用Secret'
export KBOT_CHANNEL_WORKSPACE_ID='已有的Workspace ID'
export KBOT_CHANNEL_AGENT_ID='已有的Agent ID'
export KBOT_CHANNEL_AGENT_ENV='dev'
make run
```

[主服务装配](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/cmd/server/main.go) 会先调用 `ResolveVersion` 检查目标环境中存在发布版本，再把已有的 `agents` 和 `runtime` 交给 `Dialogue` 与 `Bot`。若选择默认 `prod`，按第 20 课流程完成评测和提升。

主服务使用目标 Agent 的工具集合。接入物流 Mock 时，按[第 21 课第 5.1 节](lesson-21-a2a-logistics.md)注册工具，将返回的工具版本 ID 绑定到 Agent 的新快照中并提升，再创建新会话验证。

**主服务当前的 Tool Registry 使用内存存储。** 重启后，通过现有 API 重新注册工具会生成新的版本 ID；需要发布引用新工具 ID 的 AgentVersion 并提升，渠道已有会话可用 `/new` 切换。若要求重启后继续执行原来的固定快照，需要持久化工具版本及凭据。

同一个飞书应用在课堂上运行一个接收进程，独立演示与完整主服务择一启动。多客户端会分配事件，当前内存会话映射无法跨进程共享。[官方长连接运行说明](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-events)

## 3. 消息渠道：建立可信身份，再进入统一 Runtime

### 3.1 长连接到 Runtime 的入口

```text
飞书 SDK 长连接
  → Bot.HandleEvent
  → 内存队列
  → Dialogue.Respond
  → Engine.ChatStream
  → Reply API 回复原消息
```

本课提供文本消息接收和回复。消息入口通过 SDK 的应用认证连接建立可信来源，再使用服务端固定的 Workspace 与 Agent 运行。

### 3.2 统一消息保存外部关联信息

[Message](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/integration/types.go) 的当前定义为：

```go
type Message struct {
    Source, EventID, WorkspaceID, UserID, ConversationID, Text string
    AppID, TenantKey, ChatID, MessageID string
}
```

飞书 Bot 将 SDK 事件转换为该对象：

| 内部字段 | 飞书事件来源 | 用途 |
| --- | --- | --- |
| `Source` | 固定为 `lark` | 渠道命名空间 |
| `AppID` | `header.app_id` | 验证应用与隔离身份 |
| `TenantKey` | `header.tenant_key` | 租户隔离 |
| `EventID` | `header.event_id` | 事件关联信息 |
| `MessageID` | `event.message.message_id` | 业务去重、回复目标 |
| `ChatID` | `event.message.chat_id` | 区分聊天上下文 |
| `UserID` | `event.sender.sender_id.open_id` | 外部发送者标识 |
| `Text` | 解析 `message.content` 中的 `text` | Runtime 输入 |

`WorkspaceID` 和 `ConversationID` 虽然存在于结构中，飞书 `Dialogue` 会使用服务端目标与内部会话映射，外部载荷无法覆盖它们。

### 3.3 两次身份转换

第一次转换在 `Bot.decode`，确认 SDK 事件属于预期应用，并规范化发送者和聊天信息：

- `header.app_id` 必须等于配置的 App ID。
- Header 租户不能为空；配置了 `KBOT_LARK_TENANT_KEY` 时，必须匹配。
- Sender 带有 `tenant_key` 时，必须与 Header 一致。
- 只接收 `sender_type=user`、`message_type=text`，聊天类型限 `p2p`、`group`。
- 群消息必须包含 mention；群聊实验依赖平台只授予并推送“@ 当前机器人”范围。
- 事件、消息、聊天及发送者 ID 必须存在。机器人 mention 被移除，用户 mention 转成显示名称，空文本被忽略。

第二次转换在 `Dialogue.Respond`，将外部身份映射为本地会话所有者。源码节选：

```go
identity, _ := json.Marshal([]string{
    message.Source, message.AppID, message.TenantKey,
    message.ChatID, message.UserID,
})
digest := sha256.Sum256(identity)
userID := "channel:" + message.Source + ":" + hex.EncodeToString(digest[:])
```

使用 JSON 编码避免简单分隔符拼接产生歧义。相同用户在不同应用、租户或聊天中会得到独立会话；同一群中的不同用户也分别保留上下文。

摘要用于构造有作用域的标识。可信来源由 SDK 连接和入口检查建立，业务操作资格仍需结合应用授权及内部权限模型。

### 3.4 固定目标、复用会话和版本更新

`Dialogue` 保存服务端配置的 Workspace、Agent、环境，以及“渠道身份 → Conversation”的内存映射。核心调用为：

```go
conversation, err := d.agents.ResolveConversation(
    ctx, d.workspaceID, userID, d.agentID,
    d.environment, session.conversationID,
)
```

首次消息由 Worker 处理时创建会话，解析当时环境版本。后续消息使用已有 Conversation，继续读取已固定的 AgentVersion。

```text
消息首次入队       dev → v1
Worker 首次处理    dev → v2，创建会话 C1，固定 v2
后续发布          dev → v3
同一身份继续对话   使用 C1，继续执行 v2
用户发送 /new     创建 C2，固定 v3
```

该例说明当前版本固定点位于 **Worker 创建 Conversation 时**。如果业务要求按接收时版本执行，需要在入队阶段保存版本或建立会话。

`/new` 只创建新会话并返回提示，本次不调用模型。映射空闲超过 24 小时后在后续请求中清理；上限 4096 项，满时淘汰最久未使用的映射。进程重启也会重新建立映射。新映射会重新解析环境版本，已有数据库会话记录不会因此被删除。

最终调用复用标准 Runtime：

```go
err = d.runtime.ChatStream(ctx, engine.ChatRequest{
    ConversationID:   conversation.ID,
    WorkspaceID:      d.workspaceID,
    UserID:           userID,
    Message:          message.Text,
    AgentEnvironment: d.environment,
}, emitter)
```

代码入口：[Bot](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/integration/lark/bot.go)、[Dialogue](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/integration/dialogue.go)、[会话服务](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/platform/agent/service.go)。

## 4. 飞书适配代码：接收、执行与回复三个阶段

### 4.1 SDK 回调快速入队

`Bot.Run` 启动 Worker，并装配长连接。源码关键部分为：

```go
handler := dispatcher.NewEventDispatcher("", "").
    OnP2MessageReceiveV1(b.HandleEvent)

client := larkws.NewClient(b.config.AppID, b.config.AppSecret,
    larkws.WithEventHandler(handler),
    larkws.WithLogLevel(larkcore.LogLevelWarn),
    larkws.WithOnReady(func() {
        log.Print("Feishu long connection ready; send a text message to the bot")
    }))
err := client.Start(ctx)
```

SDK 长连接回调需要在三秒内完成。`HandleEvent` 只进行验证、规范化、去重和非阻塞入队，耗时模型调用由 Worker 完成。[官方回调时限说明](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-events)

队列容量为 128。队列满或去重表达到容量时，回调返回错误；未成功接收的消息不写成已完成记录，后续投递仍可尝试入队。已在排队、处理中或已回复的重复消息直接确认。

### 4.2 消息去重键与会话键分别建模

同一条飞书消息可能被重复投递，业务去重依据 `message_id`，`event_id` 用于事件关联。[官方接收消息说明](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)

本地 `deliveryKey` 同时考虑应用和租户：

```go
identity, _ := json.Marshal([]string{
    message.AppID, message.TenantKey, message.MessageID,
})
digest := sha256.Sum256(identity)
return hex.EncodeToString(digest[:20])
```

| 标识 | 组成 | 解决的问题 |
| --- | --- | --- |
| 会话身份 | source + app + tenant + chat + user | 哪些消息共享上下文 |
| 消息去重键 | app + tenant + message | 哪些投递属于同一次输入 |
| 回复 UUID | 复用消息去重键，40 个十六进制字符 | 多次发送请求对应同一次回复 |
| A2A `messageId` | A2A Client 每次调用生成 | 标识远端消息 |

前两项由 kbot 维护，回复 UUID 还用于飞书 API 去重。A2A 消息 ID 当前独立生成，本地消息去重不会自动成为跨服务的业务幂等契约。

### 4.3 Worker 收集 Runtime 的回答和状态

Worker 顺序消费，每次 `Respond` 使用两分钟超时。`Dialogue` 在整个会话处理期间串行执行，避免同一会话历史交错写入。

`Reply` 保存以下结果：

```go
type Reply struct {
    Text, ConversationID, AgentVersionID, Status, ApprovalID string
}
```

收集器根据 Runtime 事件决定回复内容：

| 结果 | 处理方式 |
| --- | --- |
| `answer_done` | 收集最终回答 |
| `run_finished.status=blocked` | 返回安全规则拦截提示 |
| `approval_requested` 与 `awaiting_approval` | 返回等待审批提示及审批编号 |
| Runtime 返回 error | Bot 记录失败，回复通用错误提示 |
| 无文本结果 | 返回未生成文本回复的提示 |
| 文本超过 12000 个 Unicode 字符 | 保留前 12000 个字符并追加截断说明 |

`ChatStream` 返回 nil 时仍可能处于 `blocked` 或 `awaiting_approval`，因此调用方需要读取最终状态。Runtime 失败后，当前 Bot 也会缓存错误提示；在缓存有效期间，同一消息再次投递会复用该提示，用户可发送新消息重试。

### 4.4 使用 SDK 回复原消息

`SDKReplier` 通过官方客户端管理 Token，并调用：

```text
POST /open-apis/im/v1/messages/{message_id}/reply
```

源码节选：

```go
content, err := json.Marshal(map[string]string{"text": text})
if err != nil {
    return err
}
response, err := r.client.Im.Message.Reply(ctx,
    larkim.NewReplyMessageReqBuilder().
        MessageId(messageID).
        Body(larkim.NewReplyMessageReqBodyBuilder().
            MsgType("text").
            Content(string(content)).
            Uuid(uuid).
            Build()).
        Build())
```

`content` 是序列化后的 JSON 字符串。实现会检查网络错误、空响应和 `response.Success()`，HTTP 请求返回后仍需判断飞书业务错误码。

回复 UUID 最长 50 字符；持有相同 UUID 的请求在一小时内至多成功回复一条消息。本项目使用 40 字符摘要，发送重试保留同一内容与 UUID。[官方回复消息接口](https://open.feishu.cn/document/server-docs/im-v1/message/reply)

### 4.5 生成一次回答，再重试发送

消费记录 `delivery` 包含原始消息、缓存文本、`queued`、`done` 和过期时间。其状态对应以下过程：

```text
首次成功入队
  → queued=true
  → Worker 执行 Runtime，缓存 text
  → 发送回复，最多 3 次，重试间隔 1 秒、2 秒
      ├── 成功：queued=false，done=true
      └── 失败：queued=false，done=false，保留 text

同一消息再次投递
  → queued=true 或 done=true：直接确认
  → 有效记录中有缓存 text 且未完成：只重试发送
```

发送失败后的重试复用回答，从而避免再次执行已运行的模型和工具。这个保证受进程生命周期及缓存窗口约束，不能推广为跨重启的全局“恰好一次”执行。

### 4.6 确认、完成与可恢复性

本课需要分别观察三件事：

| 阶段 | 能确认什么 |
| --- | --- |
| SDK 回调成功 | 消息已被本进程接收或识别为重复 |
| Runtime 完成 | Agent 已产生回答或明确状态 |
| Reply API 成功 | 飞书已接受本次回复请求 |

当前限制如下：

- 去重记录上限 4096，处理结束后保留一小时；排队和处理中的记录不会被过期清理。
- 队列、去重、待发送回答和渠道会话映射均保存在进程内存中。
- 进程重启后可能丢失已确认但尚未执行或回复的消息。
- 三次发送均失败后，程序没有持久化补偿调度；未来是否收到重复投递由平台决定。
- 退出时取消连接和 Worker 上下文，等待 Worker 返回；当前不承诺排空队列。

可靠恢复需要持久化 Inbox、Outbox、业务唯一约束和会话映射，再建立消费重试、失败补偿及跨实例协调。审批完成后的主动通知也需要保存原渠道与待回复消息的关联。

## 5. 将版本、身份和治理串起来

### 5.1 本课完整消息链路

```text
飞书用户发送“查询 CB-1001”
  → SDK 认证连接接收事件
  → Bot 检查应用、租户和消息类型
  → 按 app / tenant / message 去重并入队
  → Dialogue 按 app / tenant / chat / user 找到会话
  → Conversation 固定 lesson21-agent-v1
  → AgentSnapshot 固定 logistics-a2a-tool-v1
  → Guard 与 ADK 工具调用链
  → Executor 校验 Schema，发送 logistics-v1
  → 外部 Mock 检查版本并返回演示物流数据
  → Runtime 产生 answer_done 与 run_finished
  → SDKReplier 使用稳定 UUID 回复原消息
```

独立演示装配了只读物流工具、Guard 和内存审计。完整主服务还装配审批、Skill 等能力；是否启用某项治理，取决于所使用的 Runtime 装配和固定快照。

### 5.2 身份来源与权限含义

| 身份或标识 | 来源 | 当前作用 |
| --- | --- | --- |
| HTTP 用户 | JWT 与 IAM | Workspace 访问资格、会话归属 |
| 飞书应用身份 | App ID、App Secret 与 SDK 连接 | 接收应用事件、调用回复 API |
| 飞书渠道用户 | 经入口检查的 app / tenant / chat / open ID | 会话隔离与审计 Actor |
| ToolVersion | 当前 Workspace 的已发布版本 | 本地能力、参数与目标约束 |
| A2A `ROLE_USER` | 消息协议字段 | 表达消息角色 |
| 远端认证主体 | 远端认可的凭据 | 真实外部服务的访问权限；当前 A2A 分支待接入 |

渠道用户尚未映射为内部 IAM 用户，当前固定 Agent 面向应用可用范围内的用户提供对话。访问订单、执行变更等业务授权，需要进一步关联内部用户或服务主体。

飞书 `tenant_key`、kbot Workspace 和 A2A 接口 `tenant` 分别属于各自系统。集成时应显式配置对应关系，并在相应边界进行校验。

### 5.3 审批与审计沿用什么

完整 Runtime 对敏感工具建立包含 Workspace、Conversation、ToolCallID、ToolVersionID 和参数的审批请求，并保存 checkpoint。恢复时重新检查原快照与获批工具版本，再恢复指定中断。

飞书 `Dialogue` 已能返回等待审批提示及编号，尚未实现飞书审批卡片、点击权限校验及审批完成后的自动回复。

Trace 与审计记录本地会话、AgentVersion、Actor 和工具调用。远端内部执行记录、跨服务 Trace 与版本回执属于远端协作契约，需要进一步贯通。

代码入口：[审批中断](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/runtime/engine/react.go)、[审批恢复](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/runtime/engine/resume.go)、[审计](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/platform/audit/ledger.go)。

## 6. 课堂验收、排查与练习

### 6.1 推荐代码阅读顺序

| 顺序 | 代码 | 阅读问题 |
| --- | --- | --- |
| 1 | [演示入口](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/cmd/lesson22-demo/main.go) | 飞书如何复用第 21 课的 Runtime？ |
| 2 | [演示装配](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/demo/lesson21/runtime.go) | Agent 和工具版本何时固定？ |
| 3 | [A2A Client](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/runtime/tooling/a2a.go)、[物流服务](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/demo/logistics/service.go) | Card、消息和远端版本如何对应？ |
| 4 | [飞书 Bot](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/integration/lark/bot.go) | 验证、去重、排队和回复如何连接？ |
| 5 | [Dialogue](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/integration/dialogue.go) | 外部身份如何建立固定版本会话？ |
| 6 | [主服务](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/cmd/server/main.go) | 演示装配与主服务装配有哪些差异？ |

### 6.2 自动化验收

在 `kbot-course` 目录（含 Makefile）运行：

```bash
make lesson21-test
make lesson22-test
```

检查渠道并发与数据竞争：

```bash
go test -race ./internal/demo/... ./internal/integration/... ./internal/config
```

需要联动复核已有 Runtime、IAM 和审批入口时运行：

```bash
go test ./internal/platform/agent ./internal/platform/tool \
  ./internal/runtime/engine ./internal/api/...
```

测试与课堂结论的对应关系如下：

| 测试 | 验证内容 |
| --- | --- |
| `TestRuntimeCallsExternalLogisticsThroughA2A` | 正式 Engine 调用本地 HTTP Mock、工具事件、四类订单结果和消息历史 |
| `TestLogisticsRejectsUnsupportedRemoteVersion` | 缺失或错误的远端版本被拒绝 |
| `TestDialoguePinsVersionAndSeparatesChannelIdentities` | 会话版本固定、应用/租户/聊天/用户隔离、`/new` 和执行状态 |
| `TestFeishuEventRuntimeA2AAndSDKReply` | 飞书事件 → Runtime → A2A → 官方 SDK Token/Reply API 的本地完整链路 |
| `TestReplyRetryReusesAnswerAndUUID` | 回复失败后复用回答与 UUID，Runtime 只执行一次 |
| `TestFeishuIdentityChecksAndQueueBackpressure` | 身份拒绝、忽略非目标消息、队列满后可重试 |
| `TestWorkerDeduplicatesConcurrentCallbacksAndStops` | 并发重复投递去重与 Worker 取消退出 |
| `TestSDKReplierReportsFeishuBusinessError` | 飞书业务错误码转为错误 |

测试文件见 [Runtime 实验测试](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/demo/lesson21/runtime_test.go)、[Dialogue 测试](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/integration/dialogue_test.go)、[飞书 Bot 测试](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/internal/integration/lark/bot_test.go)。协议和回复链路使用本机 HTTP 替身，测试无需飞书或模型凭据。真实账号的长连接建立、应用权限及发布效果需按第 2 节进行人工验收。

### 6.3 人工验收清单

| 操作 | 应观察到的结果 |
| --- | --- |
| 启动 Mock 后执行 `make a2a-demo` | 有工具调用事件，返回 CB-1001 清关状态 |
| 使用错误远端版本直接调用 `/a2a` | JSON-RPC `-32602` 错误 |
| 飞书单聊发送物流问题 | 原消息下收到物流文本回复 |
| 同一身份连续发送两个问题 | 日志中的 Conversation 相同 |
| `/new` 后继续发送问题 | Conversation 变化 |
| 另一用户或另一聊天发送问题 | 使用独立 Conversation |
| 使用 `-live-model` 后发送普通问题 | 由配置的大模型回答 |

### 6.4 常见问题排查

| 现象 | 优先检查 |
| --- | --- |
| A2A `connection refused` | Mock 是否运行，`-card-url` 与监听端口是否一致 |
| Card 同源或白名单错误 | Card 公布地址、调用地址和 Executor 允许主机 |
| `agent_version_id must be logistics-v1` | 工具参数、Schema 与 Mock 常量是否一致 |
| 飞书长连接未就绪 | App ID、App Secret、企业自建应用类型、本机外网访问 |
| 已就绪但未收到消息 | 事件订阅、权限发布、可用范围；群聊是否 @ 机器人 |
| Runtime 完成后没有飞书回复 | `Feishu reply API code=...`、发消息权限及原消息可见性 |
| 问候语得到订单号提示 | 当前使用 DemoModel；普通对话需 `-live-model` |
| 主服务提示目标环境无版本 | Workspace、Agent、环境与发布记录 |
| 主服务重启后工具版本缺失 | 内存工具注册表已清空，需要新版本装配或持久化恢复 |
| 已提升 Agent 但回答沿用旧配置 | 已有 Conversation 固定旧版；使用 `/new` 创建新会话 |

### 6.5 练习与参考要点

**练习一：会话版本固定。** 渠道会话固定 Agent v1 后，将环境提升到 v2，现有会话和 `/new` 创建的会话分别使用哪个版本？

参考要点：现有会话继续使用 v1，新的会话解析为 v2。Worker 首次处理时才创建会话，因此应明确版本固定时点。

**练习二：重复投递与新问题如何区分？** 同一飞书消息换了 `event_id`，与用户重新发送同样的文本，各会发生什么？

参考要点：前者在有效记录窗口内按 `message_id` 去重；后者有新的消息 ID，是一次新的输入。文本相同无法作为业务去重依据。

**练习三：何时固定 AgentVersion？** 消息入队时 `dev` 指向 v1，Worker 首次处理时指向 v2，当前实现选择哪个？

参考要点：新身份创建会话时选择 v2；已有身份继续使用原 Conversation。要求接收时语义时，应在入队阶段记录版本。

**练习四：回复超时会再次执行工具吗？** Runtime 已完成，发送回复失败，随后收到同一消息的再次投递。

参考要点：当前进程的有效缓存记录会复用回答和 UUID，只补发消息；缓存过期或重启后的恢复需要持久化状态支持。

**练习五：飞书用户可以审批吗？** 渠道回复中已经有审批编号，是否可以直接根据 `open_id` 调用审批接口？

参考要点：还需要映射内部主体并校验审批权限。机器人应用凭据、渠道发送者和管理员身份分别承担各自的职责。

### 6.6 参考资料

- [运行说明与凭据模板](https://github.com/Q1mi/kbot/blob/22-end/kbot-course/examples/lesson22/README.md)：完整演示操作。
- [A2A 规范](https://a2a-protocol.org/latest/specification/)：协议对象、JSON-RPC 和认证约定。
- [A2A 核心概念](https://a2a-protocol.org/latest/topics/key-concepts/)：Message、Task、Artifact 与 Context。
- [A2A 1.0 变更](https://a2a-protocol.org/latest/whats-new-v1/)：字段及方法兼容性。
- [飞书 Go SDK 事件处理](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-events)：长连接建立和事件回调。
- [飞书消息事件](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)：消息字段、权限与重复投递。
- [飞书回复消息](https://open.feishu.cn/document/server-docs/im-v1/message/reply)：回复结构、授权和 UUID 去重。
