# 第 21 课：A2A 协议与外部物流服务示例

本课讲解 A2A 协议，以及如何通过统一 Runtime 调用外部 Agent。课堂使用一个独立运行的物流 Mock，完成“用户问题 → 固定版本 Agent → A2A Tool → 外部物流服务 → 回答”的调用链。

> 本课代码入口为 `cmd/lesson21-demo` 和 `cmd/logistics-mock`。协议采用当前客户端实现的 A2A 1.0 风格同步 JSON-RPC 子集。第 22 课将在此基础上接入飞书消息，第 23 课继续保留原有最终交付内容及代码快照。

## 1. 学习目标与实验准备

完成本课后，应能够解释 AgentCard、Message、Task 等协议对象，注册固定的 A2A 工具版本，追踪 Runtime 到外部服务的调用，并区分本地版本、远端业务版本与协议版本。

先掌握前面课程的 ToolVersion、Conversation、Guard 与审计。独立演示只需要 Go 环境和本机端口；首次运行需下载 Module 依赖。默认使用确定性模型，无需数据库、JWT、Sandbox Runner 或模型 Key。

在 `kbot-course` 目录（含 Makefile）打开两个终端：

```bash
# 终端 A：外部物流服务
make logistics-mock-run
# 终端 B：kbot Runtime 调用外部服务
make a2a-demo
```

真实模型模式使用 `make a2a-demo ARGS='-live-model'`，需先配置 `KBOT_LLM_API_KEY` 或 `ARK_API_KEY`。完整运行说明见[第 21 课示例](../examples/lesson21/README.md)。

## 2. 统一 Runtime 的版本与身份基础

### 2.1 身份字段由入口填充

`ChatRequest` 定义了 Runtime 需要的运行上下文：

```go
type ChatRequest struct {
    ConversationID   string `json:"conversation_id"`
    Message          string `json:"message"`
    WorkspaceID      string `json:"-"`
    UserID           string `json:"-"`
    AgentEnvironment string `json:"agent_env,omitempty"`
}
```

`WorkspaceID` 和 `UserID` 使用 `json:"-"`，HTTP 请求体无法通过常规 JSON 解码为这两个字段赋值。入口在完成认证和归属校验后填入可信上下文。

普通 HTTP 对话依次经过：

1. `Auth` 解析 JWT，得到用户 ID。
2. `Workspace` 读取 `X-Workspace-ID`，查询用户在该 Workspace 的角色，并检查操作权限。
3. `ResolveConversation` 创建会话或检查已有会话归属。
4. 路由填充 `WorkspaceID`、`UserID`，调用 `ChatStream`。

`X-Workspace-ID` 表达用户选择的工作空间；访问资格由 IAM 的成员关系和角色规则决定。当前规则允许 member 运行能力、editor 修改控制面，审批操作要求 owner/admin。

代码入口：[身份中间件](../internal/api/middleware/auth.go)、[对话路由](../internal/api/router.go)、[ChatRequest 与 ChatStream](../internal/runtime/engine/chat.go)。

### 2.2 已有会话绑定用户、Workspace 和 Agent

`ResolveConversation` 对已有会话执行以下检查：

```go
if err != nil ||
    conversation.WorkspaceID != workspaceID ||
    conversation.UserID != userID ||
    conversation.AgentID != agentID {
    return nil, fmt.Errorf("conversation %s not found", conversationID)
}
```

仅知道 Conversation ID，无法通过这个入口访问其他用户或其他 Workspace 的会话。

当前用户归属校验主要放在入口服务层。`ChatStream` 会检查请求 Workspace 与快照 Workspace，但没有再次比较会话用户与请求用户；部分 Workspace 比较还以字段非空为条件。新增入口必须保证身份上下文完整，并建立相应归属检查。

代码入口：[Agent Service](../internal/platform/agent/service.go)。

### 2.3 环境指针在创建会话时解析

创建会话时，平台将 `(WorkspaceID, AgentID, Environment)` 解析为具体 AgentVersion，并写入 Conversation。

```text
创建时：Agent 的 prod 指针 → agent-v3 → Conversation A
升级后：Agent 的 prod 指针 → agent-v4 → Conversation B

Conversation A 后续执行继续读取 agent-v3
Conversation B 执行读取 agent-v4
```

`ResolveSnapshot` 从会话的 `AgentVersionID` 加载快照，检查版本 ID 和 Workspace 的一致性。运行时根据快照解析 Prompt、Model Profile、Tool 和 Skill 的版本引用。

PostgreSQL 的 Conversation 使用 `(agent_version_id, workspace_id, agent_id)` 复合外键约束版本归属。服务层和数据库层共同维护这条关系。

固定配置版本提供追溯基础。模型随机性、外部业务数据、远端服务实现和动态 Guard 规则仍可能影响执行结果。

代码入口：[ResolveSnapshot](../internal/runtime/engine/engine.go)、[Agent PostgreSQL Store](../internal/platform/agent/postgres_store.go)、[数据库约束](../migrations/000001_core.up.sql)。

## 3. ToolVersion：外部能力的本地契约

### 3.1 统一配置与绑定

ToolVersion 保存 `WorkspaceID`、`SourceType`、`InputSchema`、`Endpoint`、`AuthConfig`、`Sensitive` 和 `Published` 等信息。

运行时从 AgentSnapshot 的 `ToolVersionIDs` 绑定工具：

```go
bindings, err = e.tools.Bind(
    ctx, snapshot.WorkspaceID, snapshot.ToolVersionIDs,
)
```

执行包装器将绑定结果带入执行器：

```go
result, err := t.executor.Execute(toolContext, tooling.Call{
    WorkspaceID:    t.workspaceID,
    ToolVersionID:  t.binding.VersionID,
    Arguments:      []byte(arguments),
    IdempotencyKey: "react:" + callID,
})
```

Workspace 和本地 ToolVersion 来自运行时绑定；模型提供工具参数。注册表拒绝覆盖已有版本 ID，`Resolve` 要求版本属于目标 Workspace 且已发布。

### 3.2 执行前后的共同检查

| 检查 | 执行位置 | 作用 |
| --- | --- | --- |
| 版本归属、发布状态 | `Registry.Resolve` | 确定当前 Workspace 可用的注册版本 |
| 参数 JSON Schema | `Executor.Execute` | 限制参数结构、类型和取值 |
| Skill 工具授权 | ADK 工具中间件 | 应用激活 Skill 的权限约束 |
| 敏感调用审批 | ADK 工具中间件 | 暂停并保存获批对象与 checkpoint |
| Endpoint、主机、重定向 | Executor、A2A Client | 控制外部访问目标 |
| Trace 与工具审计 | `bindingTool` | 记录调用名称、版本、调用 ID 和行为归属 |

审批发生在 ADK 工具调用链中。直接使用 `Executor.Execute` 的新入口，需要另行建立授权和审批流程。

`IdempotencyKey` 当前在 REST 分支作为请求头发送；A2A 分支尚未贯通这个键。远端写操作需要约定独立的业务幂等机制。

### 3.3 凭据与网络边界

Registry 注册时加密 `AuthConfig`，解析版本时解密。REST 分支从配置中取得认证请求头。模型获得的 ToolInfo 包含名称、说明和参数 Schema。

Executor 使用主机白名单，并限制跨源重定向；标准 HTTP Transport 在拨号时检查主机并解析地址。当前服务器白名单为 `crossborder-sim`、`localhost`、`127.0.0.1`，访问其他服务需要调整受控配置。

白名单控制网络目标，Workspace 控制注册版本的归属。这两项检查覆盖不同范围。代码允许显式列入白名单的内部地址，不能据此推导出“所有内网地址都会被阻止”。

当前服务器中的 Tool Registry 使用内存存储。通过 API 重建工具会获得新版本 ID，原快照的引用不会自动变化。长期保留版本及凭据，需要补齐持久化和密钥管理，以配合已经持久化的 Agent 快照。

代码入口：[Tool Registry](../internal/platform/tool/registry.go)、[Executor](../internal/runtime/tooling/executor.go)、[ADK 包装器与审批](../internal/runtime/engine/react.go)、[服务器装配](../cmd/server/main.go)。

## 4. A2A 协议：向独立 Agent 委托任务

### 4.1 A2A 适用的调用场景

A2A，即 Agent2Agent，为独立 Agent 系统提供能力发现、消息交互和任务协作的约定。调用方提交目标，远端自行组织模型、工具和业务流程，再返回消息或任务结果。[A2A 官方概述](https://a2a-protocol.org/latest/)

| 需要解决的问题 | 本课使用的机制 | 示例 |
| --- | --- | --- |
| 调用明确的工具能力 | REST Tool | 查询库存、读取订单 |
| 向独立系统委托任务 | A2A | 请求物流 Agent 查询或分析物流 |
| 将外部用户消息接入对话 | 渠道适配器 | 飞书消息进入 kbot |

本课物流 Mock 用固定数据演示 A2A 通信。它作为独立进程提供服务，kbot 通过 HTTP 获取 Card 和发送消息。将该进程替换为真实物流 Agent 时，本地 Runtime 继续使用已经绑定的工具接口。

### 4.2 六个核心对象

| 对象 | 含义 | 物流场景 |
| --- | --- | --- |
| AgentCard | 能力、接口及认证要求的声明 | 物流 Agent 的服务说明 |
| Message | 一次消息交互的数据 | “查询 CB-1001” |
| Part | 消息或产物中的内容单元 | 文本、文件、结构化数据 |
| Task | 有标识和生命周期的工作 | 长时间运行的物流诊断任务 |
| Artifact | 任务产生的成果 | 诊断报告、结构化物流结果 |
| Context | 关联消息或任务的上下文 | 同一订单的连续协作 |

`messageId`、`taskId` 和 `contextId` 分别标识消息、任务和上下文。本地 `Conversation.ID` 属于 kbot 的会话模型，跨系统关联需要单独保存映射。[官方核心概念](https://a2a-protocol.org/latest/topics/key-concepts/)

### 4.3 读取本课实际提供的 AgentCard

启动 `make logistics-mock-run` 后，访问：

```text
GET http://127.0.0.1:8082/.well-known/agent-card.json
```

返回以下 Card，JSON 字段排列顺序以实际响应为准：

```json
{
  "name": "Kbot 物流 Mock Agent",
  "description": "查询 CB-1001、CB-1002、CB-1003 的演示物流信息",
  "version": "logistics-v1",
  "supportedInterfaces": [
    {
      "url": "http://127.0.0.1:8082/a2a",
      "protocolBinding": "JSONRPC",
      "protocolVersion": "1.0"
    }
  ],
  "capabilities": {
    "streaming": false,
    "pushNotifications": false
  },
  "defaultInputModes": ["text/plain"],
  "defaultOutputModes": ["text/plain"],
  "skills": [
    {
      "id": "shipment_lookup",
      "name": "物流查询",
      "description": "按订单号查询物流状态",
      "tags": ["logistics"]
    }
  ]
}
```

本课客户端从 `supportedInterfaces` 选择 JSONRPC 接口。Card 的 `skills` 是远端能力说明；kbot 的 `SKILL.md` 是本地执行配置。二者独立维护。[官方能力发现](https://a2a-protocol.org/latest/topics/agent-discovery/)

通用客户端还支持读取接口中的 `tenant` 并放入请求参数。本课 Mock 没有声明该字段。远端 `tenant` 的路由和授权语义由远端定义，不能直接推导出本地 Workspace 访问权限。[官方多租户说明](https://a2a-protocol.org/latest/topics/multi-tenancy/)

### 4.4 SendMessage 请求与同步响应

本课采用 A2A 1.0 风格的 `SendMessage`、`ROLE_USER` 和 `ROLE_AGENT`。代码注释引用 v1.0.1 规范，Mock 声明和检查的请求头为 `A2A-Version: 1.0`。阅读其他版本示例时，应核对方法名、字段与接口声明。[官方版本变更说明](https://a2a-protocol.org/latest/whats-new-v1/)

实际可发送的请求体为：

```json
{
  "jsonrpc": "2.0",
  "id": "rpc-demo-1",
  "method": "SendMessage",
  "params": {
    "message": {
      "role": "ROLE_USER",
      "messageId": "msg-demo-1",
      "parts": [{"text": "查询 CB-1001"}]
    },
    "metadata": {"agent_version_id": "logistics-v1"}
  }
}
```

Mock 返回即时消息：

```json
{
  "jsonrpc": "2.0",
  "id": "rpc-demo-1",
  "result": {
    "message": {
      "role": "ROLE_AGENT",
      "messageId": "reply-msg-demo-1",
      "contextId": "logistics-msg-demo-1",
      "parts": [
        {"text": "【演示物流数据】订单 CB-1001，运单 KB20260910001：清关中。当前位置：洛杉矶海关。申报资料正在审核，请等待承运商确认放行。"}
      ],
      "metadata": {"executed_agent_version_id": "logistics-v1"}
    }
  }
}
```

`metadata.agent_version_id` 与 `executed_agent_version_id` 是本项目的业务版本约定。A2A 提供消息和 Metadata 的协议结构；本课客户端和 Mock 共同落实版本参数的含义。[A2A 规范](https://a2a-protocol.org/latest/specification/)

### 4.5 Task、流式事件与当前实现范围

耗时工作可以返回 Task，通过状态更新表达执行、等待输入、等待授权、完成或失败；Artifact 承载成果。任务查询、SSE 更新和推送通知需要双方支持相应操作与能力。[任务生命周期](https://a2a-protocol.org/latest/topics/life-of-a-task/)、[流式与异步交互](https://a2a-protocol.org/latest/topics/streaming-and-async/)

| 能力 | 本课代码 |
| --- | --- |
| 读取 Card、选择 JSONRPC 接口 | 已实现 |
| `SendMessage` 和 `result.message.parts[].text` | 已实现 |
| 物流 Mock 的远端业务版本检查 | 已实现 |
| 标准 `result.task`、Artifact 解析 | 待扩展 |
| 远端 Context 复用、任务查询与取消 | 待扩展 |
| SSE、推送通知、Card 签名验证 | 待扩展 |
| A2A 认证凭据、执行版本回执校验 | 待扩展 |

`A2AResult` 中的 `task_id`、`status`、`content` 是本地结果字段，不能据此认定客户端已经支持完整标准 Task。当前 `A2AMessage` 也未保存 `contextId` 和回复 Metadata。

## 5. 课堂实验：Runtime 通过 A2A 查询外部物流

### 5.1 将 A2A 服务注册为本地工具版本

打开 [runtime.go](../internal/demo/lesson21/runtime.go)。独立演示在启动时直接调用 `Registry.Register`，固定以下标识：

| 对象 | 演示值 |
| --- | --- |
| Workspace | `lesson21-workspace` |
| AgentVersion | `lesson21-agent-v1` |
| ToolVersion | `logistics-a2a-tool-v1` |
| 模型可调用的工具名 | `query_logistics` |
| 远端业务版本 | `logistics-v1` |
| Agent 环境 | `dev` |

工具配置使用 `SourceType: "a2a"`、Card 地址和以下 InputSchema：

```json
{
  "type": "object",
  "properties": {
    "message": {"type": "string", "minLength": 1},
    "agent_version_id": {"type": "string", "enum": ["logistics-v1"]}
  },
  "required": ["message", "agent_version_id"],
  "additionalProperties": false
}
```

随后发布 AgentSnapshot，将 `ToolVersionIDs` 固定为 `[]string{ToolVersionID}`，再将 AgentVersion 提升到 `dev`。创建 Conversation 时，环境指针解析为这个固定版本。

完整主服务可通过 `POST /api/v1/tools` 注册同一服务。请求需要有效 JWT 和具有控制面写权限的 Workspace 上下文，请求体如下：

```json
{
  "name": "query_logistics",
  "source_type": "a2a",
  "description": "查询 CB-1001、CB-1002、CB-1003 的演示物流，agent_version_id 固定为 logistics-v1",
  "schema_json": "{\"type\":\"object\",\"properties\":{\"message\":{\"type\":\"string\",\"minLength\":1},\"agent_version_id\":{\"type\":\"string\",\"enum\":[\"logistics-v1\"]}},\"required\":[\"message\",\"agent_version_id\"],\"additionalProperties\":false}",
  "endpoint_config": "{\"card_url\":\"http://127.0.0.1:8082/.well-known/agent-card.json\"}",
  "auth_config": "",
  "sensitive": false
}
```

该 API 自动生成工具版本 `id`。取得返回值后，将它填入 Agent 配置的 `tool_ids` 并发布新版本；这个 API 字段映射到快照中的 `ToolVersionIDs`。自动生成的 ID 与独立演示常量分别属于各自的运行环境。

### 5.2 DemoModel 如何驱动真实工具执行

`DemoModel` 实现 Eino 的 `Generate` 和 `Stream` 接口，遵循两步执行：

1. 收到用户文本时，生成 `query_logistics` 的 tool_call，参数包含文本和 `logistics-v1`。
2. 收到 Tool 消息时，解析 `tooling.A2AResult.Content`，生成最终回答。

每次 tool_call 使用新的调用 ID。模型输出随后进入正式 ADK 工具执行链：

```text
DemoModel / 真实模型
  → query_logistics tool_call
  → 绑定到 logistics-a2a-tool-v1
  → Executor.Execute 校验 Workspace、版本、Schema
  → A2AClient.Send
  → GET AgentCard
  → 选择 JSONRPC，校验主机和同源
  → POST /a2a，发送 SendMessage
  → 读取 result.message.parts 中的文本
  → tooling.Result → 模型 → answer_done
```

确定性模型让课堂验收不依赖模型 Key。网络调用、会话持久化接口、工具绑定、Guard 和内存审计仍使用项目已有实现。`-live-model` 会将模型替换为 `llm.NewGateway(cfg)`。

### 5.3 启动两个独立进程

在 `kbot-course` 目录（含 Makefile）打开两个终端。

终端 A，启动外部物流 Mock：

```bash
make logistics-mock-run
```

默认监听 `127.0.0.1:8082`，提供 `/healthz`、`/.well-known/agent-card.json` 和 `/a2a`。

终端 B，运行 kbot：

```bash
make a2a-demo
```

关键输出如下；调用 ID 每次变化，日志与标准输出的显示顺序可能略有差异：

```text
model mode=demo (deterministic tool call; no LLM API key required)
workspace=lesson21-workspace agent=agent-1 agent_version=lesson21-agent-v1 tool_version=logistics-a2a-tool-v1 remote_version=logistics-v1
问题：查询订单 CB-1001 的物流信息
[run_started] {"agent_version_id":"lesson21-agent-v1","conversation_id":"conversation-2"}
[tool_started] {"call_id":"logistics-...","name":"query_logistics"}
[tool_finished] {"call_id":"logistics-...","name":"query_logistics"}
回答：【演示物流数据】订单 CB-1001，运单 KB20260910001：清关中。当前位置：洛杉矶海关。申报资料正在审核，请等待承运商确认放行。
[run_finished] {"status":"completed"}
```

### 5.4 课堂验证：正常查询、未知订单与版本拒绝

切换订单：

```bash
make a2a-demo ARGS='-message "查询 CB-1002"'
make a2a-demo ARGS='-message "查询 CB-1003"'
make a2a-demo ARGS='-message "查询 CB-9999"'
```

| 输入 | 预期结果 |
| --- | --- |
| `CB-1001` | 清关中，洛杉矶海关 |
| `CB-1002` | 运输中，上海转运中心 |
| `CB-1003` | 已签收，深圳南山 |
| `CB-9999` | 未找到演示订单 |
| 不含订单号的普通文本 | 提示提供三个演示订单号之一 |

直接检查协议接口：

```bash
curl -sS http://127.0.0.1:8082/.well-known/agent-card.json

curl -sS http://127.0.0.1:8082/a2a \
  -H 'Content-Type: application/json' \
  -H 'A2A-Version: 1.0' \
  -d '{"jsonrpc":"2.0","id":"rpc-demo-1","method":"SendMessage","params":{"message":{"role":"ROLE_USER","messageId":"msg-demo-1","parts":[{"text":"查询 CB-1001"}]},"metadata":{"agent_version_id":"logistics-v1"}}}'
```

将最后一条命令的 `agent_version_id` 改为 `logistics-v2`，Mock 返回：

```json
{
  "jsonrpc": "2.0",
  "id": "rpc-demo-1",
  "error": {
    "code": -32602,
    "message": "agent_version_id must be logistics-v1"
  }
}
```

该错误通过 JSON-RPC 响应体表达，HTTP 状态仍为 200。`A2AClient.Send` 会将 `error` 转为 Go error。判断调用成功时，需要同时检查传输状态与协议结果。

更换本机端口时，同时修改监听地址、Card 公布地址和调用地址：

```bash
# 终端 A
make logistics-mock-run ARGS='-addr 127.0.0.1:18082 -public-url http://127.0.0.1:18082'
# 终端 B
make a2a-demo ARGS='-card-url http://127.0.0.1:18082/.well-known/agent-card.json'
```

### 5.5 对照代码理解五层版本与网络边界

| 信息 | 谁决定 | 当前保证 |
| --- | --- | --- |
| 本地 AgentVersion | Conversation 创建时解析 | 后续运行读取原快照 |
| 本地 ToolVersion | AgentSnapshot 固定引用 | Endpoint、Schema、敏感标记按版本解析 |
| `metadata.agent_version_id` | 本地 Schema 限定、Mock 检查 | 本课 Mock 拒绝其他业务版本 |
| AgentCard 的 `version` | 远端声明 | 通用客户端未读取和校验 |
| `protocolVersion` / `A2A-Version` | Card 声明、客户端传递 | Mock 要求 `1.0`；客户端尚未实现完整版本协商 |

Mock 在响应中声明实际执行版本；通用客户端当前只提取文本，未校验该回执。接入任意外部服务时，需要约定版本选择与可验证的执行标识，才能扩大追溯保证。

演示 Executor 仅允许 `127.0.0.1` 和 `localhost`。A2A Client 每次调用读取 Card，要求选中接口与 Card 同源，并限制重定向。Card 地址固定后，同源接口路径和远端实现仍可能变化。

A2A 分支尚未将 `ToolVersion.AuthConfig` 注入请求，也未贯通本地 `IdempotencyKey`。本课 Mock 提供只读演示查询。真实认证、远端写操作幂等和任意 kbot Agent 的 A2A 发布端点属于后续扩展。

代码入口：[演示命令](../cmd/lesson21-demo/main.go)、[Runtime 装配](../internal/demo/lesson21/runtime.go)、[物流 Mock](../internal/demo/logistics/service.go)、[A2A Client](../internal/runtime/tooling/a2a.go)。

## 6. 验收、排查与练习

执行本课检查：

```bash
make lesson21-test
```

| 测试 | 验证内容 |
| --- | --- |
| `TestRuntimeCallsExternalLogisticsThroughA2A` | 正式 Engine 调用 HTTP Mock、工具事件、订单结果和消息历史 |
| `TestLogisticsRejectsUnsupportedRemoteVersion` | 缺失或错误的远端版本被拒绝 |
| `TestA2AClientDiscoversCardAndSendsV1Message` | Card 发现、协议字段和请求头 |
| `TestA2AClientRejectsCardInterfaceOnAnotherHost` | Card 接口主机限制 |
| `TestExecutorDispatchesA2AToolThroughSharedContract` | A2A 经过统一工具执行契约 |

测试入口：[演示 Runtime 测试](../internal/demo/lesson21/runtime_test.go)、[A2A Client 测试](../internal/runtime/tooling/a2a_test.go)、[Executor 测试](../internal/runtime/tooling/a2a_executor_test.go)。

| 现象 | 排查方向 |
| --- | --- |
| `connection refused` | Mock 是否启动，Card 地址与端口是否一致 |
| Card 同源或主机错误 | Mock 的 `-public-url`、客户端地址和白名单 |
| 远端版本错误 | Schema 与 Mock 都要求 `logistics-v1` |
| HTTP 200 但调用失败 | 继续检查 JSON-RPC `error` |
| 普通问候只提示订单号 | 默认使用 DemoModel；普通对话需真实模型 |
| 主服务重启后工具缺失 | 内存 Registry 已清空，需重新装配版本或持久化恢复 |

**练习一：版本固定。** AgentSnapshot 固定 Tool v1 后，新发布 Tool v2，已有会话会使用哪个版本？

参考要点：继续使用原快照固定的 v1。切换工具需要发布新的 AgentVersion，并为新的会话解析该版本。

**练习二：远端执行回执。** 请求中包含 `agent_version_id=logistics-v1`，可以据此证明任意远端都执行了 v1 吗？

参考要点：本课 Mock 会校验该参数，通用客户端仍需接入可验证的版本回执。请求参数与实际执行行为分别需要证据。

**练习三：网络边界。** Card 指向另一主机上的 `/a2a`，当前客户端如何处理？

参考要点：客户端执行主机和同源检查，拒绝超出策略的接口。变更远端部署应同时评审本地工具版本和网络策略。

## 7. 课后衔接与参考资料

第 22 课使用 `internal/demo/lesson21.NewRuntime` 复用本课物流助手，将飞书消息转换成 Runtime 输入，并把回答发送回原消息。A2A 的调用和业务版本约束继续沿用本课实现。

- [第 22 课：飞书创建应用与接入 kbot](lesson-22-feishu-kbot.md)
- [A2A 规范](https://a2a-protocol.org/latest/specification/)
- [A2A 核心概念](https://a2a-protocol.org/latest/topics/key-concepts/)
- [A2A 1.0 变更](https://a2a-protocol.org/latest/whats-new-v1/)
- [课程地图](course-map.md)
