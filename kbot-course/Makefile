.PHONY: build test test-race vet verify run sandbox-runner-build sandbox-runner-run migrate-up migrate-down admin-build crossborder-build crossborder-test insurance-test deploy-config doubao-check

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
	cd web/admin && npm ci && npm run build

crossborder-build:
	$(MAKE) -C projects/crossborder build

crossborder-test:
	$(MAKE) -C projects/crossborder test

insurance-test:
	$(MAKE) -C projects/insurance test

deploy-config:
	docker compose --env-file .env.example -f deploy/compose.yaml config
