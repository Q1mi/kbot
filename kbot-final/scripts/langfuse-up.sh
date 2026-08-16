#!/usr/bin/env bash
# 一键启动 kbot + 两个业务模拟器 + Langfuse + 可复现 Mock LLM。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -p kbot-course -f "${REPO_ROOT}/deploy/docker-compose.yml" -f "${REPO_ROOT}/deploy/docker-compose.langfuse.yml")

"${REPO_ROOT}/scripts/langfuse-preflight.sh"

echo "==> 构建并启动课堂环境（首次会拉取 Langfuse/ClickHouse 等镜像）"
"${COMPOSE[@]}" up --build -d

wait_http() {
  local name="$1"
  local url="$2"
  local attempts="${3:-120}"
  local i
  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS --max-time 2 "${url}" >/dev/null 2>&1; then
      echo "✅ ${name} 已就绪：${url}"
      return 0
    fi
    if (( i % 10 == 0 )); then
      echo "    等待 ${name}（${i}/${attempts}）"
    fi
    sleep 2
  done
  echo "❌ ${name} 在限定时间内未就绪，最近日志：" >&2
  "${COMPOSE[@]}" logs --tail=80 >&2
  return 1
}

KBOT_URL="http://localhost:${KBOT_HTTP_PORT:-8080}"
LANGFUSE_URL="http://localhost:${LANGFUSE_PORT:-3000}"
MOCK_LLM_URL="http://localhost:${KBOT_MOCK_LLM_PORT:-8090}"
CROSSBORDER_URL="http://localhost:${CROSSBORDER_PORT:-8091}"
INSURANCE_URL="http://localhost:${INSURANCE_PORT:-8092}"

wait_http "Mock LLM" "${MOCK_LLM_URL}/healthz" 60
wait_http "跨境电商业务模拟器" "${CROSSBORDER_URL}/healthz" 60
wait_http "保险业务模拟器" "${INSURANCE_URL}/healthz" 60
wait_http "Langfuse" "${LANGFUSE_URL}/api/public/health" 180
wait_http "kbot API" "${KBOT_URL}/readyz" 120

cat <<EOF

课堂环境已启动：
  kbot Admin: ${KBOT_URL}
  Langfuse:   ${LANGFUSE_URL}
  跨境电商:   ${CROSSBORDER_URL}
  保险业务:   ${INSURANCE_URL}
  kbot 账号:  teacher@kbot.local / admin12345
  Langfuse:   admin@kbot.local / admin12345

下一步执行：make demo
EOF
