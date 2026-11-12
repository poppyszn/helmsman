COMPOSE_DEV := docker compose -f deploy/compose.dev.yml
GO_RUN := docker run --rm -v $(CURDIR):/src -w /src -v helmsman-gomod:/go/pkg/mod golang:1.26-alpine
GO_NET := docker run --rm --network helmsman-dev_default -v $(CURDIR):/src -w /src -v helmsman-gomod:/go/pkg/mod
NODE_RUN := docker run --rm -v $(CURDIR)/web:/w -w /w node:24-alpine

.DEFAULT_GOAL := help
.PHONY: help up-logs e2e-server backup-test up up-github down logs build test lint docs-check e2e seed-dev web-build

help: ## Show targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

backup-test: ## Backup the dev database, restore it into a scratch database and verify (restore drill)
	COMPOSE_FILE=deploy/compose.dev.yml deploy/backup/verify-restore.sh

up-logs: ## Dev stack plus Loki and Alloy, for log history (see docs/logs.md)
	COMPOSE_PROFILES=logs $(COMPOSE_DEV) up -d --build --wait --wait-timeout 180
	@$(MAKE) --no-print-directory seed-dev
	@echo "UI: http://localhost:5173  Loki: http://localhost:3100  Faro receiver: http://localhost:12347/collect"
	@echo "Log history is not connected yet: sign in as alice, open Integrations, and connect http://loki:3100"

up-github: ## Dev stack with a real GitHub App for runs (needs GITHUB_APP_ID in .env and deploy/secrets/github-app.pem)
	$(COMPOSE_DEV) --env-file .env -f deploy/compose.github-app.yml up -d --build --wait --wait-timeout 180
	@$(MAKE) --no-print-directory seed-dev
	@echo "UI: http://localhost:5173  runs go to real GitHub"

up: ## Start the dev stack and seed dev data (UI on http://localhost:5173)
	$(COMPOSE_DEV) up -d --build --wait --wait-timeout 180
	@$(MAKE) --no-print-directory seed-dev
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
		golang:1.26-alpine sh -c 'go vet ./... && go test -p 1 -count=1 ./...'; \
		rc=$$?; $(COMPOSE_DEV) --profile test rm -sf postgres-test >/dev/null 2>&1; test $$rc -eq 0
	$(NODE_RUN) sh -c 'npm ci && npm run lint && npm run typecheck && npm test'

e2e: ## Playwright end to end tests against the dev stack (starts it if needed), all in Docker
	@docker compose -f deploy/compose.dev.yml ps --status running --services | grep -qx server || $(MAKE) --no-print-directory up
	docker run --rm --network helmsman-dev_default --ipc=host -v $(CURDIR)/e2e:/e2e -w /e2e \
		-e E2E_BASE_URL=http://localhost:5173 -e E2E_FAKE_GITHUB=http://localhost:8090 \
		-e E2E_FARO_URL=http://alloy:12347 -e E2E_STATIC_URL=http://static-demo \
		-e E2E_EXPECT_LOKI=$(shell $(COMPOSE_DEV) ps --status running --services | grep -qx loki && echo true || echo false) \
		mcr.microsoft.com/playwright:v1.65.0-noble ./run-dev.sh $(E2E_ARGS)

e2e-server: ## Playwright checks of a real deployment. E2E_SERVER_URL, E2E_BREAKGLASS_PASSWORD, E2E_BREAKGLASS_TOTP_SECRET
	docker run --rm --ipc=host -v $(CURDIR)/e2e:/e2e -w /e2e \
		-e E2E_SERVER_URL -e E2E_BREAKGLASS_PASSWORD -e E2E_BREAKGLASS_TOTP_SECRET \
		mcr.microsoft.com/playwright:v1.65.0-noble sh -c 'npm install --no-audit --no-fund --silent && npx playwright test --project=server $(E2E_ARGS)'

seed-dev: ## Seed the dev agent, users and role bindings
	$(COMPOSE_DEV) exec -T postgres psql -U helmsman -d helmsman -v ON_ERROR_STOP=1 < deploy/seed-dev.sql
	@echo "Seeded. Sign in as alice (admin), bob (developer), carol (viewer), erin (approver). The agent joins within a minute."

server-bundle: ## Zip the files a server needs, for copying by hand (see docs/server-setup.md)
	bash deploy/server/bundle.sh

lint: ## Go lint with the version CI runs (golangci-lint)
	docker run --rm -v $(CURDIR):/app -v helmsman-gomod:/go/pkg/mod -w /app golangci/golangci-lint:v2.15.0 golangci-lint run

docs-check: ## Check the docs: titles, relative links and anchors, no em dashes
	docker run --rm -v $(CURDIR):/src -w /src python:3.13-alpine python3 scripts/check-docs.py
