SHELL := /bin/bash
.DEFAULT_GOAL := help

GO      ?= go

##@ General
help: ## List targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2} /^##@/ {printf "\n%s\n", substr($$0, 5)}' $(MAKEFILE_LIST)

##@ Build and test
check: lint test-unit ## Lint and unit tests for server and client (must be green)

lint: lint-server lint-client ## Lint server and client

lint-server: require-go
	cd server && $(GO) vet ./... && $(GO) tool -modfile=tools/go.mod golangci-lint run ./...

lint-client: client/node_modules
	cd client && npm run lint && npm run typecheck

test-unit: test-unit-server test-unit-client ## Unit tests for server and client

test-unit-server: require-go
	cd server && $(GO) test -race ./...

test-unit-client: client/node_modules
	cd client && npm test

generate: ## Run all code generators (filled in by task-05)
	@echo "generate: nothing to generate yet (task-05)"

client/node_modules: client/package-lock.json
	cd client && npm ci
	@touch client/node_modules

require-go:
	@command -v $(GO) >/dev/null || { echo "Go is not installed. On Fedora: sudo dnf install golang"; exit 1; }

.PHONY: help check lint lint-server lint-client test-unit test-unit-server test-unit-client generate require-go
