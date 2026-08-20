.PHONY: build test run crossborder-build crossborder-test

build:
	@mkdir -p bin
	go build -o bin/server ./cmd/server

test:
	go test ./...

run:
	go run ./cmd/server

crossborder-build:
	$(MAKE) -C projects/crossborder build

crossborder-test:
	$(MAKE) -C projects/crossborder test
