#!/usr/bin/env bash
# 幂等初始化课堂环境统一使用的两个业务 Workspace。
set -euo pipefail

AUTO_OPEN=0
[ "${1:-}" = "--auto-open" ] && AUTO_OPEN=1

BASE_URL="${KBOT_URL:-http://localhost:8080}"
EMAIL="${SEED_EMAIL:-admin@example.com}"
PASSWORD="${SEED_PASSWORD:-admin12345}"

for command_name in curl jq; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "✗ 缺少必要命令: $command_name" >&2
    exit 1
  fi
done

echo "==> 登录拿 token"
TOKEN=$(curl -fsS -X POST "${BASE_URL}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg email "$EMAIL" --arg password "$PASSWORD" '{email:$email,password:$password}')" | jq -r '.token')

if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
  echo "✗ 登录失败，未取得 token" >&2
  exit 1
fi

base_auth=(-H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json')
WORKSPACES=$(curl -fsS "${BASE_URL}/api/v1/workspaces?limit=200" "${base_auth[@]}")

ensure_workspace() {
  local name="$1"
  local description="$2"
  local workspace_id
  workspace_id=$(printf '%s' "$WORKSPACES" | jq -r --arg name "$name" '.[] | select(.name==$name) | .id' | head -1)
  if [ -z "$workspace_id" ]; then
    workspace_id=$(curl -fsS -X POST "${BASE_URL}/api/v1/workspaces" "${base_auth[@]}" \
      -d "$(jq -n --arg name "$name" --arg description "$description" '{name:$name,description:$description}')" | jq -r '.id')
  fi
  if [ -z "$workspace_id" ] || [ "$workspace_id" = "null" ]; then
    echo "✗ 未能初始化 Workspace: $name" >&2
    exit 1
  fi
  printf '    %s: %s\n' "$name" "$workspace_id"
}

echo "==> 初始化统一业务 Workspace"
ensure_workspace "跨境电商运营平台" "跨境电商运营与供应链协同 Agent 课程项目"
ensure_workspace "保险理赔与反欺诈平台" "保险承保、理赔与反欺诈 Agent 课程项目"

echo "==> Workspace 初始化完成。浏览器打开 ${BASE_URL} 查看或配置独立的 Provider Account、Deployment 和 Profile。"

if [ "$AUTO_OPEN" = "1" ]; then
  if command -v open >/dev/null 2>&1; then open "$BASE_URL";
  elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$BASE_URL";
  fi
fi
echo "👤 admin: ${EMAIL} / ${PASSWORD}"
