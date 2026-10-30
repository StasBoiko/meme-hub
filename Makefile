# Go tooling and environment configuration
GO ?= go
GOPATH ?= $(shell $(GO) env GOPATH)
export PATH := $(GOPATH)/bin:$(PATH)

.DEFAULT_GOAL := check

.PHONY: all fmt lint test dep check pre-commit build run seed seed-clean seed-reset help

all: check

## Run go mod tidy to clean up dependencies
dep:
	@echo "==> Tidying Go modules..."
	$(GO) mod tidy

## Format source code with go fmt and goimports
fmt:
	@echo "==> Formatting source code..."
	$(GO) fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		echo "==> Running goimports..."; \
		goimports -w .; \
	else \
		echo "Warning: goimports not found in PATH. Install with: go install golang.org/x/tools/cmd/goimports@latest"; \
	fi

## Run golangci-lint
lint:
	@echo "==> Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "Error: golangci-lint not found in PATH. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

## Run tests with data race detector (-race)
test:
	@echo "==> Running tests with -race..."
	$(GO) test -v -race ./...

## Build server binary in bin/server
build:
	@echo "==> Building server binary..."
	$(GO) build -o bin/server ./cmd/server

## Run server application
run:
	@echo "==> Running server..."
	$(GO) run ./cmd/server

## Populate database and search index with test fixtures
seed:
	@echo "==> Seeding test data..."
	$(GO) run ./cmd/seed

## Wipe all seeded test data from database and search index
seed-clean:
	@echo "==> Cleaning test data..."
	$(GO) run ./cmd/seed -clean

## Reset database and re-seed fresh test fixtures
seed-reset:
	@echo "==> Resetting and re-seeding test data..."
	$(GO) run ./cmd/seed -reset

## Run all verification checks in order (dep -> fmt -> lint -> test)
check: dep fmt lint test

## Alias for check
pre-commit: check

## Display list of available targets
help:
	@echo "Available targets:"
	@awk '/^[a-zA-Z\-_0-9]+:/ { \
		helpMessage = match(lastLine, /^## (.*)/); \
		if (helpMessage) { \
			helpCommand = substr($$1, 1, index($$1, ":")-1); \
			helpMessage = substr(lastLine, RSTART + 3, RLENGTH); \
			printf "  \033[36m%-12s\033[0m %s\n", helpCommand, helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' $(MAKEFILE_LIST)
