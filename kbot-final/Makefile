# kbot Makefile
# 常用：make up / make demo / make test / make test-integration

SHELL := /bin/bash
LITE_COMPOSE := docker compose -p kbot-lite -f deploy/docker-compose.yml --env-file .env
LANGFUSE_COMPOSE := docker compose -p kbot-course -f deploy/docker-compose.yml -f deploy/docker-compose.langfuse.yml
CROSSBORDER_COMPOSE := docker compose -p kbot-crossborder -f deploy/docker-compose.yml -f projects/crossborder/kbot-compose.yml --env-file .env --env-file projects/crossborder/compose.env
INSURANCE_COMPOSE := docker compose -p kbot-insurance -f deploy/docker-compose.yml -f projects/insurance/kbot-compose.yml --env-file .env --env-file projects/insurance/compose.env
CROSSBORDER_KBOT_URL ?= http://localhost:8181
INSURANCE_KBOT_URL ?= http://localhost:8182

.PHONY: help bootstrap up down logs ps demo up-lite down-lite logs-lite ps-lite seed seed-lite migrate migrate-lite sqlc-generate openapi test-integration \
		crossborder-build crossborder-test crossborder-up crossborder-install crossborder-e2e crossborder-logs crossborder-down \
		insurance-build insurance-test insurance-up insurance-install insurance-e2e insurance-logs insurance-down vertical-build vertical-test \
        langfuse-preflight langfuse-up langfuse-demo langfuse-ps langfuse-logs langfuse-down langfuse-reset

help: ## 列出所有 target
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ===== 项目本身 =====

bootstrap: ## 检查 Go 版本 / 拉依赖 / 生成 go.sum / 编译验证
	@bash scripts/bootstrap.sh

up: ## 启动完整课堂环境（kbot + 业务模拟器 + Langfuse + Mock LLM）
	@bash scripts/langfuse-up.sh

down: ## 停止完整课堂环境并保留数据
	$(LANGFUSE_COMPOSE) down

logs: ## 跟随完整课堂环境日志
	$(LANGFUSE_COMPOSE) logs -f --tail=100 app worker sandbox-runner crossborder-sim insurance-sim langfuse-web langfuse-worker

ps: ## 查看完整课堂环境容器状态
	$(LANGFUSE_COMPOSE) ps

demo: ## 生成普通对话、人工审批、Guard 拦截三组课堂 Trace
	@bash scripts/langfuse-demo.sh

up-lite: ## 启动轻量开发环境（不含 Langfuse）
	@if [ -n "$$($(LANGFUSE_COMPOSE) ps -q 2>/dev/null)" ]; then \
		echo "完整课堂环境正在运行，请先执行 make down。"; \
		exit 1; \
	fi
	$(LITE_COMPOSE) up --build -d
	@echo "✅ 轻量环境已启动。Admin Console: http://localhost:8080"

down-lite: ## 停止轻量开发环境并保留数据
	$(LITE_COMPOSE) down

logs-lite: ## 跟随轻量开发环境日志
	$(LITE_COMPOSE) logs -f --tail=100 app worker sandbox-runner

ps-lite: ## 查看轻量开发环境容器状态
	$(LITE_COMPOSE) ps

test: ## go test -race -count=1 ./...
	go test -race -count=1 ./...
	$(MAKE) vertical-test

build: ## go build ./...
	go build ./...
	$(MAKE) vertical-build

crossborder-build: ## 编译独立跨境电商业务模拟服务
	$(MAKE) -C projects/crossborder build

crossborder-test: ## 测试独立跨境电商业务项目
	$(MAKE) -C projects/crossborder test

crossborder-up: ## 启动 kbot + 独立跨境电商模拟服务
	$(CROSSBORDER_COMPOSE) up --build -d

crossborder-install: ## 安装跨境电商 Workspace、KB、Tool、Skill、Agent 与 Eval
	@KBOT_URL=$(CROSSBORDER_KBOT_URL) bash projects/crossborder/scripts/install.sh

crossborder-e2e: ## 验证跨境电商敏感调拨审批、恢复和审计闭环
	@KBOT_URL=$(CROSSBORDER_KBOT_URL) bash projects/crossborder/scripts/e2e.sh

crossborder-logs: ## 跟随跨境电商课堂环境日志
	$(CROSSBORDER_COMPOSE) logs -f --tail=100 app worker sandbox-runner crossborder-sim

crossborder-down: ## 停止跨境电商课堂环境并保留数据
	$(CROSSBORDER_COMPOSE) down

insurance-build: ## 编译独立保险业务模拟服务
	$(MAKE) -C projects/insurance build

insurance-test: ## 测试独立保险业务项目
	$(MAKE) -C projects/insurance test

insurance-up: ## 启动 kbot + 独立保险业务模拟服务
	$(INSURANCE_COMPOSE) up --build -d

insurance-install: ## 安装保险 Workspace、KB、Tool、Skill、Agent 与 Eval
	@KBOT_URL=$(INSURANCE_KBOT_URL) bash projects/insurance/scripts/install.sh

insurance-e2e: ## 验证高风险理赔冻结审批、恢复和审计闭环
	@KBOT_URL=$(INSURANCE_KBOT_URL) bash projects/insurance/scripts/e2e.sh

insurance-logs: ## 跟随保险课堂环境日志
	$(INSURANCE_COMPOSE) logs -f --tail=100 app worker sandbox-runner insurance-sim

insurance-down: ## 停止保险课堂环境并保留数据
	$(INSURANCE_COMPOSE) down

vertical-build: crossborder-build insurance-build ## 编译两个相互隔离的业务项目

vertical-test: crossborder-test insurance-test ## 测试两个相互隔离的业务项目
	@bash scripts/check-vertical-isolation.sh

seed: ## 在完整课堂环境初始化两个业务 Workspace
	@SEED_EMAIL=teacher@kbot.local bash scripts/seed.sh --auto-open

seed-lite: ## 在轻量开发环境初始化两个业务 Workspace
	@bash scripts/seed.sh --auto-open

migrate: ## 在完整课堂环境执行数据库迁移
	$(LANGFUSE_COMPOSE) run --rm --build migrate -up

migrate-lite: ## 在轻量开发环境执行数据库迁移
	$(LITE_COMPOSE) run --rm --build migrate -up

sqlc-generate: ## 生成 sqlc Go 代码(改 SQL 后跑;生成产物已进 git,学员一般无需跑)
	@if command -v sqlc >/dev/null 2>&1; then \
		sqlc generate; \
	elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then \
		docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc:1.27.0 generate; \
	else \
		echo "需要 sqlc v1.27.0 二进制或可用的 Docker daemon"; \
		exit 1; \
	fi

openapi: ## 生成 Swagger 2.0 spec（swaggo/swag）
	@swag init --quiet -g main.go -d cmd/server,internal/api/v1 -o pkg/sdk/openapi \
		--packageName openapi --parseInternal --parseDependencyLevel 1 --parseDepth 2

test-integration: ## dockertest 真 PG/Redis 集成测试(含各 store contract test + e2e;需 Docker)
	@# 复用常驻 PG 可加速:KBOT_TEST_DATABASE_URL=postgres://kbot:kbot@localhost:55432/kbot?sslmode=disable make test-integration
	@# -p 1 串行包:契约测试会 TRUNCATE 共享 testpg,串行避免与 tests/integration e2e 并发互踩。
	go test -race -tags=integration -count=1 -p 1 ./...

# ===== Langfuse 课堂环境 =====

langfuse-preflight: ## 检查 Langfuse 课堂环境依赖、端口和 Compose 配置
	@bash scripts/langfuse-preflight.sh

langfuse-up: up ## 兼容入口：等同于 make up

langfuse-demo: demo ## 兼容入口：等同于 make demo

langfuse-ps: ps ## 兼容入口：等同于 make ps

langfuse-logs: logs ## 兼容入口：等同于 make logs

langfuse-down: down ## 兼容入口：等同于 make down

langfuse-reset: ## 删除 Langfuse 课堂环境及其数据卷
	@echo "即将删除 kbot-course Compose 项目的容器和数据卷。"
	$(LANGFUSE_COMPOSE) down -v --remove-orphans
