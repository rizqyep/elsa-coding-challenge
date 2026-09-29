SHELL := /bin/bash
.DEFAULT_GOAL := help

COMPOSE := docker compose
GO      ?= go

-include .env
export

##@ General
help: ## List targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2} /^##@/ {printf "\n%s\n", substr($$0, 5)}' $(MAKEFILE_LIST)

##@ Local stack
.env:
	cp .env.example .env

up: .env ## Start the stack, wait until healthy, apply migrations (+ seed when APP_ENV=local)
	$(COMPOSE) up -d --wait
	$(MAKE) migrate

migrate: .env ## Apply database migrations (idempotent)
	$(COMPOSE) run --rm --build migrate

down: ## Stop the stack (data is kept)
	$(COMPOSE) down

reset: ## Stop the stack and delete its volumes
	$(COMPOSE) down -v

ps: ## Show service status
	$(COMPOSE) ps

logs: ## Follow logs, e.g. make logs S=redis
	$(COMPOSE) logs -f $(S)

redis-cli: ## Open redis-cli in the Redis container
	$(COMPOSE) exec redis redis-cli

psql: ## Open psql in the PostgreSQL container
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

##@ Build and test
check: lint test-unit check-generated ## Lint, unit tests, and generated-code freshness (must be green)

lint: lint-server lint-client lint-contracts ## Lint server, client, and the API contracts

lint-server: require-go
	cd server && $(GO) vet ./... && $(GO) tool -modfile=tools/go.mod golangci-lint run ./...

lint-client: client/node_modules
	cd client && npm run lint && npm run typecheck

test-unit: test-unit-server test-unit-client ## Unit tests for server and client

test-unit-server: require-go
	cd server && $(GO) test -race ./...

test-unit-client: client/node_modules
	cd client && npm test

test-integration: require-go ## Integration tests against real Redis, PostgreSQL, and Toxiproxy (needs Docker)
	cd server && $(GO) test -race -tags integration -count=1 -timeout 10m ./...

##@ Contracts and code generation (D13)
GENERATED := server/internal/httpapi/gen/api.gen.go server/internal/protocol/schemas client/src/api/schema.ts client/src/protocol/messages.ts

generate: require-go tools/contracts/node_modules ## Regenerate code from docs/api and module interfaces (output is committed)
	cd server/internal/httpapi && $(GO) tool -modfile=../../tools/go.mod oapi-codegen -config oapi-codegen.yaml ../../../docs/api/openapi.yaml
	rm -rf server/internal/protocol/schemas && cp -r docs/api/schemas server/internal/protocol/schemas
	cd server && $(GO) generate ./...
	cd tools/contracts && npm run --silent gen:api -- ../../client/src/api/schema.ts && npm run --silent gen:protocol

check-generated: ## Fail if committed generated code is out of date with docs/api
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
	for f in $(GENERATED); do mkdir -p "$$tmp/$$(dirname $$f)"; cp -r "$$f" "$$tmp/$$f"; done && \
	$(MAKE) --no-print-directory generate >/dev/null && \
	stale=0; for f in $(GENERATED); do diff -rq "$$f" "$$tmp/$$f" >/dev/null || { echo "stale: $$f"; stale=1; }; done; \
	if [ $$stale -ne 0 ]; then echo "Generated code was out of date and has been regenerated; review and commit it."; exit 1; fi

lint-contracts: tools/contracts/node_modules ## Validate openapi.yaml and asyncapi.yaml
	cd tools/contracts && npm run --silent lint:openapi && npm run --silent lint:asyncapi

tools/contracts/node_modules: tools/contracts/package-lock.json
	cd tools/contracts && npm ci
	@touch tools/contracts/node_modules

client/node_modules: client/package-lock.json
	cd client && npm ci
	@touch client/node_modules

require-go:
	@command -v $(GO) >/dev/null || { echo "Go is not installed. On Fedora: sudo dnf install golang"; exit 1; }

.PHONY: help up migrate down reset ps logs redis-cli psql check lint lint-server lint-client lint-contracts test-unit test-unit-server test-unit-client test-integration generate check-generated require-go
