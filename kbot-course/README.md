# kbot-course

## 课程资料

- [课程地图](docs/course-map.md)

本仓库保存《Go 企业级 AI Agent 平台实战》的逐课代码。

课程从最小 HTTP 服务开始，每一课使用两个标签：

```text
01-start → 01-end
02-start → 02-end
...
```

- `XX-start`：本课开始前可编译、可测试的代码；
- `XX-end`：完成本课核心实现并通过验收的代码。

## 当前起点

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
