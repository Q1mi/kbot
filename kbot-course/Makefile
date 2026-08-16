.PHONY: build test test-race vet verify run mockllm-build mockllm-run sandbox-runner-build sandbox-runner-run migrate-up migrate-down admin-build crossborder-build crossborder-test insurance-test deploy-config

build:
	@mkdir -p bin
	go build -o bin/server ./cmd/server

test:
	go test ./...

test-race:
	go test -race -count=1 ./...

vet:
	go vet ./...

verify: vet test-race crossborder-test insurance-test admin-build deploy-config

run:
	go run ./cmd/server

mockllm-build:
	@mkdir -p bin
	go build -o bin/mockllm ./cmd/mockllm

mockllm-run:
	go run ./cmd/mockllm

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
	cd web/admin && npm ci && npm run build

crossborder-build:
	$(MAKE) -C projects/crossborder build

crossborder-test:
	$(MAKE) -C projects/crossborder test

insurance-test:
	$(MAKE) -C projects/insurance test

deploy-config:
	docker compose --env-file .env.example -f deploy/compose.yaml config
