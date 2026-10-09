# 第 19 课：OpenTelemetry Trace 与 Langfuse

> 课件来源：[19-end](https://github.com/Q1mi/kbot/blob/19-end/kbot-course/docs/langfuse.md)。 本课实验对应 `19-end`；执行文中的命令前，请保存当前工作并切换到该标签，再进入 `kbot-course/`。

本课把 Agent Run、模型调用、工具执行组织成可观察的调用链，并把关键操作写入可校验的审计账本。
Langfuse 接收 OTel Span 后将其显示为 Observation；一次 Agent 请求对应一个 Trace，
同一个 Conversation 的多轮请求通过 Session ID 关联。

## 获取课程代码

仓库地址为 `https://github.com/Q1mi/kbot`，课程 Go Module 位于 `kbot-course/`。
首次拉取：

```bash
git clone --branch 19-end https://github.com/Q1mi/kbot.git
cd kbot/kbot-course
```

已经拉取过旧标签的同学，先保存本地修改，再在仓库根目录执行：

```bash
git fetch origin +refs/tags/19-end:refs/tags/19-end
git switch --detach 19-end
cd kbot-course
```

以下命令均在 `kbot-course/` 目录运行。Go 版本以 `go.mod` 为准。

## 配置 Langfuse

1. 在 Langfuse Cloud 或自部署实例创建 Project，获取该项目的 Public Key 和 Secret Key。
2. 参考 `.env.langfuse.example` 配置环境变量。如果还没有 `.env`，可以复制示例；已有文件时合并配置，保留前面课程的配置。
3. 把 `LANGFUSE_BASE_URL` 改为项目所在区域或自部署地址，再加载配置：

```bash
source .env
```

`.env` 是本地文件，已经加入 `.gitignore`。程序通过进程环境读取配置，需要显式执行 `source`。

配置示例会把 Public Key 和 Secret Key 编码为 HTTP Basic Auth，导出到：

```text
${LANGFUSE_BASE_URL}/api/public/otel/v1/traces
```

课程采用 `otlptracehttp`，使用 HTTP/protobuf。
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 指定完整 Trace 地址；如果使用通用的
`OTEL_EXPORTER_OTLP_ENDPOINT`，则配置到 `/api/public/otel`，Exporter 会追加 `/v1/traces`。
Trace 专用配置优先于通用配置，修改实例时注意同步环境变量。

Langfuse v4 配置包含 `x-langfuse-ingestion-version=4`，用于实时写入；自部署旧版本需要查阅对应版本说明。
仅设置 `LANGFUSE_PUBLIC_KEY` 和 `LANGFUSE_SECRET_KEY` 无法启用课程里的 OTel Exporter，
示例脚本会将它们转换为实际使用的 `OTEL_*` 配置。

官方参考：[OTel 端点、认证与属性映射](https://langfuse.com/integrations/native/opentelemetry)。

## 先运行确定性演示

```bash
make langfuse-demo
```

此命令使用服务端相同的 Engine 和埋点封装，配合内存控制面、模拟模型、模拟订单工具和内存审计账本。
它只需要 Langfuse 连接配置，运行时无需 PostgreSQL、Docker 或模型 API Key。

默认生成三条 Trace：

| 场景 | 预期步骤 | 检查重点 |
| --- | --- | --- |
| 流式回答 | `agent.run → llm.stream` | 输入、拼接后的回答、首个内容分片时间、完整消费耗时、用量 |
| 工具调用 | `agent.run → llm.generate → tool.execute → llm.generate` | 两次模型调用和工具步骤属于同一个 Trace，工具参数及返回值可见 |
| 模拟读取失败 | `agent.run → llm.stream` | 已接收的部分回答和 ERROR 状态；命令识别该预期失败后继续完成校验 |

表中箭头表示执行顺序；两个模型步骤和工具步骤都是 `agent.run` 的直接子节点。
流式成功和失败使用同一个 Session ID，工具场景使用另一段固定 Agent Version 的会话。
终端会输出每次运行的 `trace_id` 和会话 ID；在 Langfuse 的 Observations/Tracing 页面按这些值查找。
命令退出前会刷新导出队列，并验证内存审计链。

模拟模型名称为 `course-demo-model`，Token 为预设教学数据。要演示费用计算，可在 Langfuse 的
模型配置中添加与 `^course-demo-model$` 匹配的自定义模型，为 `input`、`output` 配置价格后重新运行。
例如教学价格为每个输入 Token 0.000001 USD、每个输出 Token 0.000002 USD，
流式成功场景的 50 输入 / 12 输出 Token 对应 0.000074 USD。这些价格仅用于演示。

## 连接真实模型

保留前面课程使用的模型配置，例如：

```bash
export ARK_API_KEY='你的方舟 API Key'
export KBOT_LLM_MODEL='doubao-seed-2-0-lite-260215'
go run ./cmd/langfuse-demo -real
```

`-real` 使用真实 Gateway 和供应商返回的 Token，用真实模型完成流式与工具两个场景；订单工具仍返回课堂模拟数据。
它会产生真实模型调用费用。是否调用工具由模型响应决定，示例提示词明确要求查询订单。

如果使用完整 HTTP 服务，配置好前面课程的数据库、JWT、模型和 Sandbox Runner 后，在同一终端执行
`make run`，再通过 Admin Console 创建会话、发送消息。两种入口共用本课埋点代码。

费用依赖 Langfuse 是否匹配模型价格。自定义部署 ID、豆包模型别名或私有价格需要配置相应模型规则；
缺少供应商用量时，本课保留未知状态。代码只上报供应商用量，费用交给 Langfuse 模型价格计算。
缓存输入与推理输出从输入/输出总数中扣除后分别记录，确保各用量分类互斥。
官方参考：[Token 与费用、互斥用量分类](https://langfuse.com/docs/observability/features/token-and-cost-tracking)。

## 埋点字段与代码位置

| 数据 | 实现 |
| --- | --- |
| 批量导出、服务名、刷新关闭 | `internal/infrastructure/otel/tracing.go` 的 `Setup` |
| 用户、会话、Workspace、Agent Version、环境 | `StartRun` 保存公共属性，`StartOperation` 复制到每个业务子 Span |
| 根节点类型与输入输出 | `internal/runtime/engine/chat.go`：`agent`、用户问题、最终回答、运行状态 |
| 模型名称和显式调用参数 | `observed_model.go`、`llm/gateway.go`：保留默认模型、调用覆盖及故障切换后的模型身份 |
| 模型输入输出与实际用量 | `observed_model.go`：`generation`、文本消息、工具调用、finish reason、usage details |
| 流式首个内容分片和结束时间 | `observed_model.go`：读取 EOF/错误/取消时结束 Span；提前停止消费的 Span 在所属 Run 退出时同步结束并标记错误 |
| 工具及沙箱参数、返回值、状态码 | `internal/runtime/engine/react.go`：`tool` 类型 |
| 审批恢复 | `resume.go`：新建 Trace，沿用 Conversation ID，记录 `kbot.run.resumed=true`；用户维度为审批执行的决策人 |
| 内容采集开关、长度限制和结构化字段脱敏 | `internal/infrastructure/otel/content.go` |

模型参数只记录调用时明确给出的通用选项；供应商隐式默认值不作推测。
流式适配器保留 Eino 原始分片与错误，追踪额外保留的内容限制为 16 KiB；超过限制后仍持续记录用量。
首个内容时间从第一个文本、工具调用或生成内容分片计时，跳过纯角色与纯用量分片。

公共属性通过进程内 Go Context 传播到课程创建的 Span；跨服务 Trace/Baggage 传播需要在具体传输边界另行配置。
`kbot.workspace.id` 等原始属性保留，同时提供 `langfuse.trace.metadata.workspace_id` 等顶层元数据用于筛选。

## 内容采集范围

`KBOT_TRACE_CONTENT` 默认关闭。课堂示例显式设为 `true`，方便观察模拟输入输出。
关闭后，调用关系、模型名称、耗时、用量和错误状态继续保留，输入输出正文停止采集。
`KBOT_TRACE_ENVIRONMENT` 默认 `development`，部署时可配置为 `staging` 或 `production`。

采集代码会遮盖常见结构化凭据字段（如 `password`、`api_key`、`authorization`、`token`），
并处理 JSON 字符串形式的工具参数。模型消息只取文本和工具调用，不导出供应商 Extra、二进制媒体或推理正文。
输出截断后仍是有效 JSON，并带有截断标记。
这层处理不能识别自由文本里的全部个人信息；接入真实业务数据前，需要按业务要求脱敏后再开启正文采集。

## 自动化验证与排查

```bash
go test ./internal/infrastructure/otel ./internal/runtime/engine ./internal/runtime/llm ./internal/platform/audit ./cmd/langfuse-demo
go test -race ./internal/infrastructure/otel ./internal/runtime/engine ./internal/runtime/llm ./cmd/langfuse-demo
go test ./...
```

测试使用本机 HTTP 模拟服务和内存 Span Recorder，无需真实账号。OTLP 测试会解析 Exporter 发出的 protobuf，
校验地址、认证头和导出数量，并覆盖资源 Schema 初始化。运行时测试验证父子关系、公共属性、用量、
工具正文、流式 EOF/错误/取消/提前停止，以及正文开关、脱敏和长度限制。

| 现象 | 检查位置 |
| --- | --- |
| 无数据 | 服务进程是否拿到 OTEL 环境变量、区域与密钥是否匹配、导出错误日志、网络连接 |
| 404 | 通用地址与 Trace 专用地址的 `/v1/traces` 是否配置正确 |
| 401/403 | Public/Secret Key 是否属于当前地址下的项目，认证头是否已更新 |
| 有步骤但没有正文 | `KBOT_TRACE_CONTENT=true` 是否已经导入当前进程 |
| 有 Token 但没有费用 | 模型名称是否匹配 Langfuse 模型价格 |
| 流式数据尚未出现 | 等待流消费结束；短命令检查退出刷新错误 |

Langfuse 负责运行观测。本课的审计账本通过事件哈希与前序哈希验证记录完整性；
Trace 导出与审计写入分别执行。Prompt 管理、自动评估和实验功能不在本课接入范围内。
