# kbot-course

## 课程资料

- [课程地图](docs/course-map.md)
- [第 21 课：A2A 协议与物流示例](docs/lesson-21-a2a-logistics.md) · [运行说明](examples/lesson21/README.md)
- [第 22 课：飞书创建应用与接入 kbot](docs/lesson-22-feishu-kbot.md) · [运行说明](examples/lesson22/README.md)

第 21 课可直接打开两个终端演示：

```bash
# 终端 A：外部物流服务
make logistics-mock-run
# 终端 B：kbot Runtime 通过 A2A 查询物流
make a2a-demo
```

第 22 课飞书接入在本地配置 `KBOT_LARK_APP_ID`、`KBOT_LARK_APP_SECRET` 后运行 `make feishu-demo`。应用权限、长连接订阅和发布步骤见第 22 课示例文档。使用 `ARGS='-live-model'` 可切换到配置的大模型。

## 豆包模型准备

从第 05 课开始，课堂运行链路直接连接火山方舟豆包模型。运行前在当前终端配置：

```bash
export ARK_API_KEY="你的方舟 API Key"
export KBOT_LLM_MODEL="doubao-seed-2-0-lite-260215"
```

`KBOT_LLM_BASE_URL` 默认使用 `https://ark.cn-beijing.volces.com/api/v3`。密钥只通过环境变量注入。

从第 08 课开始，代码执行由独立 Sandbox Runner 承担。先设置内部 Token，
再启动 Runner；API 和 Runner 必须使用同一个值：

```bash
export KBOT_SANDBOX_RUNNER_TOKEN="replace-with-at-least-32-characters"
make sandbox-runner-run
```

Runner 需要本机 Docker daemon，并会为每次 Python/Bash 调用创建一个禁网、
只读、非 root、资源受限的一次性容器。

本仓库保存《Go 企业级 AI Agent 平台实战》的逐课代码。

课程从最小 HTTP 服务开始，每一课使用两个标签：

```text
01-start → 01-end
02-start → 02-end
...
```

- `XX-start`：本课开始前可编译、可测试的代码；
- `XX-end`：完成本课核心实现并通过验收的代码。

## 第 01 课起点

`01-start` 只提供：

- Go Module；
- 最小 Makefile；
- `/healthz`；
- SIGINT/SIGTERM 与优雅退出。

第 01 课结束后，项目将增加：

- `internal/domain`：最小领域对象；
- `internal/platform`：控制面入口；
- `internal/runtime/engine`：Runtime 稳定接口；
- Conversation 固定 AgentVersion 的自动化测试。

## 基础命令

```bash
make build
make test
make run
```

## Admin Console

`web/admin` 从第 04 课开始直接提供最终 React Admin 源码。前端用于调用 API、查看会话、审批、评测和 Trace，课程现场编码聚焦 Go Agent 后端。
