COMPOSE_DEV := docker compose -f deploy/compose.dev.yml
GO_RUN := docker run --rm -v $(CURDIR):/src -w /src -v helmsman-gomod:/go/pkg/mod golang:1.26-alpine
GO_NET := docker run --rm --network helmsman-dev_default -v $(CURDIR):/src -w /src -v helmsman-gomod:/go/pkg/mod
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

test: ## Run Go tests (with a throwaway Postgres) and web lint/typecheck, all in Docker
	$(COMPOSE_DEV) --profile test up -d --wait postgres-test
	$(GO_NET) -e TEST_DATABASE_URL='postgres://helmsman:test@postgres-test:5432/helmsman?sslmode=disable' \
		golang:1.26-alpine sh -c 'go vet ./... && go test -count=1 ./...'; \
		rc=$$?; $(COMPOSE_DEV) --profile test rm -sf postgres-test >/dev/null 2>&1; test $$rc -eq 0
	$(NODE_RUN) sh -c 'npm ci && npm run lint && npm run typecheck'

e2e: ## Playwright end to end tests (arrive in M1+)
	@echo "e2e: no tests yet (first added in M1)"

seed-dev: ## Seed the dev agent, users and role bindings
	$(COMPOSE_DEV) exec -T postgres psql -U helmsman -d helmsman -v ON_ERROR_STOP=1 < deploy/seed-dev.sql
	@echo "Seeded. Sign in as alice (admin), bob (developer), carol (viewer), erin (approver). Reload the catalog from Admin > Catalog."
