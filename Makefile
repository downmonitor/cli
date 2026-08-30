BINARY  := downmonitor
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test lint fmt run test-install snapshot clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) ./cmd/downmonitor

install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/downmonitor

test:
	go test -race ./...

lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt:"; gofmt -l .; exit 1)
	go vet ./...

fmt:
	gofmt -w .

run: build
	./dist/$(BINARY) $(ARGS)

test-install:
	shellcheck install.sh scripts/test-install.sh
	./scripts/test-install.sh

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf dist
