SHELL := /bin/bash
.DEFAULT_GOAL := help

COMPOSE := docker compose
GO      ?= go

-include .env
export

##@ General
help: ## List targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2} /^##@/ {printf "\n%s\n", substr($$0, 5)}' $(MAKEFILE_LIST)

##@ Local stack (TRD §11)
# Profiles: make up PROFILES="observability replica chaos". Replica turns on WAIT; chaos routes Redis and PostgreSQL through Toxiproxy.
PROFILES ?=
COMPOSE_UP := $(COMPOSE) $(foreach p,$(PROFILES),--profile $(p))
ifneq ($(filter replica,$(PROFILES)),)
export REDIS_WAIT_REPLICAS := 1
endif
ifneq ($(filter chaos,$(PROFILES)),)
export REDIS_ADDR := toxiproxy:26379
export POSTGRES_DSN := $(subst @postgres:5432/,@toxiproxy:25432/,$(POSTGRES_DSN))
endif
WS     ?= 2
WORKER ?= 2
URL    := http://localhost:$(or $(HTTP_HOST_PORT),8080)
TOXI   := http://127.0.0.1:8474

.env:
	cp .env.example .env

up: .env ## Build, migrate and seed, start everything, wait until healthy (PROFILES=... optional)
	$(COMPOSE_UP) up -d --build --wait
	@echo "Stack is up: $(URL)"

migrate: .env ## Apply database migrations (idempotent; make up runs them too)
	$(COMPOSE) run --rm --build migrate

down: ## Stop the stack, profiles included (data is kept)
	$(COMPOSE) --profile '*' down

reset: ## Stop the stack and delete its volumes
	$(COMPOSE) --profile '*' down -v

scale: ## Change replica counts without restarting the rest, e.g. make scale WS=4 WORKER=3
	$(COMPOSE_UP) up -d --no-recreate --wait --scale ws=$(WS) --scale worker=$(WORKER)

demo: up ## Start the stack and print how to run the demo quiz
	@echo
	@echo "  Host:    open $(URL), choose \"Host a quiz\", pick \"Quick demo\", create, then Start."
	@echo "  Players: open the join link the host screen shows, one browser tab (or window) per player."
	@echo

ps: ## Show service status
	$(COMPOSE) --profile '*' ps

logs: ## Follow logs, e.g. make logs S=ws
	$(COMPOSE) logs -f $(S)

redis-cli: ## Open redis-cli in the Redis container
	$(COMPOSE) exec redis redis-cli

psql: ## Open psql in the PostgreSQL container
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

##@ Failure switches (TRD §11.5)
chaos-kill: ## Kill one container, e.g. make chaos-kill S=ws-1 (stays down until chaos-start)
	docker kill quiz-$(S)

chaos-stop: ## Stop every instance of a service, e.g. make chaos-stop S=worker
	$(COMPOSE) stop $(S)

chaos-start: ## Bring stopped or killed services back
	$(COMPOSE_UP) up -d --no-recreate --wait --scale ws=$(WS) --scale worker=$(WORKER)

chaos-redis-latency: ## Add latency to every Redis call, e.g. MS=50 (needs PROFILES=chaos)
	curl -fsS -X POST $(TOXI)/proxies/redis/toxics -d '{"name":"latency","type":"latency","attributes":{"latency":$(or $(MS),50)}}' >/dev/null
	@echo "Redis latency +$(or $(MS),50) ms; make chaos-reset removes it"

chaos-redis-down: ## Cut services off from Redis for SEC seconds, e.g. SEC=5 (needs PROFILES=chaos)
	curl -fsS -X POST $(TOXI)/proxies/redis -d '{"enabled":false}' >/dev/null
	@echo "Redis cut off for $(or $(SEC),5) s"; sleep $(or $(SEC),5)
	curl -fsS -X POST $(TOXI)/proxies/redis -d '{"enabled":true}' >/dev/null
	@echo "Redis restored"

chaos-pg-down: ## Pause PostgreSQL for SEC seconds, e.g. SEC=30
	$(COMPOSE) pause postgres
	@echo "PostgreSQL paused for $(or $(SEC),30) s"; sleep $(or $(SEC),30)
	$(COMPOSE) unpause postgres

chaos-reset: ## Remove injected faults and bring every service back
	-@curl -fsS $(TOXI)/reset -X POST >/dev/null 2>&1 && echo "Toxiproxy reset"
	-@$(COMPOSE) unpause postgres >/dev/null 2>&1
	$(MAKE) --no-print-directory chaos-start

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
	cd server && $(GO) test -race -tags integration -count=1 -timeout 5m ./...

test-e2e: require-go ## End-to-end and fault tests against the running stack (make up PROFILES=chaos first)
	cd server && $(GO) test -tags e2e -count=1 -timeout 20m -v ./e2e/

##@ Contracts and code generation (D13)
GENERATED := server/internal/httpapi/gen/api.gen.go server/internal/protocol/schemas $(wildcard server/internal/*/mocks) client/src/api/schema.ts client/src/protocol/messages.ts

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

.PHONY: help up migrate down reset scale demo ps logs redis-cli psql chaos-kill chaos-stop chaos-start chaos-redis-latency chaos-redis-down chaos-pg-down chaos-reset check lint lint-server lint-client lint-contracts test-unit test-unit-server test-unit-client test-integration test-e2e generate check-generated require-go
