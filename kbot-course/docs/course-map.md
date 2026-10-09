# kbot 课程地图

本仓库对应《Go 企业级 AI Agent 平台实战》的 23 节边敲边讲课程。课程从最小 Go HTTP 服务开始，逐步完成 Agent Runtime、Tool、RAG、Skill、人在环审批、安全治理、协议集成和容器化交付。

- 每课从 `XX-start` 开始，完成后对照 `XX-end`。
- `start` 包含本课需要的最小接口壳，静态数据和辅助前端按需预置；本课关键测试通常在现场编码阶段补齐。
- 现场编码聚焦本课的核心 Go 抽象与一条可验收调用链。

各课程标签中的独立课件已汇总到[课件索引](README.md)。下表中的代码入口与验收命令以对应的 `XX-end` 标签为准，运行实验前先切换到该标签。

## 课时总览

| 课时 | 主题 | 本课核心交付 | 重点代码 | 最短验收 |
|---|---|---|---|---|
| 01 | 分层骨架与稳定端口 | Go Agent 的 domain/platform/runtime 分层 | `internal/domain`、`internal/platform`、`internal/runtime/engine` | `go test ./...` |
| 02 | 跨境业务状态与幂等 | 可重复调用且能检测参数冲突的库存调拨服务 | `projects/crossborder/internal/service` | `make -C projects/crossborder test` |
| 03 | HTTP Tool 契约 | 将订单、库存和调拨能力暴露为稳定 JSON API | `projects/crossborder/internal/httpapi`、`config/tools.json` | `make -C projects/crossborder test` |
| 04 | IAM 与 Workspace | JWT、五档角色和 Workspace 请求边界 | `internal/platform/iam`、`internal/api/middleware` | `go test ./internal/config ./internal/platform/iam ./internal/api/...` |
| 05 | Eino 模型网关 | Eino v0.9.15 标准 ChatModel 直连火山方舟豆包 | `internal/runtime/llm`、`internal/runtime/engine` | `go test ./internal/runtime/llm ./internal/runtime/engine`；配置 `ARK_API_KEY` 后执行 `make doubao-check` |
| 06 | SSE 流式对话 | `run_started/answer_delta/answer_done/run_finished/error` 事件和取消传播 | `internal/runtime/engine/chat.go`、`internal/api/sse.go` | `go test ./internal/runtime/engine ./internal/api` |
| 07 | Tool 版本治理 | 不可变 Tool Version、发布状态、Workspace 隔离、凭据加密和固定版本解析 | `internal/platform/tool` | `go test ./internal/platform/tool ./internal/api` |
| 08 | 安全执行与 Sandbox | REST Executor、主机 allowlist、重定向门禁和独立代码沙箱 | `internal/runtime/tooling`、`internal/runtime/sandbox`、`cmd/sandbox-runner` | `go test ./internal/runtime/tooling ./internal/runtime/sandbox` |
| 09 | Eino ADK ReAct | ChatModelAgent、Runner、ToolsNode 和最大步数控制 | `internal/runtime/engine/react.go` | `go test ./internal/runtime/engine` |
| 10 | Connector 与 ingest | Markdown 导入和可观察的 ingest 状态机 | `internal/connector`、`internal/platform/kb` | `go test ./internal/connector/... ./internal/platform/kb` |
| 11 | 混合检索 | BM25、向量召回和 Eino Retriever Router 的 RRF 融合 | `internal/runtime/retriever` | `go test ./internal/runtime/retriever ./internal/runtime/tooling` |
| 12 | Prompt 与 Model Profile | Eino ChatTemplate、不可变版本、ADK 重试与故障转移 | `internal/platform/prompt`、`internal/platform/modelconfig` | `go test ./internal/platform/prompt ./internal/platform/modelconfig ./internal/runtime/...` |
| 13 | SKILL.md | Eino Skill Middleware、渐进式披露、绑定 Tool 收窄及 KB/网络声明校验 | `internal/platform/skill`、`internal/runtime/skillrunner` | `go test ./internal/platform/skill ./internal/runtime/skillrunner ./internal/runtime/engine` |
| 14 | Agent Version 与会话快照 | 环境提升、版本固定和多轮历史恢复 | `internal/platform/agent`、`internal/runtime/engine` | `go test ./internal/platform/agent ./internal/runtime/engine ./internal/api` |
| 15 | PostgreSQL 持久化 | migration、Workspace 复合外键和 IAM/Agent PostgreSQL Store | `migrations`、`internal/infrastructure/postgres`、`internal/platform/agent`、`internal/platform/iam` | `go test ./cmd/migrate ./internal/infrastructure/postgres ./internal/platform/agent ./internal/platform/iam` |
| 16 | Approval Checkpoint | StatefulInterrupt 暂停敏感 Tool 并保存 ADK checkpoint | `internal/platform/approval`、`internal/runtime/engine` | `go test ./internal/platform/approval ./internal/runtime/engine` |
| 17 | A2UI 与恢复执行 | 受限审批卡片、`approved/rejected` 动作、精确 interrupt address 和 ResumeWithParams | `internal/a2ui`、`internal/runtime/engine/approval_worker.go` | `go test ./internal/a2ui ./internal/platform/approval ./internal/runtime/engine ./internal/api` |
| 18 | Guard 安全管线 | 输入输出 Hook、PII 处理和 Workspace 动态规则 | `internal/runtime/guard` | `go test ./internal/runtime/guard ./internal/api` |
| 19 | [Trace、Langfuse 与审计](langfuse.md) | Agent/LLM/Tool OTel Span、Langfuse Observation 和可验证审计链 | `internal/infrastructure/otel`、`internal/platform/audit`、`internal/runtime/engine`、`cmd/langfuse-demo` | `go test ./internal/infrastructure/otel ./internal/platform/audit ./internal/runtime/engine ./cmd/langfuse-demo` |
| 20 | Eval 发布门禁 | Dataset、Judge、通过率计算和版本发布约束 | `internal/platform/eval` | `go test ./internal/platform/eval ./internal/platform/agent ./internal/api` |
| 21 | [A2A 协议及物流示例](lesson-21-a2a-logistics.md) | AgentCard、SendMessage、外部物流 Mock、工具版本与身份边界 | `internal/runtime/tooling/a2a.go`、`internal/demo/lesson21`、`cmd/lesson21-demo` | `make lesson21-test` |
| 22 | [飞书创建应用与接入 kbot](lesson-22-feishu-kbot.md) | 企业自建应用、长连接、消息身份映射、Runtime 调用与回复 | `cmd/lesson22-demo`、`internal/integration/lark`、`internal/integration/dialogue.go` | `make lesson22-test` |
| 23 | 最终交付 | Admin、Compose、CI 和完整 Sandbox 环境 | `deploy`、`web/admin`、`.github/workflows` | `make verify` |

“最短验收”只列出无需外部凭据或长期服务即可执行的自动化证据。真实豆包需要个人 `ARK_API_KEY`；完整容器环境按 [`deploy/README.md`](../deploy/README.md) 操作。

## 课程阶段

| 阶段 | 课时 | 阶段结果 |
|---|---|---|
| Agent 地基 | 01–04 | 分层骨架、业务模拟器、Tool 契约和 Workspace 边界 |
| Runtime 主循环 | 05–09 | 模型、流式、Tool 版本、安全执行和 ReAct |
| 知识与配置快照 | 10–14 | RAG、Prompt、模型、Skill、Agent Version 与 Conversation |
| 持久化与人在环 | 15–17 | PostgreSQL、Checkpoint、A2UI 与恢复执行 |
| 安全与治理 | 18–20 | Guard、Trace、Audit、Eval |
| 外部服务与消息接入 | 21–22 | A2A 外部 Agent 调用与飞书消息收发 |
| 最终交付 | 23 | 保留原有 Admin、Compose、CI 和 Sandbox 交付 |

## 标签使用

第 21、22 课的新旧范围与起止快照见[代码快照说明](lesson-snapshots.md)。第 23 课继续使用原最终交付快照。

开始某一课：

```bash
git switch --detach 09-start
git switch -c practice/09
```

完成后对照标准答案：

```bash
git diff 09-end -- .
```

第 02、03 课的跨境业务包含独立 Go Module，测试时进入对应项目，或使用根 Makefile 提供的目标。新第 21、22 课均使用 `kbot-course` 根 Go Module。第 23 课保留原快照，其场景服务与交付检查继续使用该快照的配置。
