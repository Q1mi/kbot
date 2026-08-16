# kbot-final：Go 企业级 AI Agent 稳定版

kbot-final 是课程完成后的稳定版 Agent 平台，覆盖 Agent 控制面、ReAct 运行时、知识库、工具与 Skills、人在环审批、评测、安全、审计和可观测性，并提供可复现的 Docker Compose 环境。仓库内的跨境电商与保险项目采用独立 Go Module，通过标准 Tool 协议接入平台。

项目使用 Go 1.26.5、Eino、chi、PostgreSQL、pgvector、Redis、Asynq、MinIO、OpenTelemetry、Langfuse、React 与 Ant Design。当前代码定位为可运行、可测试、可演示的企业工程基线；生产部署仍需结合组织要求补充 SSO、密钥托管、网络策略、HA、备份与灾备。

## 课堂最终效果

启动完整课堂环境后，学员可以在浏览器中完成以下链路：

1. 登录 Admin Console，在 14 个主页面管理 Workspace、Agent、Prompt、模型、Tool、Skill、KB、Team、Eval、Guard、Audit 与 Observability。
2. 在“会话”页进行多轮 SSE 对话，查看运行状态、Conversation ID 和 Langfuse Trace 深链，并从历史列表恢复已有上下文。
3. 发起退款请求，模型调用敏感 Tool，服务端保存与 approval ID 一一绑定的 checkpoint，并在有序会话时间线中返回 A2UI v0.9.1 审批卡片。
4. 点击批准或拒绝；批准后由 Asynq Worker 按 approval ID 恢复，只执行对应的敏感操作，卡片原位展示执行中与完成状态。
5. 在 Langfuse 中查看 Agent、Generation、Guard、Tool、审批暂停与恢复等 Trace 节点，以及 Token、模型、用户和会话维度。
6. 输入 Prompt Injection 样例，在页面和 Trace 中观察 Guard 拦截与审计留痕。

## 独立业务项目

| 项目 | 业务范围 | 独立运行 |
|---|---|---|
| [跨境电商运营与供应链协同 Agent](projects/crossborder/README.md) | 订单履约、库存调拨、物流方案、退款、结算对账 | `make crossborder-up crossborder-install crossborder-e2e` |
| [保险承保、理赔与反欺诈 Agent](projects/insurance/README.md) | 人工核保、责任审核、赔款计算、支付冻结、欺诈调查 | `make insurance-up insurance-install insurance-e2e` |

两个项目拥有各自的领域模型、Tool 契约、Skill、知识库、Eval、Dockerfile 和 Compose 文件，业务代码之间没有依赖，并分别使用独立 Go Module 与 Compose 环境。

两套课堂环境可以同时启动：跨境电商控制台使用 `http://localhost:8181`，保险控制台使用 `http://localhost:8182`；PostgreSQL、Redis、Worker 与数据卷也分别隔离。

## 工作空间：业务资源隔离边界

Workspace 是 kbot 中承载一个业务项目、部门或租户的逻辑资源容器。同一个 kbot 环境可以创建多个工作空间，每个空间分别管理自己的模型配置、Prompt、Tool、知识库、Skill、Agent、Team、会话和评测集。

课堂环境首次启动会幂等初始化两个业务空间：

```text
Workspace：跨境电商运营平台
├── 电商专用 Provider Accounts
├── 电商模型 Deployments
├── 商品运营 Profiles
├── 供应链协同 Profiles
├── 商品运营 Prompt 预设
├── 供应链协同 Prompt 预设
├── 2 个场景知识库 / 8 个电商 Tools / 3 个 Skills
└── 商品运营 Agent / 供应链协同 Agent

Workspace：保险理赔与反欺诈平台
├── 保险专用 Provider Accounts
├── 保险模型 Deployments
├── 理赔审核 Profiles
├── 反欺诈分析 Profiles
├── 理赔审核 Prompt 预设
├── 反欺诈分析 Prompt 预设
├── 2 个场景知识库 / 9 个保险 Tools / 3 个 Skills
└── 理赔审核 Agent / 反欺诈分析 Agent
```

完整课堂环境会使用 Mock LLM 为两个 Workspace 分别创建独立的 Provider Account、两项 Deployment 和两项业务 Profile。轻量环境配置了 `KBOT_LLM_API_KEY` 时也会执行同样的幂等初始化；未配置模型凭据时保留空的 Workspace 容器，由学员在“模型配置”中录入真实 Provider Account。两个 Workspace 始终使用独立的凭据记录和路由策略。

模型 Profile v1 就绪后，启动流程还会幂等创建 8 个课程 Prompt 资产：4 个 System Prompt 固定绑定对应的 Profile v1，4 个 User Prompt Template 带 JSON Schema 变量约束，用于渲染标准化业务任务。两类 Prompt 均在 Agent Builder 中通过独立字段绑定；会话 Playground 根据 User Prompt Template 自动生成首轮任务表单，并由服务端渲染和固化实际版本。详细设计与变量见 [课程业务 Prompt 预设](docs/course-prompt-presets.md)。

完整课堂环境还会启动两个相互独立的业务模拟器，初始化 4 个场景知识库与 8 份业务文档，通过真实 REST 和内部 SDK 试调发布 17 个 Tool，发布 6 个 Skill，并装配 4 个最小权限 Agent。所有调拨、退款、结算申诉、补件、赔付、支付冻结和调查立案 Tool 保留人工审批元数据。详见 [课程知识库、Tool、Skill 与 Agent 预设](docs/course-business-assets.md)。

登录 Admin 后，通过右上角选择当前工作空间。前端会在后续 API 请求中携带 `X-Workspace-ID`，后端据此查询和创建对应空间的资源；浏览器会保存最近一次选择。推荐的业务配置顺序是：

```text
工作空间
  → 模型配置
  → Prompt
  → Tool / 知识库
  → Skill
  → Agent / Team
  → 会话验证
  → Eval / Guard / Audit / Observability
```

Workspace 通过业务表中的 `workspace_id` 提供逻辑隔离，共享当前 kbot 环境的 PostgreSQL、Redis、MinIO 和 Langfuse。需要独立故障域或独立数据基础设施时，可使用本仓库为跨境电商和保险项目提供的独立 Compose 环境。

Workspace 已接入成员管理与请求期 RBAC。平台管理员可治理全部空间；Workspace 提供 owner、admin、editor、member、viewer 五档角色，后端会校验 JWT 中的用户状态、Workspace 归属、资源归属和方法级权限。跨 Workspace 的 Agent、Tool、Skill、KB、Conversation、Approval 与 Audit 访问会被拒绝。生产部署仍需按组织要求接入 SSO/OIDC、集中式策略治理和外部 Secret Manager。

## 可以写进简历的技术亮点

| 方向 | 项目实现 |
|---|---|
| Agent 运行时 | 基于 Eino ChatModel 实现可测试的 ReAct 循环、Function Calling、最大步数控制、SSE/WS 流式输出与上下文取消传播 |
| 控制面设计 | Prompt、Tool、Skill、Agent、Team 和 Model Profile 采用不可变版本与环境指针；Tool 发布门禁按版本绑定试调记录，Team 版本可提升到 dev/staging/prod，Conversation 固化实际运行配置 |
| 多模型治理 | Workspace 级 Provider Account、AES-GCM 密钥加密、Deployment/Profile 主备路由、数据分级、项目环境绑定、RPM/TPM/月预算强制执行与模型调用归因 |
| 工具生态 | 用统一 Executor/Factory 接入 REST、内部 SDK、独立代码 Sandbox、MCP Streamable HTTP 与 A2A v1.0.1；stdio 协议代码只在隔离 Runner/测试边界复用 |
| 企业 RAG | Markdown Connector、内容哈希增量同步、Asynq 异步 ingest、PostgreSQL FTS + pgvector + RRF 混合检索与三档检索 Playground |
| 人在环审批 | 敏感 Tool 触发 checkpoint、Approval 与 A2UI 受控组件；checkpoint 按 approval ID 唯一绑定，审批 action 经身份和 Workspace 校验后由 Worker 安全续跑 |
| 可观测性 | 使用 OpenTelemetry 记录 Agent/LLM/Guard/Tool/Approval spans，通过 OTLP/HTTP 接入自托管 Langfuse，并关联 user/session/release/metadata |
| 质量与安全 | Prompt Injection、PII、限流、持久化月度配额、工作空间动态 Guard 规则；确定性/LLM light/LLM full Judge；Eval 历史与离线 Gate |
| 数据与任务 | 26 组 migration、sqlc、PostgreSQL/pgvector、Redis、Asynq、MinIO；审计与模型调用日志按月分区并安全归档 |
| 工程质量 | contract test 复用内存与 PostgreSQL 实现、dockertest 集成测试、race detector、Vitest、前端路由拆包、Swagger 2.0、CI |

简历描述应以自己完成并能讲清的模块为准。当前交付范围不包含 OpenAPI 3 客户端生成、SSO 和 Kubernetes。

## 架构

```text
接入层       REST / SSE / WebSocket / Webhook / Lark / A2UI / Admin Console
   ↓
控制面       IAM / Prompt / Model / Tool / Skill / KB(Knowledge Base) / Agent / Team / Eval / Audit
   ↓ immutable snapshot
数据面       Engine / LLM Gateway / Tooling / Sandbox Client / Retriever / Guard / Cache / Team
   ↓
执行隔离     sandbox-runner / Docker 一次性执行容器
   ↓
基础设施     PostgreSQL / pgvector / Redis / Asynq / MinIO / OTel / Prometheus / Langfuse
```

核心运行路径：

```text
cmd/server → internal/api → internal/platform + internal/runtime → internal/infrastructure
cmd/worker → KB ingest / approval resume / partition maintenance
cmd/sandbox-runner → 内部 HTTP API / Docker 容器创建 / 资源与安全边界
```

架构细节见 [docs/architecture.md](docs/architecture.md)，运行方式见 [docs/runbook.md](docs/runbook.md)。

## 快速启动

### 完整课堂环境（默认）

默认入口启动 kbot、Worker、独立 Sandbox Runner、PostgreSQL、Redis、MinIO、跨境电商与保险业务模拟器、确定性 Mock LLM，以及完整的 Langfuse 可观测环境。

```bash
make bootstrap
make up
make demo
```

- kbot Admin：<http://localhost:8080>，`teacher@kbot.local / admin12345`
- Langfuse：<http://localhost:3000>，`admin@kbot.local / admin12345`
- `make up` 会等待 Mock LLM、Langfuse 和 kbot readiness 全部通过。
- `make demo` 会生成普通对话、A2UI 敏感操作审批和 Guard 拦截三组 Trace。
- Sandbox Runner 仅在 Compose 内部网络监听，App 和 Worker 使用内部 Token 调用；Docker Socket 只挂载到 Runner。

常用命令：

```bash
make ps                 # 查看容器
make logs               # 跟随日志
make down               # 停止并保留数据卷
make test               # go test -race -count=1 ./...
make test-integration   # 真 PostgreSQL/Redis 集成测试，需要 Docker
```

### 轻量开发环境

低配置电脑或仅开发 API、Runtime、控制面时，可以启动不含 Langfuse、ClickHouse 和 Mock LLM 的基础环境：

```bash
cp .env.example .env
# 修改 .env 中的 JWT 与 Credential 加密密钥，均至少 32 个字符
make up-lite
make seed-lite
```

- Admin Console：<http://localhost:8080>
- 默认账号：`admin@example.com / admin12345`
- 真实对话需要在 Admin 创建 Provider Account，或设置全局 `KBOT_LLM_API_KEY` 回退。
- 轻量环境同样包含 Sandbox Runner，可完成 `code_execution` Tool 的真实隔离执行。
- 使用 `make ps-lite`、`make logs-lite` 和 `make down-lite` 管理该环境。
- 完整环境和轻量环境使用相同宿主机端口，切换前先停止当前环境。

### 配置模型、Prompt、Skill 与 Agent

Admin 中的推荐配置顺序：

1. “模型配置”创建 Provider Account、Deployment 和带主备顺序的 Model Profile；
2. “Prompts”创建新版本，绑定 Model Profile Version 与生成参数，再晋升到目标环境；
3. “Tools”和“知识库”准备 Agent 的原子能力；
4. “Skills”编辑 SKILL.md、查看版本并发布；
5. “Agents”装配 Prompt、Tools、已发布 Skill Version、KB 与网络权限；
6. 在 Agent 详情页创建新版本，并晋升到 `dev/staging/prod`。

“会话” Playground 可以选择 Agent 环境；创建会话后环境选择会锁定，Conversation 继续使用创建时 pin 的 Agent Version。

Skill frontmatter 使用以下字段名：

```yaml
---
name: refund-order
description: 处理退款申请
allowed-tools: [refund_order]
allowed-kbs: [<knowledge-base-id>]
disable-model-invocation: false
requires_network: false
---
```

发布门禁会校验 Tool 名称与 KB ID。Runtime 会执行 Agent 与 Skill 双层 allowlist；REST、MCP、A2A 工具还受 Agent 版本的网络权限控制。设置 `disable-model-invocation: true` 后，Skill 不进入模型可见的 L1 清单，用户可通过 `/skill <name>` 或 `/<name>` 显式触发。

### 可选本地模型

```bash
KBOT_OLLAMA_BASE_URL=http://ollama:11434/v1 \
  docker compose -f deploy/docker-compose.yml --profile local-llm up -d --build
bash scripts/ollama-pull.sh
```

模型数据保存在 `kbot_ollama` volume 中。首次下载耗时和体积取决于所选模型。
`secret` 分级会话只允许使用本地模型；未配置 Ollama 时会显式返回错误。

## Langfuse + A2UI 课堂环境

课堂 overlay 会启动 kbot、确定性 Mock LLM、Langfuse v3 及其 PostgreSQL、ClickHouse、Redis 和 MinIO 依赖，无需公网模型 API Key。

```bash
make up
make demo
```

- kbot Admin：<http://localhost:8080>，`teacher@kbot.local / admin12345`
- Langfuse：<http://localhost:3000>，`admin@kbot.local / admin12345`
- `make demo` 会生成普通问答、A2UI 敏感操作审批和 Guard 拦截三组数据。
- 原有 `make langfuse-up/langfuse-demo/langfuse-ps/langfuse-logs/langfuse-down` 保留为兼容入口。

详细授课脚本、页面操作和排障步骤见 [Langfuse + A2UI 实验手册](docs/labs/langfuse-a2ui-demo.md)。课堂环境开启全采样和模型内容采集，并使用固定演示密钥，仅供本机教学。生产配置应关闭敏感内容采集、调整采样率并替换全部密钥。

```bash
make down             # 停止并保留课堂数据
make langfuse-reset   # 删除 kbot-course 容器与数据卷
```

## 开发与验证

```bash
go build ./...
go test -race -count=1 ./...
go vet ./...
(cd web/admin && npm run build && npm audit --audit-level=moderate)
docker compose -f deploy/docker-compose.yml config --quiet
make openapi
```

Go SDK 位于 `pkg/sdk/go`，运维 CLI 位于 `cmd/kbotctl`：

```bash
go run ./cmd/kbotctl -- \
  -email admin@example.com -password admin12345 \
  agent-chat -agent <agent-id> -input "你好"
```

## 项目结构

```text
cmd/                    server、worker、migrate、kbotctl、mockllm 入口
internal/api/           HTTP、SSE、WebSocket、中间件与 A2UI action
internal/platform/      IAM、Prompt、Tool、Skill、KB、Agent、Eval、Audit 等控制面
internal/runtime/       Engine、LLM、Tooling、Retriever、Guard、Cache、Team
internal/infrastructure PostgreSQL、Redis、Asynq、MinIO、OTel、Metrics
migrations/             26 组 golang-migrate 数据库迁移
web/admin/              React + Ant Design Admin Console
deploy/                 基础与 Langfuse Docker Compose、镜像构建文件
scripts/                初始化、演示和课堂环境脚本
projects/crossborder/   独立跨境电商 Agent 业务项目
projects/insurance/     独立保险 Agent 业务项目
docs/                   架构、Runbook、协议集成与课堂实验
```

## 实现边界

当前已实现并验证：PostgreSQL/pgvector/Redis 默认 Compose 装配、异步 KB、Prompt/模型/Tool/Team 版本治理、会话历史恢复、运行时 Tool/KB/网络权限、ReAct 与多源工具、动态 Guard/配额、三层 Eval Judge 与历史、MCP、A2A、飞书/Webhook 自动回复、Langfuse 和 A2UI 审批闭环。

生产部署需要结合组织环境继续补充：

- OpenAPI 3 与 Go/TypeScript 客户端自动生成，当前 Swagger 2.0 与两端类型仍独立维护。
- SSO/OIDC、API Key 鉴权、集中式策略治理、外部 Secret Manager、Kubernetes、HA、备份与灾备。
- 大规模语料下的索引调优、容量验证、SSE/WS 长连接测试和真实模型成本基线。

## 文档导航

- [架构综述](docs/architecture.md)
- [运行与配置](docs/runbook.md)
- [课程业务资源](docs/course-business-assets.md)
- [课程 Prompt 预设](docs/course-prompt-presets.md)
- [Langfuse + A2UI 实验](docs/labs/langfuse-a2ui-demo.md)
- [MCP 集成](docs/integrations/mcp.md) / [A2A 集成](docs/integrations/a2a.md) / [Webhook 集成](docs/integrations/webhook.md) / [飞书集成](docs/integrations/lark.md)
提交前请跑完与改动范围相匹配的测试。
