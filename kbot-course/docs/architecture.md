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
