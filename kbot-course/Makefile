.PHONY: build test run

build:
	@mkdir -p bin
	go build -o bin/server ./cmd/server

test:
	go test ./...

run:
	go run ./cmd/server
