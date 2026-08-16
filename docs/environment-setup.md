# 环境准备

## 必需工具

| 工具 | 推荐版本 | 用途 |
|---|---|---|
| Git | 2.40+ | 切换课程标签和管理练习分支 |
| Go | 1.26.5 | 编译和测试三个 Go Module |
| Node.js | 22 LTS | 构建辅助 Admin Console |
| Docker | 27+ | 运行 PostgreSQL、Redis、Sandbox 和课堂服务 |
| Docker Compose | v2.35+ | 启动多服务环境 |

完整环境建议为 Docker 分配 12 GiB 以上内存；同时打开 Admin 与 Langfuse 时建议 16 GiB。

## 克隆与基础检查

```bash
git clone git@github.com:Q1mi/kbot.git
cd kbot
go version
node --version
docker version
docker compose version
```

开始课程：

```bash
git switch --detach 01-start
git switch -c practice/01
cd kbot-course
go test ./...
```

使用最终稳定版：

```bash
git switch master
cd kbot-final
cp .env.example .env
make bootstrap
```

## 模型配置

课程核心测试使用确定性 Mock LLM，无需公网模型 Key。需要演示 DeepSeek、豆包等 OpenAI-compatible 模型时，在本地 `.env` 中配置：

```bash
KBOT_LLM_BASE_URL=https://api.deepseek.com/v1
KBOT_LLM_API_KEY=<your-api-key>
KBOT_LLM_MODEL=deepseek-chat
```

`.env` 已加入忽略规则。不要在终端日志、截图、课程作业或 Git 提交中暴露真实密钥。

## 首次启动检查

```bash
cd kbot-final
make build
make test
docker compose -f deploy/docker-compose.yml --env-file .env.example config --quiet
```

完整课堂环境使用 `make up`，轻量环境使用 `make up-lite`。两套环境共享默认宿主机端口，切换前先停止当前环境。
