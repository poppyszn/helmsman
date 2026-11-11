COMPOSE_DEV := docker compose -f deploy/compose.dev.yml
GO_RUN := docker run --rm -v $(CURDIR):/src -w /src -v helmsman-gomod:/go/pkg/mod golang:1.26-alpine
NODE_RUN := docker run --rm -v $(CURDIR)/web:/w -w /w node:24-alpine

.DEFAULT_GOAL := help
.PHONY: help up down logs build test lint e2e seed-dev web-build

help: ## Show targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

up: ## Start the dev stack (UI on http://localhost:5173)
	$(COMPOSE_DEV) up -d --build --wait --wait-timeout 180
	@echo "UI: http://localhost:5173  API: http://localhost:8080"

down: ## Stop the dev stack (keeps volumes)
	$(COMPOSE_DEV) down

logs: ## Tail dev stack logs
	$(COMPOSE_DEV) logs -f --tail=100

build: ## Build the server and agent images
	docker build -f deploy/docker/server.Dockerfile -t helmsman-server:dev .
	docker build -f deploy/docker/agent.Dockerfile -t helmsman-agent:dev .

web-build: ## Build the UI into web/dist
	$(NODE_RUN) sh -c 'npm ci && npm run lint && npm run build'
	touch web/dist/.gitkeep

test: ## Run Go unit tests and web lint/typecheck, all in Docker
	$(GO_RUN) sh -c 'go vet ./... && go test ./...'
	$(NODE_RUN) sh -c 'npm ci && npm run lint && npm run typecheck'

e2e: ## Playwright end to end tests (arrive in M1+)
	@echo "e2e: no tests yet (first added in M1)"

seed-dev: ## Seed dev users, roles, bindings (arrives in M1)
	@echo "seed-dev: nothing to seed until M1"
