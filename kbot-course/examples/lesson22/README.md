# 第 22 课示例：飞书创建应用与接入 kbot

本课的独立入口是 `cmd/lesson22-demo`。它复用第 21 课的物流 Runtime，并增加飞书长连接、会话映射、消息去重和文本回复。

先在一个终端执行 `make logistics-mock-run`，保持物流服务运行。在另一个终端按下文配置飞书应用并执行 `make feishu-demo`。默认使用确定性物流模型，只需飞书应用凭据即可验证消息链路；`-live-model` 支持真实模型。

## 1. 配置飞书企业自建应用

接入凭据为 **App ID 和 App Secret 两项**。本项目使用飞书官方 Go SDK 长连接接收事件，电脑能够主动访问飞书服务即可运行，无需公网回调地址、Verification Token 或 Encrypt Key。[官方长连接说明](https://open.feishu.cn/document/server-docs/event-subscription-guide/event-subscription-configure-/request-url-configuration-case)

### 1.1 创建应用与配置权限

1. 在飞书开放平台创建**企业自建应用**。
2. 在应用能力中添加**机器人**，设置名称与头像。
3. 在“凭证与基础信息”取得 App ID、App Secret。
4. 在权限管理中开通下表中的应用身份权限；群聊权限按需开通。
5. 将自己的账号加入应用可用范围。

| 权限 | 用途 | 本示例是否需要 |
| --- | --- | --- |
| `im:message.p2p_msg:readonly` | 接收用户发给机器人的单聊消息 | 单聊演示需要 |
| `im:message:send_as_bot` | 以机器人身份发送和回复消息 | 需要 |
| `im:message.group_at_msg:readonly` | 接收群聊中 @ 当前机器人的消息 | 群聊演示需要 |

只需订阅机器人单聊和群聊 @ 消息范围。代码忽略机器人发送的消息及非文本消息。权限与事件范围见[接收消息](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive)，回复授权见[回复消息](https://open.feishu.cn/document/server-docs/im-v1/message/reply)。

### 1.2 在项目中填写凭据并启动

```bash
# 仓库根目录。已有本地 .env 时直接编辑，保留现有配置。
cp examples/lesson22/.env.example examples/lesson22/.env
```

编辑 `examples/lesson22/.env`：

```dotenv
KBOT_LARK_APP_ID='cli_你的应用ID'
KBOT_LARK_APP_SECRET='你的应用Secret'
```

`examples/lesson22/.env` 已被 Git 忽略。程序从环境变量加载配置，在启动终端执行：

```bash
set -a
source examples/lesson22/.env
set +a
make feishu-demo
```

同时保持另一个终端中的 `make logistics-mock-run` 正在运行。进程启动后等待以下日志：

```text
Feishu long connection ready; send a text message to the bot
```

### 1.3 订阅事件、发布应用并发送消息

1. 保持程序运行，在开放平台的“事件与回调”中选择**使用长连接接收事件**并保存。平台提示未建立连接时，先检查上一步的连接日志。
2. 添加事件 **接收消息 `im.message.receive_v1`**。
3. 创建应用版本并发布；按企业配置完成管理员审核，确认权限和可用范围生效。
4. 在飞书中打开机器人单聊，发送：`查询订单 CB-1001 的物流信息`。
5. 机器人应回复带有“演示物流数据”的物流状态。

需要演示群聊时，将机器人加入群，在群中发送 `@机器人 查询 CB-1002`。同一用户在同一聊天中的后续消息复用会话；发送 `/new` 开启新会话。

完整链路：

```text
飞书消息
  → 官方 SDK 长连接
  → Bot.HandleEvent 校验应用和租户、按 message_id 去重、入队
  → Dialogue 解析渠道身份并创建或复用 Conversation
  → Engine.ChatStream 读取固定的 AgentSnapshot
  → query_logistics 固定工具版本
  → A2A AgentCard + SendMessage
  → 外部物流 Mock
  → answer_done
  → 官方 SDK 获取并缓存 tenant_access_token
  → Reply Message API 回复原消息
```

### 1.4 切换到真实大模型

在本地配置文件中填写 `ARK_API_KEY`，重新加载环境变量后启动：

```bash
set -a
source examples/lesson22/.env
set +a
make feishu-demo ARGS='-live-model'
```

此时普通问题由配置的大模型回答，物流问题由模型选择 `query_logistics` 工具。命令行也支持 `make a2a-demo ARGS='-live-model'`。真实模型调用产生对应供应商费用。

## 2. 将飞书接到完整 kbot 主服务

`make run` 已装配同一个长连接 Bot，并使用主服务已有的 Runtime，包括 Guard、审批、审计和已配置工具。

先按前面课程启动 PostgreSQL、完成迁移并配置主服务所需的 JWT、模型与 Sandbox Runner Token。通过管理台或 API 创建目标 Agent，发布版本并提升到 `dev`。然后增加：

```bash
export KBOT_LARK_APP_ID='cli_你的应用ID'
export KBOT_LARK_APP_SECRET='你的应用Secret'
export KBOT_CHANNEL_WORKSPACE_ID='已存在的Workspace ID'
export KBOT_CHANNEL_AGENT_ID='已存在的Agent ID'
export KBOT_CHANNEL_AGENT_ENV='dev'
make run
```

首次准备目标 Agent 时，可先保持飞书凭据未设置，使用主服务完成 Agent 创建和发布，再配置渠道并重启。启动时会检查目标环境中已存在发布版本。若使用默认 `prod`，按第 20 课流程完成评测与提升。

独立演示和主服务择一连接同一个飞书应用，避免多实例分配事件导致内存会话分散。主服务使用所配置 Agent 的工具集合；自动创建物流工具的代码位于独立演示包中。主服务工具注册表当前保存在内存，需按[第 21 课第 5.1 节](../../docs/lesson-21-a2a-logistics.md)的 API 注册工具并在 AgentSnapshot 中绑定版本。重启后，通过 API 重新注册会生成新的工具版本 ID，需要发布引用新 ID 的 AgentVersion 并提升，再用 `/new` 创建新会话。保留原固定快照的跨重启执行能力需要持久化工具版本及凭据。

## 3. 代码中的版本与身份边界

| 边界 | 实现 |
| --- | --- |
| 本地 Agent 版本 | 会话固定 `lesson21-agent-v1`；环境提升后，已有会话继续使用原版本 |
| 本地工具版本 | AgentSnapshot 固定 `logistics-a2a-tool-v1` |
| 远端物流版本 | 工具 Schema 的 `enum` 固定 `logistics-v1`，Mock 再校验该值 |
| A2A 网络范围 | 演示 Executor 仅允许 `127.0.0.1`、`localhost`；Card 中接口须与 Card 同源 |
| 飞书应用和租户 | 校验 `header.app_id`、租户非空和发送者租户一致；可用 `KBOT_LARK_TENANT_KEY` 限定租户 |
| 会话身份 | 使用 source、App ID、tenant、chat ID、open ID 的 JSON 编码摘要作为会话所有者 |
| Workspace 和 Agent | 来自服务端固定配置；消息正文无法选择其他运行目标 |
| 消息与回复去重 | 按应用、租户、`message_id` 去重；回复采用稳定 UUID |

`Dialogue` 在 Worker 首次处理消息时解析环境版本。会话在进程内空闲 24 小时、容量淘汰、进程重启或 `/new` 后重新创建；新会话重新解析环境指针。会话表上限 4096 项，满时淘汰最久未使用的映射。

此接入允许应用可用范围内的用户与固定 Agent 对话。渠道身份用于会话隔离与审计，尚未关联内部 IAM 用户及其业务权限。审批暂停和 Guard 拦截会返回说明文本；审批完成后的主动推送、飞书审批卡片及点击回调属于后续扩展。

队列上限 128，单 Worker 顺序消费，单次 Runtime 超时 2 分钟。成功入队后 SDK 即可确认事件。去重及回复缓存最多保留 4096 项，处理结束后保留 1 小时；回复最多尝试 3 次，间隔 1 秒、2 秒，复用回答和 UUID。失败记录遇到同一消息再次投递时只补发缓存回答。确认后的事件能否再次投递取决于平台，当前没有持久化补偿调度；进程重启会丢失队列、去重和映射状态。多实例和可靠恢复需补充持久化 Inbox、Outbox 及会话映射。

## 4. 代码入口与验收

| 文件 | 阅读内容 |
| --- | --- |
| [物流服务](../../internal/demo/logistics/service.go) | AgentCard、SendMessage、订单数据与远端版本检查 |
| [物流进程](../../cmd/logistics-mock/main.go) | 独立 HTTP 服务启动 |
| [演示 Runtime](../../internal/demo/lesson21/runtime.go) | 发布版本、工具 Schema、受控 Executor、演示模型 |
| [演示命令](../../cmd/lesson22-demo/main.go) | 飞书接入及真实模型切换 |
| [飞书 Bot](../../internal/integration/lark/bot.go) | 长连接、事件适配、异步消费、去重、重试和回复 |
| [渠道会话](../../internal/integration/dialogue.go) | 固定目标、身份范围、多轮会话与执行状态 |
| [主服务](../../cmd/server/main.go) | 将飞书接入正式 Runtime |

```bash
make lesson22-test
go test -race ./internal/demo/... ./internal/integration/... ./internal/config
```

自动化测试使用本地 HTTP 服务，覆盖真实 Engine → A2A → 物流 Mock，以及飞书事件 → Engine → A2A → 官方 SDK Token / Reply API 的链路。测试无需飞书或模型密钥。真实飞书长连接与企业权限发布，需要使用自己的应用按上述步骤验收。

| 现象 | 排查方式 |
| --- | --- |
| A2A connection refused | 确认 Mock 已启动，端口与 `-card-url` 一致 |
| 未出现长连接 ready | 检查两项凭据、应用类型和本机对飞书服务的网络访问 |
| 长连接已建立，收不到消息 | 检查消息事件订阅、权限、版本发布、用户可用范围；群聊需 @ 机器人 |
| Runtime 完成，飞书未显示回复 | 查看 `Feishu reply API code=...`，检查机器人发消息权限和应用发布状态 |
| 普通问候只返回订单号提示 | 当前处于确定性演示模式，配置模型 Key 并使用 `-live-model` |
| 主服务提示目标环境没有版本 | 检查固定 Workspace、Agent、环境与发布记录 |
| 回答过长 | 文本回复最多保留 12000 个 Unicode 字符，并追加截断说明 |

完整讲解见[第 22 课课件](../../docs/lesson-22-feishu-kbot.md)。
