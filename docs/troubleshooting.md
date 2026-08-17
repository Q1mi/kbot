# 常见问题与排障

## 切换标签后处于 detached HEAD

课程标签是只读检查点。创建个人分支后再编码：

```bash
git switch --detach 08-start
git switch -c practice/08
cd kbot-course
```

## Go 版本不匹配

```bash
go version
cat go.mod | head
```

课程和最终版统一使用 Go 1.26.5。升级工具链后重新执行 `go mod download`。

## 前端依赖或构建失败

```bash
cd web/admin
npm ci
npm run build
```

课程固定使用 `package-lock.json`。`node_modules/` 和 `dist/` 都属于本地产物，不应提交。

## Compose 提示缺少环境变量

```bash
cp .env.example .env
docker compose --env-file .env -f deploy/docker-compose.yml config --quiet
```

课程终态使用 `deploy/compose.yaml`，最终版使用 `deploy/docker-compose.yml`。请在对应项目目录执行命令。

## 端口被占用

默认端口包括 8080、3000、5173、5432、6379、8090、8091、8092、9000 和 9001。先停止已有课堂环境：

```bash
make down
make down-lite
```

## Sandbox Runner 无法执行代码

确认 Docker daemon 可用，并保证 App 与 Runner 使用相同 Token：

```bash
docker info
export KBOT_SANDBOX_RUNNER_TOKEN="replace-with-at-least-32-characters"
make sandbox-runner-run
```

Runner 执行容器需要预拉取固定 digest 的 Python 和 BusyBox 镜像。Docker Socket 只应挂载到 Runner。

## 真实模型调用失败

依次检查 Base URL、模型名、API Key、网络代理、方舟账号余额和模型开通状态。先运行 `go test ./internal/runtime/llm` 验证 Gateway，再执行 `make doubao-check` 定位真实网络调用问题。

## 数据库迁移或测试状态污染

课堂练习使用独立数据库或 Compose project。需要重新开始时先停止环境，再按对应项目文档清理课堂数据卷。执行删除数据卷命令前确认项目名，避免影响其他环境。

## 快速定位失败范围

```bash
go test ./目标包
go test -race -count=1 ./...
git diff --check
```

最终版还可以执行：

```bash
make test-integration
docker compose -f deploy/docker-compose.yml --env-file .env.example config --quiet
```
