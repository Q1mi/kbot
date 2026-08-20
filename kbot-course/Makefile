.PHONY: build test run admin-build crossborder-build crossborder-test

build:
	@mkdir -p bin
	go build -o bin/server ./cmd/server

test:
	go test ./...

run:
	go run ./cmd/server

admin-build:
	cd web/admin && npm run build

crossborder-build:
	$(MAKE) -C projects/crossborder build

crossborder-test:
	$(MAKE) -C projects/crossborder test
