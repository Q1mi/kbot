#!/usr/bin/env bash
# 一次性引导:Go 版本预检 + 拉取已在 go.mod 定版的依赖 + 生成 go.sum + 编译验证。
# 用法:bash scripts/bootstrap.sh
# 跑完后请把生成的 go.sum 一并提交——之后 `make up` 即可启动完整课堂环境。
set -euo pipefail
cd "$(dirname "$0")/.."

REQ_GO_MAJOR=1
REQ_GO_MINOR=26
REQ_GO_PATCH=5

if ! command -v go >/dev/null 2>&1; then
  echo "❌ 未检测到 Go 工具链。请先安装 Go ${REQ_GO_MAJOR}.${REQ_GO_MINOR}.${REQ_GO_PATCH}+:https://go.dev/dl/"
  exit 1
fi
GO_VER=$(go env GOVERSION | sed -E 's/^go//; s/[^0-9.].*$//')
IFS=. read -r GO_MAJOR GO_MINOR GO_PATCH <<< "$GO_VER"
GO_PATCH=${GO_PATCH:-0}
if [ "$GO_MAJOR" -lt "$REQ_GO_MAJOR" ] || \
  { [ "$GO_MAJOR" -eq "$REQ_GO_MAJOR" ] && [ "$GO_MINOR" -lt "$REQ_GO_MINOR" ]; } || \
  { [ "$GO_MAJOR" -eq "$REQ_GO_MAJOR" ] && [ "$GO_MINOR" -eq "$REQ_GO_MINOR" ] && [ "$GO_PATCH" -lt "$REQ_GO_PATCH" ]; }; then
  echo "❌ Go 版本过低:当前 $GO_VER,需要 ≥ $REQ_GO_MAJOR.$REQ_GO_MINOR.$REQ_GO_PATCH。"
  exit 1
fi

echo "==> Go 版本 OK: $GO_VER"
echo "==> 拉依赖 (go mod download)"
go mod download
echo "==> 整理 + 生成 go.sum (go mod tidy)"
go mod tidy
echo "==> 编译验证 (go build ./...)"
go build ./...
echo "==> 静态检查 (go vet ./...)"
go vet ./...
echo
echo "✅ bootstrap 完成。"
echo "   完整课堂环境:make up → make demo"
echo "   轻量开发环境:make up-lite → make seed-lite"
echo "   LLM API Key 可选；完整课堂环境自带 Mock LLM。"
