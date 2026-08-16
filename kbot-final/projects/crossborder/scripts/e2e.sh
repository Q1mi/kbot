#!/usr/bin/env bash
set -euo pipefail

base_url=${KBOT_URL:-http://localhost:8080}
email=${KBOT_EMAIL:-admin@example.com}
password=${KBOT_PASSWORD:-admin12345}
workspace_name=${CROSSBORDER_WORKSPACE:-跨境电商运营平台}
agent_name=${CROSSBORDER_AGENT_NAME:-跨境电商运营与供应链协同 Agent}

token=$(curl -fsS -X POST "$base_url/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "$(jq -n --arg email "$email" --arg password "$password" '{email:$email,password:$password}')" | jq -r '.token')
base_auth=(-H "Authorization: Bearer $token" -H 'Content-Type: application/json')
workspace_id=$(curl -fsS "$base_url/api/v1/workspaces" "${base_auth[@]}" | jq -r --arg name "$workspace_name" '.[] | select(.name==$name) | .id' | head -1)
if [ -z "$workspace_id" ]; then
  echo "workspace not found; run install.sh first" >&2
  exit 1
fi
auth=("${base_auth[@]}" -H "X-Workspace-ID: $workspace_id")
agent_id=$(curl -fsS "$base_url/api/v1/agents" "${auth[@]}" | jq -r --arg name "$agent_name" '.[] | select(.name==$name) | .id' | head -1)
if [ -z "$agent_id" ]; then
  echo "agent not found; run install.sh first" >&2
  exit 1
fi

stream_file=$(mktemp)
trap 'rm -f "$stream_file"' EXIT
curl -fsS -N -X POST "$base_url/stream/agents/$agent_id/chat" "${auth[@]}" \
  -d '{"message":"诊断并执行调拨订单 TTS-20260801-1001"}' >"$stream_file"

conversation_id=$(sed -n 's/^data: //p' "$stream_file" | jq -r 'select(.type=="started") | .data.conversation_id' | head -1)
approval_id=$(sed -n 's/^data: //p' "$stream_file" | jq -r 'select(.type=="await_approval") | .text' | head -1)
if [ -z "$conversation_id" ] || [ -z "$approval_id" ]; then
  echo "stream did not produce approval checkpoint" >&2
  cat "$stream_file" >&2
  exit 1
fi
if ! sed -n 's/^data: //p' "$stream_file" | jq -e 'select(.type=="a2ui") | select((.data|tostring)|contains("库存调拨审批"))' >/dev/null; then
  echo "approval stream did not include crossborder A2UI metadata" >&2
  exit 1
fi

curl -fsS -X POST "$base_url/api/v1/approvals/$approval_id/approve" "${auth[@]}" -d '{}' >/dev/null

completed=false
for _ in $(seq 1 15); do
  conversation=$(curl -fsS "$base_url/api/v1/conversations/$conversation_id" "${auth[@]}")
  if printf '%s' "$conversation" | jq -e '.messages[]? | select(.role=="assistant" and (.content|contains("调拨已经创建")))' >/dev/null; then
    completed=true
    break
  fi
  sleep 1
done
if [ "$completed" != "true" ]; then
  echo "approval resume did not complete" >&2
  exit 1
fi

audit=$(curl -fsS "$base_url/api/v1/audit/logs?conversation_id=$conversation_id&limit=100" "${auth[@]}")
for action in await_approval resumed; do
  if ! printf '%s' "$audit" | jq -e --arg action "$action" '.[] | select(.action==$action)' >/dev/null; then
    echo "audit action missing: $action" >&2
    exit 1
  fi
done

dataset_id=$(curl -fsS "$base_url/api/v1/eval/datasets" "${auth[@]}" | jq -r '.[] | select(.name=="crossborder-offline-golden") | .id' | head -1)
if [ -z "$dataset_id" ]; then
  echo "offline eval dataset not found; run install.sh first" >&2
  exit 1
fi
eval_result=$(curl -fsS -X POST "$base_url/api/v1/eval/runs" "${auth[@]}" \
  -d "$(jq -n --arg dataset "$dataset_id" --arg agent "$agent_id" '{dataset_id:$dataset,agent_id:$agent,judge_method:"contains",threshold:1}')")
if ! printf '%s' "$eval_result" | jq -e '.passed == true and .pass_rate == 1' >/dev/null; then
  echo "offline eval did not reach 100% pass rate" >&2
  printf '%s\n' "$eval_result" >&2
  exit 1
fi

jq -n --arg workspace_id "$workspace_id" --arg agent_id "$agent_id" --arg conversation_id "$conversation_id" --arg approval_id "$approval_id" --arg eval_run_id "$(printf '%s' "$eval_result" | jq -r '.run_id')" '{status:"passed",workspace_id:$workspace_id,agent_id:$agent_id,conversation_id:$conversation_id,approval_id:$approval_id,eval_run_id:$eval_run_id,eval_pass_rate:1}'
