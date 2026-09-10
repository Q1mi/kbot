.PHONY: build test run sandbox-runner-build sandbox-runner-run migrate-up migrate-down admin-build crossborder-build crossborder-test doubao-check logistics-mock-run a2a-demo feishu-demo lesson21-test lesson22-test

logistics-mock-run:
	go run ./cmd/logistics-mock $(ARGS)

a2a-demo:
	go run ./cmd/lesson21-demo $(ARGS)

feishu-demo:
	go run ./cmd/lesson22-demo $(ARGS)

lesson21-test:
	go test ./internal/demo/... ./internal/runtime/tooling ./cmd/lesson21-demo ./cmd/logistics-mock

lesson22-test:
	go test ./internal/integration/... ./internal/config ./cmd/lesson22-demo ./cmd/server

build:
	@mkdir -p bin
	go build -o bin/server ./cmd/server

test:
	go test ./...

run:
	go run ./cmd/server

doubao-check:
	@test -n "$$ARK_API_KEY" || (echo "ARK_API_KEY is required" && exit 1)
	go test -tags=integration -run TestDoubaoGenerate -v ./internal/runtime/llm

sandbox-runner-build:
	@mkdir -p bin
	go build -o bin/sandbox-runner ./cmd/sandbox-runner

sandbox-runner-run:
	go run ./cmd/sandbox-runner

migrate-up:
	migrate -path migrations -database "$(KBOT_DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(KBOT_DATABASE_URL)" down 1

admin-build:
	cd web/admin && npm run build

crossborder-build:
	$(MAKE) -C projects/crossborder build

crossborder-test:
	$(MAKE) -C projects/crossborder test
