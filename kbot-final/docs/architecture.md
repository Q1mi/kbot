# kbot 架构综述

> 本文帮助学员在 30 分钟内建立全局认知。实现状态与演进边界以 [status.md](status.md) 为准。

## 一句话

kbot 是面向 Go 开发者的企业级 AI Agent 教学平台：工程师开发原子 Tools 与 KB，Agent 设计者管理 Prompts 与 Skills，业务用户通过对话、IM 或 Webhook 使用 Agent。

## 分层

```
接入层 (api)         REST / SSE / A2UI / WS / Webhook
   │
控制面 (platform)    iam / prompt / tool / skill / kb / agent / eval / audit
   │   配置快照
数据面 (runtime)     engine(Eino ADK+Skill) / llm / tooling / sandbox client /
   │                 retriever / promptcache / guard / cache / team
执行隔离             sandbox-runner → Docker daemon → 一次性 Python/Bash 容器
基础设施 (infra)     postgres / redis / otel / jobs / metrics
```

**控制面 vs 数据面分离**：控制面管"配置/版本/权限"，数据面管"对话/检索/工具执行"。二者通过 **immutable 配置快照** 通信——Runtime 启动 Agent 时拉一份含所有依赖具体版本号的快照，不实时回查。

## 关键机制（按能力域）

- **M1 地基**：IAM（JWT + 中间件链 recover→requestid→log→trace→CORS→auth→workspace）、最简 Runtime、SSE/WS、docker-compose。
- **M2 能力**：Tool Registry（rest_api/mcp_server/internal_sdk/code_execution/a2a 五源，平台 `Executor` 适配 Eino `InvokableTool`）、Docker Sandbox、KB（Connector + ingest 状态机 + Eino Retriever Router + PostgreSQL 混合检索）。
- **M3 配方**：Prompt/模型原子不可变版本、环境基线、Candidate 灰度、Provider Account/Deployment/Profile 控制面、Eino ChatTemplate、本地缓存与 Pub/Sub；Skills 使用 Eino Skill Middleware 实现 L1/L2 渐进式披露；Agent/Conversation 固化配置快照。
- **M4 工程化**：Guard（注入/PII/限流/配额/分级路由，按 4 个 Hook 注入）、Eval 门禁（三层 Judge + CI 阻断）、Audit（对话与 Tool/Skill/Sandbox 结构化飞行记录仪）、Cache（Embedding/Redis 幂等与分布式锁）、OTel GenAI 语义约定 + Prometheus + Langfuse。
- **M5 向外**：多 Agent（Supervisor 使用 ChatModelAgent + AgentTool，Pipeline 使用确定性顺序编排）、A2A v1.0.1 互联（AgentCard + JSON-RPC `SendMessage`）、A2UI v0.9.1 受控生成式 UI、飞书/Webhook 入站适配、Go SDK + kbotctl。

## 数据流：一次对话

```
用户消息
  → 建立 Langfuse Trace context（user / session / release / metadata）
  → Guard.OnInput（注入检测 + PII 脱敏）
  → 取/建 Conversation（pin AgentVersion，并解析 baseline/candidate）
  → 固化 PromptVersion / ModelProfileVersion / generation config / experiment variant
  → 按 pinned 版本解析快照（Prompt 渲染 / Skill specs / Tool IDs）
  → Eino ChatModelAgent + Runner：
        ChatModel 识别 tool_calls → ToolsNode 执行 Tool/KB/网络权限中间件并回喂
        Skill Middleware 暴露 L1 元数据，调用 skill 工具后加载 L2 并收窄 Tool/KB 范围
        敏感 Tool → StatefulInterrupt + checkpoint + pending approval → SSE 发 A2UI surface
        A2UI action 批准 → approval ID 定位 interrupt address → Worker 用 ResumeWithParams 定点恢复
  → Guard.OnOutput（出站 PII 脱敏）→ 流式 emit
  → 持久化消息 + Audit 轨迹
```

## 独立 Sandbox Runner

`code_execution` Tool 通过内部 HTTP 调用 `cmd/sandbox-runner`。App 与 Worker 不持有 Docker Socket；Runner 使用固定的服务端策略创建一次性容器，请求只能提交 `language` 和 `code`。

```text
Eino ToolsNode → Tool Executor → Sandbox HTTP Client
                              ↓ Bearer Token
                       sandbox-runner
                              ↓ Docker CLI
        Python/Bash 一次性容器（禁网、只读、非 root、资源受限）
```

默认边界：256MB 内存、0.5 CPU、64 PID、64MB `/tmp`、30 秒超时、64KB 代码、1MB stdout/stderr、单 Runner 4 并发。Runner 额外启用 `cap-drop=ALL`、`no-new-privileges` 和文件描述符上限。所有参数只能由部署环境配置。

Docker Socket 具备宿主机高权限，Runner 应部署在专用节点或开发环境中，并限制网络入口。生产演进可将 Runner 内部执行后端替换为 Kubernetes Agent Sandbox + gVisor；App 的 Tool Calling 与内部执行协议可以继续复用。

## 技术选型要点

Go 1.26.6 · chi 路由 · pgx + sqlc · PostgreSQL + pgvector · go-redis · asynq · JWT · AES-GCM ·
Eino v0.9.15 ChatModel / ADK · OTel + Langfuse + Prometheus · A2UI v0.9.1。详见 `docs/adr/`。

## Langfuse 与 A2UI 的边界

Langfuse 接收 OTel traces，承担模型调用、Token、Guard、Tool 和审批链路的观测与排障。kbot 的业务库
继续保存 Conversation、Message、Approval、Checkpoint 和 Audit 等事实记录。

A2UI 负责在对话流中传递声明式 surface。服务端与浏览器共同限制 Catalog、组件和 action；真正的审批
状态变更仍由带身份、Workspace 和业务归属校验的 API 完成。当前受控组件集聚焦敏感工具审批，详见
ADR 0021、ADR 0022 和 `docs/labs/langfuse-a2ui-demo.md`。

**检索演进目标**：当前 Compose 装配使用 PostgreSQL `simple + ts_rank_cd`、pgvector 和应用层 RRF，
作为轻量可运行基线。课程终态按 ADR 0019 增加 OpenSearch：PostgreSQL 作为事实数据源，
通过 Transactional Outbox + Asynq 同步可重建搜索索引，使用 IK 中文分词、BM25、k-NN 和 RRF；
pgvector 保留为轻量部署与故障降级实现。

## 当前实现边界

默认运行路径已经使用 PostgreSQL、pgvector 与 Redis，实现 server/worker 跨进程共享、审计留证和持久化评估集。
各 `Memory*Store` 仍作为测试 fixture 和 `db == nil` 时的轻量装配存在，同一组 contract test 会验证内存与
PostgreSQL 两种实现。

当前实现边界与验证结果以 [`docs/status.md`](status.md) 和代码为准。

主运行路径是：

```text
cmd/server
  → internal/api
  → internal/platform
  → internal/runtime
  → internal/infrastructure
```

早期重复的 `internal/agent`、`internal/llm`、`internal/server`、`internal/rag` 教学占位实现已删除。
