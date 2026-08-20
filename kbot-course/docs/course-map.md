# kbot 课程地图

本仓库对应《Go 企业级 AI Agent 平台实战》的 23 节边敲边讲课程。课程从最小 Go HTTP 服务开始，逐步完成 Agent Runtime、Tool、RAG、Skill、人在环审批、安全治理、协议集成和容器化交付。

- 每课从 `XX-start` 开始，完成后对照 `XX-end`。
- `start` 已包含本课所需的接口、测试脚手架、静态数据和辅助前端。
- 现场编码聚焦本课的核心 Go 抽象与一条可验收调用链。

## 课时总览

| 课时 | 主题 | 本课核心交付 | 重点代码 | 最短验收 |
|---|---|---|---|---|
| 01 | 分层骨架与稳定端口 | Go Agent 的 domain/platform/runtime 分层 | `internal/domain`、`internal/platform`、`internal/runtime/engine` | `go test ./...` |
| 02 | 跨境业务状态与幂等 | 可重复调用且能检测参数冲突的库存调拨服务 | `projects/crossborder/internal/service` | `make -C projects/crossborder test` |
| 03 | HTTP Tool 契约 | 将订单、库存和调拨能力暴露为稳定 JSON API | `projects/crossborder/internal/httpapi`、`config/tools.json` | `make -C projects/crossborder test` |
| 04 | IAM 与 Workspace | JWT、五档角色和 Workspace 请求边界 | `internal/platform/iam`、`internal/api/middleware` | `go test ./internal/config ./internal/platform/iam ./internal/api/...` |
| 05 | Eino 模型网关 | Eino v0.9.15 标准 ChatModel 直连火山方舟豆包 | `internal/runtime/llm`、`internal/runtime/engine` | `go test ./internal/runtime/llm ./internal/runtime/engine`；配置 `ARK_API_KEY` 后执行 `make doubao-check` |
| 06 | SSE 流式对话 | 增量事件、结束状态和取消传播 | `internal/runtime/engine/chat.go`、`internal/api/sse.go` | `go test ./internal/runtime/engine ./internal/api` |
| 07 | Tool 版本治理 | Tool Version、试调、发布和固定版本引用 | `internal/platform/tool` | `go test ./internal/platform/tool ./internal/api` |
| 08 | 安全执行与 Sandbox | REST Executor、SSRF 门禁和独立代码沙箱 | `internal/runtime/tooling`、`internal/runtime/sandbox`、`cmd/sandbox-runner` | `go test ./internal/runtime/tooling ./internal/runtime/sandbox` |
| 09 | Eino ADK ReAct | ChatModelAgent、Runner、ToolsNode 和最大步数控制 | `internal/runtime/engine/react.go` | `go test ./internal/runtime/engine` |
| 10 | Connector 与 ingest | Markdown 导入和可观察的 ingest 状态机 | `internal/connector`、`internal/platform/kb` | `go test ./internal/connector/... ./internal/platform/kb` |
| 11 | 混合检索 | BM25、向量召回和 Eino Retriever Router 的 RRF 融合 | `internal/runtime/retriever` | `go test ./internal/runtime/retriever ./internal/runtime/tooling` |
| 12 | Prompt 与 Model Profile | Eino ChatTemplate、不可变版本、ADK 重试与故障转移 | `internal/platform/prompt`、`internal/platform/modelconfig` | `go test ./internal/platform/prompt ./internal/platform/modelconfig ./internal/runtime/...` |
| 13 | SKILL.md | Eino Skill Middleware、渐进式披露和最小权限 | `internal/platform/skill`、`internal/runtime/skillrunner` | `go test ./internal/platform/skill ./internal/runtime/skillrunner ./internal/runtime/engine` |
| 14 | Agent Version 与会话快照 | 环境提升、版本固定和多轮历史恢复 | `internal/platform/agent`、`internal/runtime/engine` | `go test ./internal/platform/agent ./internal/runtime/engine ./internal/api` |
| 15 | PostgreSQL 持久化 | migration、复合外键和 PostgreSQL Store | `migrations`、`internal/infrastructure/postgres` | `go test ./cmd/migrate ./internal/infrastructure/postgres ./internal/platform/agent ./internal/platform/iam` |
| 16 | Approval Checkpoint | StatefulInterrupt 暂停敏感 Tool 并保存 ADK checkpoint | `internal/platform/approval`、`internal/runtime/engine` | `go test ./internal/platform/approval ./internal/runtime/engine` |
| 17 | A2UI 与恢复执行 | 审批卡片、精确 interrupt address 和 ResumeWithParams | `internal/a2ui`、`internal/runtime/engine/approval_worker.go` | `go test ./internal/a2ui ./internal/platform/approval ./internal/runtime/engine ./internal/api` |
| 18 | Guard 安全管线 | 输入输出 Hook、PII 处理和 Workspace 动态规则 | `internal/runtime/guard` | `go test ./internal/runtime/guard ./internal/api` |
| 19 | Trace 与审计 | OTel Trace 和可验证的审计链 | `internal/infrastructure/otel`、`internal/platform/audit` | `go test ./internal/infrastructure/otel ./internal/platform/audit ./internal/runtime/engine` |
| 20 | Eval 发布门禁 | Dataset、Judge、通过率计算和版本发布约束 | `internal/platform/eval` | `go test ./internal/platform/eval ./internal/platform/agent ./internal/api` |
| 21 | Team 与开放协议 | AgentTool Supervisor、Eino MCP Adapter、A2A、Webhook 和飞书 | `internal/runtime/team`、`internal/runtime/tooling`、`internal/integration` | `go test ./internal/runtime/team ./internal/platform/team ./internal/runtime/tooling ./internal/integration/...` |
| 22 | 保险场景迁移 | 理赔责任、赔款计算和反欺诈分流 | `projects/insurance` | `make -C projects/insurance test` |
| 23 | 最终交付 | Admin、Compose、CI 和完整 Sandbox 环境 | `deploy`、`web/admin`、`.github/workflows` | `make verify` |

## 课程阶段

| 阶段 | 课时 | 阶段结果 |
|---|---|---|
| Agent 地基 | 01–04 | 分层骨架、业务模拟器、Tool 契约和 Workspace 边界 |
| Runtime 主循环 | 05–09 | 模型、流式、Tool 版本、安全执行和 ReAct |
| 知识与配置快照 | 10–14 | RAG、Prompt、模型、Skill、Agent Version 与 Conversation |
| 持久化与人在环 | 15–17 | PostgreSQL、Checkpoint、A2UI 与恢复执行 |
| 治理与开放能力 | 18–21 | Guard、Trace、Audit、Eval、Team 和协议集成 |
| 迁移与交付 | 22–23 | 第二业务验证与 Docker Compose 总装 |

## 标签使用

开始某一课：

```bash
git switch --detach 09-start
git switch -c practice/09
```

完成后对照标准答案：

```bash
git diff 09-end -- .
```

第 02、03、22 课包含独立 Go Module，测试时进入对应项目，或使用根 Makefile 提供的目标。
