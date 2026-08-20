.PHONY: build test run

build:
	@mkdir -p bin
	go build -o bin/insurance-simulator ./cmd/simulator

test:
	go test -race -count=1 ./...

run:
	go run ./cmd/simulator
