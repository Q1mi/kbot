#!/usr/bin/env bash
# 拉取数据分级路由使用的本地 Ollama 模型。
# 本脚本按需启动 local-llm profile 并拉取或更新模型。
set -euo pipefail
cd "$(dirname "$0")/.."

MODEL="${KBOT_OLLAMA_MODEL:-qwen2.5:7b}"
echo "==> 拉取 Ollama 模型:$MODEL(首次约 5GB)"
docker compose -f deploy/docker-compose.yml --profile local-llm up -d ollama
docker compose -f deploy/docker-compose.yml --profile local-llm exec -T ollama ollama pull "$MODEL"
echo "✅ 完成。classification=secret 的请求将路由到本地 $MODEL(OTel gen_ai.system=ollama)。"

# 国内加速:若 registry.ollama.ai 慢,可设镜像(写进 .env 的 ollama 服务环境或主机):
#   export OLLAMA_HOST=...           # 自建/镜像源
# 或先用代理 docker pull 模型,再 ollama create 导入。详见 docs/runbook.md。
