.PHONY: build test run admin-build crossborder-build crossborder-test doubao-check

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

admin-build:
	cd web/admin && npm run build

crossborder-build:
	$(MAKE) -C projects/crossborder build

crossborder-test:
	$(MAKE) -C projects/crossborder test
