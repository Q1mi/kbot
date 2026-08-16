#!/usr/bin/env bash
set -euo pipefail

project_dir=$(cd "$(dirname "$0")/.." && pwd)
base_url=${KBOT_URL:-http://localhost:8080}
email=${KBOT_EMAIL:-admin@example.com}
password=${KBOT_PASSWORD:-admin12345}
workspace_name=${INSURANCE_WORKSPACE:-保险理赔与反欺诈平台}
kb_name=${INSURANCE_KB_NAME:-车险条款与反欺诈规则库}
agent_name=${INSURANCE_AGENT_NAME:-保险承保、理赔与反欺诈 Agent}
kb_root=${INSURANCE_KB_ROOT:-/scenarios/insurance/knowledge}
tool_base_url=${INSURANCE_TOOL_BASE_URL:-http://insurance-sim:8092}

for command_name in curl jq; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "missing required command: $command_name" >&2
    exit 1
  fi
done

echo "==> 登录 kbot"
token=$(curl -fsS -X POST "$base_url/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "$(jq -n --arg email "$email" --arg password "$password" '{email:$email,password:$password}')" | jq -r '.token')
base_auth=(-H "Authorization: Bearer $token" -H 'Content-Type: application/json')

echo "==> 获取或创建独立 Workspace"
workspaces=$(curl -fsS "$base_url/api/v1/workspaces" "${base_auth[@]}")
workspace_id=$(printf '%s' "$workspaces" | jq -r --arg name "$workspace_name" '.[] | select(.name==$name) | .id' | head -1)
if [ -z "$workspace_id" ]; then
  workspace_id=$(curl -fsS -X POST "$base_url/api/v1/workspaces" "${base_auth[@]}" \
    -d "$(jq -n --arg name "$workspace_name" '{name:$name,description:"保险承保、理赔与反欺诈 Agent 课程项目"}')" | jq -r '.id')
fi
auth=("${base_auth[@]}" -H "X-Workspace-ID: $workspace_id")

echo "==> 获取或创建知识库"
kbs=$(curl -fsS "$base_url/api/v1/kbs" "${auth[@]}")
kb_id=$(printf '%s' "$kbs" | jq -r --arg name "$kb_name" '.[] | select(.name==$name) | .id' | head -1)
if [ -z "$kb_id" ]; then
  kb_id=$(curl -fsS -X POST "$base_url/api/v1/kbs" "${auth[@]}" \
    -d "$(jq -n --arg name "$kb_name" '{name:$name,embedding_model:"local"}')" | jq -r '.id')
  curl -fsS -X POST "$base_url/api/v1/kbs/$kb_id/connectors/markdown/sync" "${auth[@]}" \
    -d "$(jq -n --arg root "$kb_root" '{root_path:$root}')" >/dev/null
fi

test_input() {
  case "$1" in
    get_policy) jq -nc '{policy_id:"POL-CAR-2026-0001"}' ;;
    get_underwriting_case|assess_underwriting) jq -nc '{case_id:"UW-2026-0001"}' ;;
    approve_underwriting) jq -nc '{case_id:"UW-2026-0001",decision:"approve",premium:4160,reason_codes:["HIGH_PRIOR_CLAIM_FREQUENCY","OLDER_VEHICLE"],idempotency_key:"tool-test-underwriting",dry_run:true}' ;;
    get_claim|evaluate_coverage) jq -nc '{claim_id:"CLM-2026-0001"}' ;;
    get_fraud_features) jq -nc '{claim_id:"CLM-2026-0002"}' ;;
    request_additional_documents) jq -nc '{claim_id:"CLM-2026-0001",documents:["repair_invoice"],idempotency_key:"tool-test-documents",dry_run:true}' ;;
    hold_claim_payment) jq -nc '{claim_id:"CLM-2026-0002",reason:"tool preflight",idempotency_key:"tool-test-hold",dry_run:true}' ;;
    approve_claim) jq -nc '{claim_id:"CLM-2026-0001",approved_amount:6300,reason_codes:["COVERED_COLLISION","DEDUCTIBLE_APPLIED"],idempotency_key:"tool-test-approve",dry_run:true}' ;;
    open_fraud_investigation) jq -nc '{claim_id:"CLM-2026-0002",reason:"tool preflight",idempotency_key:"tool-test-investigation",dry_run:true}' ;;
    *) jq -nc '{}' ;;
  esac
}

echo "==> 注册、试调并发布 Tool"
existing_tools=$(curl -fsS "$base_url/api/v1/tools" "${auth[@]}")
tool_ids=()
while IFS= read -r definition; do
  name=$(printf '%s' "$definition" | jq -r '.name')
  tool_id=$(printf '%s' "$existing_tools" | jq -r --arg name "$name" '.[] | select(.name==$name) | .id' | head -1)
  if [ -z "$tool_id" ]; then
    payload=$(printf '%s' "$definition" | jq --arg base "$tool_base_url" '.endpoint_config.url |= sub("^http://insurance-sim:8092";$base) | {name,source_type,description,sensitive,schema_json:(.schema_json|tojson),endpoint_config:(.endpoint_config|tojson),auth_config:"{}"}')
    tool_id=$(curl -fsS -X POST "$base_url/api/v1/tools" "${auth[@]}" -H "Idempotency-Key: install-insurance-$name" -d "$payload" | jq -r '.id')
    input=$(test_input "$name")
    test_result=$(curl -fsS -X POST "$base_url/api/v1/tools/$tool_id/test" "${auth[@]}" -d "$(jq -n --argjson input "$input" '{input:$input}')")
    if [ "$(printf '%s' "$test_result" | jq -r '.status')" != "success" ]; then
      printf 'tool %s test failed: %s\n' "$name" "$(printf '%s' "$test_result" | jq -r '.error')" >&2
      exit 1
    fi
    curl -fsS -X POST "$base_url/api/v1/tools/$tool_id/publish" "${auth[@]}" -d '{}' >/dev/null
  fi
  tool_ids+=("$tool_id")
done < <(jq -c '.[]' "$project_dir/config/tools.json")
tool_ids_json=$(printf '%s\n' "${tool_ids[@]}" | jq -R . | jq -s .)

echo "==> 创建并发布 Skill"
existing_skills=$(curl -fsS "$base_url/api/v1/skills" "${auth[@]}")
skill_version_ids=()
while IFS= read -r skill_file; do
  skill_name=$(awk '/^name:/{print $2; exit}' "$skill_file")
  skill_id=$(printf '%s' "$existing_skills" | jq -r --arg name "$skill_name" '.[] | select(.name==$name) | .id' | head -1)
  if [ -z "$skill_id" ]; then
    skill_md=$(sed "s/__KB_ID__/$kb_id/g" "$skill_file")
    created=$(curl -fsS -X POST "$base_url/api/v1/skills" "${auth[@]}" \
      -d "$(jq -n --arg category insurance --arg skill_md "$skill_md" '{category:$category,skill_md:$skill_md}')")
    skill_id=$(printf '%s' "$created" | jq -r '.skill.id')
    version_id=$(printf '%s' "$created" | jq -r '.version.id')
    curl -fsS -X POST "$base_url/api/v1/skills/$skill_id/publish" "${auth[@]}" -d "$(jq -n --arg id "$version_id" '{version_id:$id}')" >/dev/null
  else
    version_id=$(curl -fsS "$base_url/api/v1/skills/$skill_id/versions" "${auth[@]}" | jq -r '[.[] | select(.status=="published")][0].id')
  fi
  skill_version_ids+=("$version_id")
done < <(find "$project_dir/skills" -name SKILL.md -type f | sort)
skill_ids_json=$(printf '%s\n' "${skill_version_ids[@]}" | jq -R . | jq -s .)

echo "==> 创建 Agent"
agents=$(curl -fsS "$base_url/api/v1/agents" "${auth[@]}")
agent_id=$(printf '%s' "$agents" | jq -r --arg name "$agent_name" '.[] | select(.name==$name) | .id' | head -1)
if [ -z "$agent_id" ]; then
  agent_payload=$(jq -n --arg name "$agent_name" --arg kb "$kb_id" --argjson tools "$tool_ids_json" --argjson skills "$skill_ids_json" '{name:$name,template:"insurance_claims",system_prompt:"你是保险承保、理赔与反欺诈 Agent。保单、责任、金额、风险分和状态必须来自业务工具。金额与状态变更由确定性规则校验，所有敏感写操作必须等待人工审批。",tool_ids:$tools,skill_version_ids:$skills,kb_ids:[$kb],allow_network:true,max_steps:12}')
  agent_id=$(curl -fsS -X POST "$base_url/api/v1/agents" "${auth[@]}" -d "$agent_payload" | jq -r '.id')
fi

echo "==> 创建离线 Eval 数据集"
datasets=$(curl -fsS "$base_url/api/v1/eval/datasets" "${auth[@]}")
dataset_id=$(printf '%s' "$datasets" | jq -r '.[] | select(.name=="insurance-offline-golden") | .id' | head -1)
if [ -z "$dataset_id" ]; then
  dataset_id=$(curl -fsS -X POST "$base_url/api/v1/eval/datasets" "${auth[@]}" -d '{"name":"insurance-offline-golden","target_kind":"agent"}' | jq -r '.id')
  while IFS= read -r eval_case; do
    if [ "$(printf '%s' "$eval_case" | jq -r '.requires_approval // false')" = "true" ]; then
      continue
    fi
    input=$(printf '%s' "$eval_case" | jq -r '.input')
    expected=$(printf '%s' "$eval_case" | jq -r '.expected')
    curl -fsS -X POST "$base_url/api/v1/eval/datasets/$dataset_id/cases" "${auth[@]}" -d "$(jq -n --arg input "$input" --arg expected "$expected" '{input:$input,expected:$expected}')" >/dev/null
  done < "$project_dir/evals/cases.jsonl"
fi

jq -n --arg workspace_id "$workspace_id" --arg kb_id "$kb_id" --arg agent_id "$agent_id" --arg dataset_id "$dataset_id" '{workspace_id:$workspace_id,kb_id:$kb_id,agent_id:$agent_id,dataset_id:$dataset_id}'
