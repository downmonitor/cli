BINARY  := downmonitor
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := build

.PHONY: build install test cover lint fmt run test-install snapshot clean help

build: ## Build ./dist/downmonitor
	go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) ./cmd/downmonitor

install: ## Install into $GOPATH/bin
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/downmonitor

test: ## Run the tests with the race detector
	go test -race ./...

cover: ## Run the tests and open the coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format the code
	golangci-lint fmt ./...

run: build ## Build and run, e.g. make run ARGS="regions"
	./dist/$(BINARY) $(ARGS)

test-install: ## Test install.sh against a locally built release
	shellcheck install.sh scripts/test-install.sh
	./scripts/test-install.sh

snapshot: ## Build every release artifact locally, without publishing
	goreleaser release --snapshot --clean

clean:
	rm -rf dist coverage.out

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'
