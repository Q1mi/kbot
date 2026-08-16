#!/usr/bin/env bash
# Langfuse 课堂环境启动前检查。只读检查，不修改本机 Docker 状态。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILES=(-f "${REPO_ROOT}/deploy/docker-compose.yml" -f "${REPO_ROOT}/deploy/docker-compose.langfuse.yml")

fail() {
  echo "❌ $*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || fail "未找到 Docker，请先安装 Docker Desktop 或 Docker Engine。"
docker compose version >/dev/null 2>&1 || fail "当前 Docker 未提供 Compose v2。"
docker info >/dev/null 2>&1 || fail "Docker daemon 未运行。"

echo "==> 校验 Compose 配置"
docker compose -p kbot-course "${COMPOSE_FILES[@]}" config --quiet

mem_bytes="$(docker info --format '{{.MemTotal}}' 2>/dev/null || true)"
if [[ "${mem_bytes}" =~ ^[0-9]+$ ]]; then
  mem_gib=$((mem_bytes / 1024 / 1024 / 1024))
  echo "==> Docker 可用内存：约 ${mem_gib} GiB"
  if (( mem_gib < 12 )); then
    echo "⚠️  建议给 Docker 分配至少 12 GiB 内存，课堂投屏推荐 16 GiB。"
  fi
fi

ports=(
  "${LANGFUSE_PORT:-3000}"
  "${LANGFUSE_MINIO_PORT:-9090}"
  "${LANGFUSE_MINIO_CONSOLE_PORT:-9091}"
  "${KBOT_HTTP_PORT:-8080}"
  "${KBOT_MOCK_LLM_PORT:-8090}"
  "${CROSSBORDER_PORT:-8091}"
  "${INSURANCE_PORT:-8092}"
  "${KBOT_POSTGRES_PORT:-5432}"
  "${KBOT_REDIS_PORT:-6379}"
  "${KBOT_MINIO_PORT:-9000}"
  "${KBOT_MINIO_CONSOLE_PORT:-9001}"
)
busy=()
for port in "${ports[@]}"; do
  if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
    # 已由本课程 Compose 占用时允许重复执行 up。
    if ! docker compose -p kbot-course "${COMPOSE_FILES[@]}" ps --format json 2>/dev/null | grep -q "${port}"; then
      busy+=("${port}")
    fi
  fi
done

if ((${#busy[@]} > 0)); then
  fail "以下端口已被其他进程占用：${busy[*]}"
fi

echo "✅ Langfuse 课堂环境预检通过"
