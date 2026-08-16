# kbot Runbook（运行与上线检查）

> 本文覆盖本地 Compose、运行排障和生产部署边界。Kubernetes、HA、备份和灾备需要结合实际基础设施补充。

## 一、进程 / 资源
- `deploy/Dockerfile` 使用 Distroless 与 multi-stage build，只复制 Go 二进制和 React 构建产物。
- PID 1 是 server 进程，接 SIGTERM 优雅退出（`main.go` 已用 `signal.NotifyContext` + `srv.Shutdown`）。
- SSE/WS 长连接：`WriteTimeout` 必须为 0，避免整体写超时中断事件流；只设置 `ReadHeaderTimeout`。

## 二、外部依赖
- PostgreSQL：课堂 Compose 使用单实例；生产环境需要配置高可用、备份和恢复演练。
- Redis：课堂 Compose 使用单实例；生产环境需要根据可用性要求选择 Sentinel 或 Cluster。
- MinIO：课堂 Compose 使用单实例；生产环境需要配置独立凭据、冗余存储和生命周期策略。

## 三、配置（全部走环境变量，不进镜像）
| 变量 | 必填 | 说明 |
|---|---|---|
| `KBOT_DATABASE_URL` | 是 | Postgres 连接串 |
| `KBOT_REDIS_URL` | 是 | Redis 连接串 |
| `KBOT_CORS_ALLOWED_ORIGINS` | 否 | HTTP/SSE/WS 共用来源白名单，逗号分隔；默认仅本机 Admin 地址 |
| `KBOT_LLM_API_KEY` | 否 | 旧全局 LLM 回退密钥；新项目优先使用 Provider Account |
| `KBOT_JWT_SECRET_KEY` | 是 | ≥32 字符 |
| `KBOT_CREDENTIAL_ENCRYPTION_KEY` | 是 | Provider API Key 的 AES-GCM 加密密钥，至少 32 字符且不得与 JWT 密钥复用 |
| `KBOT_OTLP_ENDPOINT` | 否 | 空则禁用 trace 导出 |
| `KBOT_OTLP_HEADERS` | 否 | OTLP/HTTP 请求头，逗号分隔 `key=value`；Langfuse 使用 Basic Auth |
| `KBOT_OTEL_SAMPLE_RATIO` | 否 | Trace 采样比例 0..1 |
| `KBOT_OTEL_CAPTURE_CONTENT` | 否 | 是否采集模型输入输出，默认 `false` |
| `KBOT_SERVICE_VERSION` | 否 | OTel service.version 与 Langfuse release |
| `KBOT_LANGFUSE_UI_URL` | 否 | 浏览器可访问的 Langfuse 地址，用于 Admin Trace 深链 |
| `KBOT_LANGFUSE_PROJECT_ID` | 否 | Langfuse project ID，用于 Admin Trace 深链 |
| `KBOT_WEBHOOK_SECRET` | 否 | webhook 触发器 HMAC |
| `KBOT_LARK_VERIFY_TOKEN` | 否 | 飞书事件订阅校验 |
| `KBOT_LARK_ENCRYPT_KEY` | 否 | 飞书事件订阅签名校验与 AES-256-CBC 载荷解密 |
| `KBOT_LARK_APP_ID` / `KBOT_LARK_APP_SECRET` | 否 | 飞书机器人出站回复凭证 |
| `KBOT_LARK_AGENT_ID` | 否 | 飞书消息默认触发的 Agent ID |
| `KBOT_AUTOSEED_ADMIN` | 否 | 首启无 admin 用户则自动建一个(仅 dev,prod 置 `false`) |
| `KBOT_EMBEDDER` | 否 | KB 向量化:`local`(默认,离线确定性)/ `openai`(走 LLM Gateway /embeddings,需真 key) |
| `KBOT_EMBEDDER_DIM` | 否 | 向量维度,必须 == `kb_chunks.embedding vector(1536)`;默认 1536 |
| `KBOT_EMBEDDER_MODEL` | 否 | openai 模式的 embedding 模型名,默认 `text-embedding-3-small` |
| `KBOT_S3_ENDPOINT` | 否 | MinIO/S3 端点；空或连接失败时审计归档/导出禁用，数据库分区保持原样 |
| `KBOT_S3_BUCKET` / `_ACCESS_KEY` / `_SECRET_KEY` | 否 | 对象存储桶与凭证(导出用;归档固定用 `kbot-archive` 桶) |
| `KBOT_AUDIT_ARCHIVE_AFTER_MONTHS` | 否 | 分区超过该月龄归档 MinIO 后 detach+drop,默认 13 |

生产建议：`KBOT_ENVIRONMENT=prod`、`KBOT_OTEL_SAMPLE_RATIO=0.1`、
`KBOT_OTEL_CAPTURE_CONTENT=false`、`KBOT_AUTOSEED_ADMIN=false`。

## 数据库迁移(golang-migrate + sqlc)

- **迁移文件**:`migrations/{000001..000026}_{name}.up.sql / .down.sql`,golang-migrate 顺序应用。
- **docker compose**:专用 `migrate` 服务在 `db` healthy 后跑一次 `-up` 即退出;`app`/`worker`
  以 `depends_on: migrate (service_completed_successfully)` 等它跑完,保证 schema 先就位。
  - 因此 dev 环境**默认每次 `make up` 都会过一遍迁移**(已应用的会跳过,幂等)。
  - 生产发布建议把 `migrate` 作为显式部署步骤，并在应用实例切流前确认迁移完成。
- **本地手动**：完整课堂环境使用 `make migrate`，轻量环境使用 `make migrate-lite`，均通过 Compose
  中的 `migrate` 服务执行 `-up`。需要回退或查看版本时，使用对应 Compose project 执行
  `docker compose ... run --rm migrate -down 1` 或 `-version`。
- **sqlc 代码生成**:`make sqlc-generate`。命令优先使用本机 `sqlc`，缺少二进制时通过 Docker 固定使用
  `sqlc/sqlc:1.27.0`。生成代码已提交，学员修改 SQL 后才需要重新生成。

### 集成测试(store contract test,需 Docker)

- `make test-integration`(= `go test -race -tags=integration -count=1 -p 1 ./...`)。普通 `make test` 不需要 Docker;
  带 `integration` tag 的 contract test(memory + postgres 双实现)才需要真 PG。
- 测试用 `ory/dockertest` 自动起 `pgvector/pgvector:pg16` 并跑迁移;**本地迭代可复用常驻容器加速**:
  ```bash
  docker run -d --name kbot_testpg -e POSTGRES_USER=kbot -e POSTGRES_PASSWORD=kbot -e POSTGRES_DB=kbot \
    -p 55432:5432 pgvector/pgvector:pg16
  KBOT_TEST_DATABASE_URL=postgres://kbot:kbot@localhost:55432/kbot?sslmode=disable make test-integration
  ```
  `KBOT_TEST_DATABASE_URL` 仅测试用、非运行时配置;留空则走 dockertest 自动起容器。

## 四、回滚
- 配置回滚靠**环境指针**：把 `agent_envs.version_id` / `prompt_envs.version_id` 切回旧版即可（秒级，无需发版）。
- 迁移用 `migrate down` 可逆。
- 镜像回滚靠重新部署旧 tag。

## 多 Provider 数据分级路由

- 新路径：Workspace Provider Account → Model Deployment → immutable Model Profile Version →
  Prompt Version。主部署失败后按 Profile 的 Fallback 顺序重试。
- API Key 只在创建或轮换时输入，加密保存且不回显；轮换后引用该账号的后续调用自动使用新 Key。
- Prompt 灰度在 Conversation 创建时解析并固定，转全后 Candidate 成为新 Baseline。
- `model_call_logs` 记录 Prompt Version、Profile Version、Deployment 与实验 Variant。
- 未绑定 Model Profile 的旧 Prompt 仍使用下述全局云/Ollama回退。
- LLM 网关两路 Provider:**云**(`KBOT_LLM_*`,DeepSeek 等 OpenAI 兼容)与**本地**(`KBOT_OLLAMA_*`,Ollama 的 OpenAI 兼容 `/v1`)。
- 全局回退路由规则:会话 `classification == secret` 只允许本地 Ollama;其余分级走云。
  OTel `gen_ai.system` 反映所选(`ollama` / `openai-compatible`),`model_call_logs.classification` 记录分级。
- `KBOT_OLLAMA_BASE_URL` 留空会禁用本地模型;secret 请求会返回错误,避免敏感内容被送往云端。
- 显式绑定 Model Profile 的请求由 `classification_max` 限制可处理的最高数据分级。
- 模型拉取：基础 Compose 默认不启动 Ollama。使用 `--profile local-llm` 启动后，`ollama-init` 会拉取配置的模型；手动拉取或更新可运行 `bash scripts/ollama-pull.sh`。
- **国内加速 ollama pull**:`registry.ollama.ai` 在大陆可能慢或超时。可以给主机或容器配置代理，也可以在有网络的机器预拉镜像和模型；低配置环境可使用 `KBOT_OLLAMA_MODEL=qwen2.5:3b`。

## Agent 与 Skill 版本发布

- Agent 新版本会固化 Prompt 配置、Tool Version、Skill Version、KB ID、网络权限和最大步数，并自动成为 `dev` 当前版本。
- 在 Agent 详情页把已验证版本晋升到 `staging` 或 `prod`；环境指针更新只影响后续新会话和后续 Team 快照。
- Skill 只能绑定已发布版本。Skill 引用的 `allowed-tools` 与 `allowed-kbs` 必须同时挂载到 Agent。
- `allowed-kbs` 填 Knowledge Base ID。KB 检索工具执行前同时检查 Agent KB allowlist 和活动 Skill KB allowlist。
- Agent 未授权网络时，REST、MCP 和 A2A 工具在审批前被拒绝；历史快照没有该字段时保留原运行语义。
- `disable-model-invocation: true` 禁止模型自动发现和激活 Skill；人工调用格式为 `/skill <name>` 或 `/<name>`。
- 审批 checkpoint 保存活动 Skill 名称，Worker 恢复后重建 Tool、KB 与网络约束，再执行已批准操作。

## 五、断网演练
- 把 LLM 出口 iptables 切断 → 验证分级路由是否降级到本地模型。
- 干掉 Redis → 验证 Prompt 缓存的 stale fallback（用上一次成功拉到的版本）。

## 六、健康与可观测入口
- `GET /metrics`：Prometheus 指标（chat 请求数 / 工具调用 / 注入拦截 / 审计丢弃等）。
- `GET /health`、`GET /healthz`：进程存活检查，不访问外部依赖。
- `GET /readyz`：就绪检查；PostgreSQL 或 Redis 不可用时返回 `503`。
- trace：OTLP 导出到 Langfuse/Phoenix；Langfuse 使用 conversation ID 作为 session ID 聚合排查。
- `GET /api/v1/audit/logs?conversation_id=...`：对话轨迹。

本机课堂环境可执行：

```bash
make langfuse-preflight
make up
make demo
```

完整实验和排障步骤见 [`docs/labs/langfuse-a2ui-demo.md`](labs/langfuse-a2ui-demo.md)。课堂 profile
开启全采样和内容采集；生产配置应按数据分级与合规要求降低采样或关闭内容采集。

轻量运行核心服务时使用 `make up-lite`，配套状态、日志和停止命令为 `make ps-lite`、`make logs-lite`、
`make down-lite`。两套环境共用默认宿主机端口，切换前需要停止当前环境。

## 七、协议型工具

- MCP：App/Worker 支持规范 `2025-11-25` 的 Streamable HTTP；stdio 只允许放在独立连接器 Runner，配置见
  [`docs/integrations/mcp.md`](integrations/mcp.md)。
- A2A：支持 v1.0.1 JSON-RPC `SendMessage`，配置见
  [`docs/integrations/a2a.md`](integrations/a2a.md)。
- 两类远端调用都应配置超时、最小权限鉴权和 Audit；高风险 Tool 同样应标记 `sensitive=true` 走审批。
