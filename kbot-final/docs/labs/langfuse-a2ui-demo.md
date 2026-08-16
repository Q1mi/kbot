# Langfuse + A2UI 企业审批课堂实验

这节实验用一套本地 Docker Compose 环境展示企业 Agent 的完整运行链路：多轮 SSE 对话、Guard、
LLM Generation、敏感工具暂停、A2UI 人工审批、checkpoint 恢复执行和 Langfuse 追踪。

## 1. 学习目标

完成实验后，学员能够：

1. 用 OpenTelemetry 将 Go Agent 的 Trace 直接写入自托管 Langfuse；
2. 从一次聊天中识别 Guard、Generation、Tool 和 Approval 等观测节点；
3. 理解异步续跑为什么会跨 Trace，以及如何使用 conversation/session 关联；
4. 使用 A2UI 的 surface、component tree、data model 和 action 构建受控生成式 UI；
5. 解释敏感操作从暂停、审批到幂等恢复的安全边界。

## 2. 环境要求

- Docker Desktop 或 Docker Engine + Compose v2；
- Docker 可用内存至少 12 GiB，课堂投屏推荐 16 GiB；
- 本机默认端口可用：`8080`、`8090`、`3000`、`5432`、`6379`、`9000`、`9001`、`9090`、`9091`；
- 首次启动可以访问镜像仓库。

先运行只读预检：

```bash
make langfuse-preflight
```

若端口被占用，可在所有命令前使用同一组覆盖变量。例如：

```bash
export LANGFUSE_PORT=3100
export LANGFUSE_MINIO_PORT=9190
export LANGFUSE_MINIO_CONSOLE_PORT=9191
export KBOT_MINIO_PORT=9100
export KBOT_MINIO_CONSOLE_PORT=9101
make langfuse-preflight
```

## 3. 启动与造数

```bash
make up
make demo
```

首次启动会拉取 Langfuse、ClickHouse 等镜像并构建 kbot。`make up` 会等待 Mock LLM、Langfuse 和
kbot readiness 全部通过。`make demo` 会自动完成以下动作：

1. 注册课堂教师账号并创建独立 Workspace；
2. 注册、测试和发布 `refund_order` 敏感 REST Tool；
3. 创建绑定该 Tool 的“企业退款助手”；
4. 发送普通问答并记录 Trace ID；
5. 发送退款请求，断言 SSE 中出现三类 A2UI 消息；
6. 通过 A2UI action 批准审批，等待 worker 从 checkpoint 恢复；
7. 发送 Prompt Injection，验证 Guard 拦截。

入口与账号：

| 系统 | 地址 | 账号 |
|---|---|---|
| kbot Admin | `http://localhost:8080` | `teacher@kbot.local / admin12345` |
| Langfuse | `http://localhost:3000` | `admin@kbot.local / admin12345` |

覆盖端口后，将地址中的端口替换为实际值。以上凭证只用于本机课堂环境。

## 4. 前端演示脚本

登录 kbot Admin 后：

1. 顶部选择“跨境电商运营平台”Workspace；
2. 进入“会话”，选择“企业退款助手”；
3. 输入“请为订单 KBOT-2026-001 办理退款”；
4. 观察会话状态变为“运行已暂停，等待审批”，可在“运行明细”中查看完整事件顺序；
5. 观察紧跟本次请求的“敏感操作审批”卡片，其中包含风险提示、业务摘要、折叠技术参数和两个动作；
6. 点击 Trace 标签，浏览器会打开对应的 Langfuse Trace；
7. 点击“批准并执行”，观察卡片依次进入执行中、执行完成状态，最终退款结果出现在卡片下方。

这个页面体现了四个前端能力：

- 多轮消息复用同一个 `conversation_id`；
- SSE 运行事件实时转换为状态标签；
- A2UI `createSurface`、`updateComponents`、`updateDataModel` 合并为本地 surface state；
- 会话消息与 A2UI surface 进入同一有序时间线，待审批期间锁定会话输入；
- Client-to-Server action 经授权 API 执行，并通过 data model 更新原卡片。

课堂 Mock LLM 会从输入中提取 `KBOT/ORD/ORDER` 订单号、退款金额和“原因”文本；建议演示时同时核对
用户消息与卡片业务摘要，说明模型生成的工具参数必须在人工审批前清晰呈现。

## 5. Langfuse 讲解路径

在 Langfuse 的 Traces 页面搜索 `langfuse-demo.sh` 输出的 Trace ID，依次展开：

- `agent.chat`：一次 Agent 运行的业务根节点；
- `guard.input` / `guard.output`：输入输出安全处理；
- `chat kbot-classroom-mock`：模型 Generation，可查看输入、输出、tool call、token usage 和 cached tokens；
- `refund_order`：敏感工具暂停点，metadata 中可见 `approval.status=pending`；
- worker 侧 `agent-resume` 与 `approval.resume`：审批后的异步恢复和工具执行。

server 和 worker 跨越 Asynq 队列，因此恢复过程会产生一条新 Trace。两条 Trace 使用相同
conversation ID 作为 Langfuse session ID。课程可以在这里延伸讲解 trace propagation、消息头透传和
异步因果链建模。

## 6. A2UI 消息拆解

审批 UI 由三条 Server-to-Client 消息组成：

```json
{"version":"v0.9","createSurface":{"surfaceId":"approval-<id>","catalogId":"https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json"}}
{"version":"v0.9","updateComponents":{"surfaceId":"approval-<id>","components":["Card / Column / Text / Button ..."]}}
{"version":"v0.9","updateDataModel":{"surfaceId":"approval-<id>","value":{"status":"pending","tool":{}}}}
```

组件属性通过 JSON Pointer 绑定数据模型。按钮只允许 `approval.approve` 和 `approval.reject` 两个动作。
浏览器提交 action 时携带 `surfaceId`、`sourceComponentId`、时间戳与 approval/conversation context；API
会与数据库记录逐项核对。

建议课堂中修改一项 action context 后用 `curl` 重放，让学员观察 API 返回 403/400，再回到 UI 完成正常
审批。这能直观看出声明式 UI 与后端授权校验各自承担的职责。

## 7. 常用运维命令

```bash
make ps                # 查看容器状态
make logs              # 跟随 kbot、worker、Langfuse 日志
make down              # 停止容器，保留数据卷
make langfuse-reset    # 删除课堂容器和数据卷，重新开始
```

排障顺序：

1. `make langfuse-preflight` 检查端口、Docker daemon 和 Compose；
2. `make ps` 确认依赖服务健康；
3. `make logs` 查找迁移、OTLP 或 worker 错误；
4. 访问 `http://localhost:3000/api/public/health` 和 `http://localhost:8080/readyz`；
5. 修改 Compose 版本后执行 `make langfuse-reset`，避免旧数据结构影响课堂。

## 8. 生产化讨论清单

课堂 profile 为了投屏效果开启了全采样和 Prompt/响应内容采集。迁移到企业环境时需要评审：

- PII/Secret 脱敏、内容采集范围和数据驻留；
- 动态采样、保留期限、成本预算和失败降级；
- Langfuse SSO/RBAC、TLS、密钥轮换、备份和高可用；
- Trace 与审批审计记录的访问权限；
- action 幂等键、重放保护、审批超时和职责分离；
- Catalog 版本治理，以及每个新组件/action 的白名单和安全测试。

协议、部署边界与操作步骤以本实验手册和根目录 README 为准。
