# 第 21 课示例：A2A 调用外部物流服务

本课只演示 A2A 外部服务调用。`cmd/logistics-mock` 提供独立物流进程，`cmd/lesson21-demo` 通过正式 Engine、Tool Registry 和 Executor 查询物流。默认使用确定性模型，无需 PostgreSQL、JWT、Sandbox Runner 或模型 Key。

## 1. 通过 A2A 调用外部物流 Mock

在 `kbot-course` 目录（含 Makefile）打开两个终端。首次运行会下载 Go Module 依赖，Go 版本依据根目录 `go.mod`。

终端 A，启动独立物流进程：

```bash
make logistics-mock-run
```

该进程默认监听 `127.0.0.1:8082`，提供三个接口：

| 接口 | 用途 |
| --- | --- |
| `GET /healthz` | 健康检查 |
| `GET /.well-known/agent-card.json` | 声明 JSONRPC、协议版本 `1.0` 和 `/a2a` 地址 |
| `POST /a2a` | 处理 JSON-RPC `SendMessage`，查询固定演示订单 |

终端 B，运行 kbot：

```bash
make a2a-demo
```

关键输出如下，实际 ID 和工具事件字段以运行结果为准：

```text
model mode=demo (deterministic tool call; no LLM API key required)
workspace=lesson21-workspace ... agent_version=lesson21-agent-v1 tool_version=logistics-a2a-tool-v1 remote_version=logistics-v1
问题：查询订单 CB-1001 的物流信息
[run_started] ...
[tool_started] ...
[tool_finished] ...
回答：【演示物流数据】订单 CB-1001，运单 KB20260910001：清关中。当前位置：洛杉矶海关。申报资料正在审核，请等待承运商确认放行。
[run_finished] {"status":"completed"}
```

查询其他订单：

```bash
make a2a-demo ARGS='-message "查询 CB-1002"'
make a2a-demo ARGS='-message "查询 CB-1003"'
make a2a-demo ARGS='-message "查询 CB-9999"'
```

| 订单号 | 预期状态 |
| --- | --- |
| `CB-1001` | 清关中，洛杉矶海关 |
| `CB-1002` | 运输中，上海转运中心 |
| `CB-1003` | 已签收，深圳南山 |
| `CB-9999` | 未找到订单 |

### 直接观察协议请求

```bash
curl -sS http://127.0.0.1:8082/.well-known/agent-card.json

curl -sS http://127.0.0.1:8082/a2a \
  -H 'Content-Type: application/json' \
  -H 'A2A-Version: 1.0' \
  -d '{
    "jsonrpc":"2.0",
    "id":"demo-1",
    "method":"SendMessage",
    "params":{
      "message":{
        "role":"ROLE_USER",
        "messageId":"msg-demo-1",
        "parts":[{"text":"查询 CB-1001"}]
      },
      "metadata":{"agent_version_id":"logistics-v1"}
    }
  }'
```

将 `agent_version_id` 改为 `logistics-v2`，Mock 返回 JSON-RPC 错误，证明服务端校验了本课版本约定。`metadata.agent_version_id` 和回复中的 `executed_agent_version_id` 是本项目的业务约定。A2A 定义通信格式；远端业务版本由双方共同约束。本示例实现 AgentCard 和同步文本响应，异步 Task、流式传输和远端认证留作后续扩展。[A2A 官方规范](https://a2a-protocol.org/latest/specification/)

使用其他本机端口时，在两个终端对应设置：

```bash
# 终端 A
make logistics-mock-run ARGS='-addr 127.0.0.1:18082 -public-url http://127.0.0.1:18082'
# 终端 B
make a2a-demo ARGS='-card-url http://127.0.0.1:18082/.well-known/agent-card.json'
```

## 2. 使用真实模型

配置 `ARK_API_KEY` 或 `KBOT_LLM_API_KEY` 后执行：

```bash
make a2a-demo ARGS='-live-model -message "查询 CB-1001"'
```

默认模式会将普通文本转换为物流 tool_call；真实模型模式由模型选择工具。物流 Mock 继续保留演示数据。

## 3. 代码与验收

- [独立物流服务](../../internal/demo/logistics/service.go)
- [A2A 演示入口](../../cmd/lesson21-demo/main.go)
- [Runtime 与版本装配](../../internal/demo/lesson21/runtime.go)
- [A2A Client](../../internal/runtime/tooling/a2a.go)

```bash
make lesson21-test
```

正式讲解见[第 21 课课件](../../docs/lesson-21-a2a-logistics.md)。下一课在同一个物流 Runtime 上增加飞书入口，请切换到 `22-end` 阅读 `examples/lesson22/README.md`。
