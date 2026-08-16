# 本地完整环境

```bash
cp .env.example .env
# 修改 KBOT_JWT_SECRET、管理员密码和 KBOT_SANDBOX_RUNNER_TOKEN 后启动
docker compose --env-file .env -f deploy/compose.yaml up --build
```

浏览器访问 `http://localhost:5173`。Admin Console 通过 Nginx 同源代理访问 API 和 SSE，Compose 会等待 PostgreSQL 迁移、Mock LLM、Sandbox Runner、跨境模拟器、保险模拟器与 API 健康检查通过。
首次登录使用 `.env` 中的 `KBOT_BOOTSTRAP_EMAIL` 与 `KBOT_BOOTSTRAP_PASSWORD`。服务启动时会创建管理员及其默认 Workspace。
Webhook 或飞书接入默认关闭；启用任一渠道时，需要同时配置对应密钥、`KBOT_CHANNEL_WORKSPACE_ID` 和 `KBOT_CHANNEL_AGENT_ID`。

Sandbox Runner 是独立服务，只有它挂载 Docker Socket。API 通过内部 HTTP 和 Bearer Token
调用 Runner，`/readyz` 会检查 Runner 到 Docker daemon 的完整连通性。第一次启动会拉取
固定 digest 的 Python 3.12.11 与 BusyBox 1.37.0 镜像，需要预留首次下载时间。

跨境模拟器和保险模拟器分别监听宿主机 `8091`、`8092`，容器内 Host 已加入 Tool allowlist。它们对应第 02/03 课和第 22 课的两套业务工具契约。

登录 Admin 后，在 Tools 页面注册 `code_execution` Tool，即可通过会话验证 Python 执行。
注册时使用 `source_type=code_execution`，`endpoint_config={"language":"python"}`，
Schema 使用 `{"type":"object","properties":{"code":{"type":"string"}},"required":["code"]}`。

停止服务：

```bash
docker compose --env-file .env -f deploy/compose.yaml down
```

需要同时删除课堂数据库卷时显式追加 `--volumes`。
