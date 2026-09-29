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

.PHONY: help up migrate down reset ps logs redis-cli psql check lint lint-server lint-client test-unit test-unit-server test-unit-client generate require-go
