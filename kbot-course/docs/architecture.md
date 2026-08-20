# kbot 最小架构

第 01 课先建立四层依赖方向：

```text
API
 ↓
Platform（控制面：配置、版本、发布）
 ↓ immutable snapshot
Runtime（数据面：对话、检索、工具执行）
 ↓
Infrastructure
```

当前版本只定义控制面与 Runtime 之间的稳定接口。

Conversation 固定 AgentVersion ID。Runtime 每次都按照这个固定版本解析
AgentSnapshot，因此控制面发布新版本后，已有会话仍能保持原有行为。

## 独立 Sandbox Runner

`code_execution` Tool 通过内部 HTTP 调用 `cmd/sandbox-runner`。业务服务只持有
Runner URL 和 Bearer Token，Docker Socket 仅挂载到 Runner：

```text
Agent ReAct → Tool Executor → Sandbox HTTP Client
                              ↓ Bearer Token
                       sandbox-runner
                              ↓ Docker CLI
        Python/Bash 一次性容器（禁网、只读、非 root、资源受限）
```

课堂默认限制为 256MB 内存、0.5 CPU、64 PID、64MB `/tmp`、30 秒超时、
64KB 代码、1MB 输出和 4 个并发。执行请求只能提交语言与代码，无法覆盖这些限制。
Runner 启动时先清理孤儿容器、预拉取 digest 固定的 Python/Bash 镜像，并把镜像就绪
纳入 `/readyz`；并发槽满时立即返回 429，避免请求排队耗尽上游连接。

REST Tool 在注册后按固定版本执行。Executor 会执行完整 JSON Schema 校验、精确主机
allowlist、拨号前 DNS/IP 解析和同源重定向约束；Tool 凭据以 AES-GCM 密文留在注册表，
API 只暴露 `has_auth`，Runtime 解析固定版本后才取得凭据并写入受限 Header。
Docker Socket 权限很高，课堂与开发环境应限制 Runner 的网络入口；生产环境可以在
保持内部 HTTP 契约的前提下换用 gVisor、Kata Containers 或 Kubernetes 沙箱后端。

## PostgreSQL 主路径

第 15 课起，Server 启动时必须连接 PostgreSQL。用户、Workspace 成员、Agent、不可变
Agent Version、环境指针、Conversation 和 Message 都由数据库存储；内存实现继续作为
快速单元测试 fixture。Conversation 使用 `(agent_version_id, workspace_id, agent_id)`
复合外键固定同一 Workspace 内的真实版本，多轮消息按 `(created_at, id)` 稳定回放。

敏感 Tool 会把当时的消息、有效 Tool Version 集合和剩余步骤固化为 checkpoint。
A2UI action 只执行带身份与 Workspace 校验的审批状态迁移；后台 Approval Worker 从
PostgreSQL 扫描已批准任务，通过 lease + fencing token 抢占执行，成功后 CAS 完成，
失败按上限重试。HTTP 请求生命周期不会承载 Tool 副作用。

## OpenTelemetry 与审计

配置 `OTEL_EXPORTER_OTLP_ENDPOINT` 或 `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 后，
Server 会安装带批量导出的 OpenTelemetry SDK。将 OTLP/HTTP 地址指向 Langfuse，并通过
`OTEL_EXPORTER_OTLP_HEADERS` 提供认证信息。Agent Run 携带 Workspace、Agent Version、
Conversation 和 User 稳定维度，模型与工具操作作为子 Span 输出；高风险操作同时写入
按 Workspace 隔离的哈希链审计账本。
