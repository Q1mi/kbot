# 每课验收命令

以下命令均在对应标签的 `kbot-course/` 目录执行。每课先运行目标包，阶段结束时再运行全量 race test。

| 课时 | 核心验收命令 |
|---|---|
| 01 | `go test ./...` |
| 02 | `make -C projects/crossborder test` |
| 03 | `make -C projects/crossborder test` |
| 04 | `go test ./internal/config ./internal/platform/iam ./internal/api/...` |
| 05 | `go test ./internal/runtime/llm ./cmd/mockllm` |
| 06 | `go test ./internal/runtime/engine ./internal/api` |
| 07 | `go test ./internal/platform/tool ./internal/api` |
| 08 | `go test ./internal/runtime/tooling ./internal/runtime/sandbox` |
| 09 | `go test ./internal/runtime/engine` |
| 10 | `go test ./internal/connector/... ./internal/platform/kb` |
| 11 | `go test ./internal/runtime/retriever ./internal/runtime/tooling` |
| 12 | `go test ./internal/platform/prompt ./internal/platform/modelconfig ./internal/runtime/...` |
| 13 | `go test ./internal/platform/skill ./internal/runtime/skillrunner ./internal/runtime/engine` |
| 14 | `go test ./internal/platform/agent ./internal/runtime/engine ./internal/api` |
| 15 | `go test ./cmd/migrate ./internal/infrastructure/postgres ./internal/platform/agent ./internal/platform/iam` |
| 16 | `go test ./internal/platform/approval ./internal/runtime/engine` |
| 17 | `go test ./internal/a2ui ./internal/platform/approval ./internal/runtime/engine ./internal/api` |
| 18 | `go test ./internal/runtime/guard ./internal/api` |
| 19 | `go test ./internal/infrastructure/otel ./internal/platform/audit ./internal/runtime/engine` |
| 20 | `go test ./internal/platform/eval ./internal/platform/agent ./internal/api` |
| 21 | `go test ./internal/runtime/team ./internal/platform/team ./internal/runtime/tooling ./internal/integration/...` |
| 22 | `make -C projects/insurance test` |
| 23 | `make verify` |

## 标签操作模板

```bash
git switch --detach 09-start
git switch -c practice/09
cd kbot-course

# 编码后执行本课验收
go test ./internal/runtime/engine

# 对照标准答案
git diff 09-end -- .
```

## 阶段验收

```bash
go vet ./...
go test -race -count=1 ./...
```

涉及独立业务 Module 时继续执行：

```bash
make -C projects/crossborder test
make -C projects/insurance test
```

第 23 课的 `make verify` 会覆盖 Go vet、race test、两个业务 Module、前端构建和 Compose 配置解析。
