SHELL := bash

GREEN  := $(shell tput setaf 2 2>/dev/null || echo "")
YELLOW := $(shell tput setaf 3 2>/dev/null || echo "")
RED    := $(shell tput setaf 1 2>/dev/null || echo "")
RESET  := $(shell tput sgr0 2>/dev/null || echo "")

.DEFAULT_GOAL := help

# ─── Variables ────────────────────────────────────────────────────────────────
APP_NAME     := shorty
GO           := go
GOARCH       := arm64
GOOS         := linux
BUILD_DIR    := .build
CMD_REDIRECT := cmd/redirect
CMD_API      := cmd/api
CMD_WORKER   := cmd/worker
TF_DIR       := deploy/terraform
ENV          ?= dev

# ─── Help ─────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Available commands
	@echo "Available commands:"
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[0;33m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
	@echo ""

##@ Development

.PHONY: dev-up
dev-up: ## Start full local environment (LocalStack + Redis + Observability)
	@echo "$(GREEN)Starting local environment...$(RESET)"
	docker compose -f docker-compose.yml -f docker-compose.infra.yml up -d
	@echo "$(GREEN)Ready. Grafana: http://localhost:3000 | Jaeger: http://localhost:16686$(RESET)"

.PHONY: dev-down
dev-down: ## Stop local environment
	docker compose -f docker-compose.yml -f docker-compose.infra.yml down

.PHONY: dev-logs
dev-logs: ## Stream all container logs
	docker compose -f docker-compose.yml -f docker-compose.infra.yml logs -f

.PHONY: run-api
run-api: ## Run API service locally with hot reload (Air)
	air -c .air.api.toml

.PHONY: run-redirect
run-redirect: ## Run redirect service locally with hot reload (Air)
	air -c .air.redirect.toml

##@ Specification (Spec-Driven Development)

.PHONY: spec-validate
spec-validate: ## Validate OpenAPI spec (docs/api/openapi.yaml)
	@echo "$(YELLOW)Validating OpenAPI spec...$(RESET)"
	npx @redocly/cli lint docs/api/openapi.yaml
	@echo "$(GREEN)Spec is valid$(RESET)"

.PHONY: spec-gen
spec-gen: spec-validate ## Generate Go server stubs + types from OpenAPI spec
	@echo "$(YELLOW)Generating code from spec...$(RESET)"
	oapi-codegen -config config/oapi-codegen.yaml docs/api/openapi.yaml
	@echo "$(GREEN)Code generated$(RESET)"

.PHONY: spec-docs
spec-docs: ## Serve interactive API docs (Redoc) at http://localhost:8082
	@# @redocly/cli v2 renamed preview-docs → preview; port 8082 avoids
	@# colliding with the API container on :8080.
	npx @redocly/cli preview docs/api/openapi.yaml --port 8082

.PHONY: spec-build-docs
spec-build-docs: ## Build static Redoc HTML → docs/api/redoc.html
	npx @redocly/cli build-docs docs/api/openapi.yaml -o docs/api/redoc.html
	@echo "$(GREEN)Generated: docs/api/redoc.html$(RESET)"

.PHONY: spec-diff
spec-diff: ## Diff current spec against last released version (breaking change detection)
	@echo "$(YELLOW)Checking for breaking changes...$(RESET)"
	npx @redocly/cli diff docs/api/openapi.yaml docs/api/openapi.prev.yaml

##@ BDD (Behavior-Driven Development)

.PHONY: bdd
bdd: ## Run all BDD feature tests (godog)
	@echo "$(YELLOW)Running BDD tests...$(RESET)"
	GODOG_FORMAT=pretty $(GO) test ./tests/bdd/... -v
	@echo "$(GREEN)BDD tests complete$(RESET)"

.PHONY: bdd-feature
bdd-feature: ## Run specific BDD feature: make bdd-feature FEATURE=redirect
	@echo "$(YELLOW)Running feature: $(FEATURE)$(RESET)"
	GODOG_FORMAT=pretty GODOG_TAGS=$(FEATURE) $(GO) test ./tests/bdd/... -v

.PHONY: bdd-report
bdd-report: ## Run BDD tests and generate HTML report
	GODOG_FORMAT=cucumber $(GO) test ./tests/bdd/... > tests/bdd/results.json
	npx cucumber-html-reporter \
		--inputJsonFile tests/bdd/results.json \
		--outputPath tests/bdd/report.html
	@echo "$(GREEN)Report: tests/bdd/report.html$(RESET)"

##@ Testing

.PHONY: test
test: ## Run unit tests with race detector
	@echo "$(YELLOW)Running unit tests...$(RESET)"
	$(GO) test ./internal/... ./pkg/... -race -count=1 -timeout=60s

.PHONY: test-integration
test-integration: ## Run integration tests against LocalStack
	@echo "$(YELLOW)Running integration tests (LocalStack)...$(RESET)"
	$(GO) test ./tests/integration/... -race -count=1 -timeout=120s -tags=integration

.PHONY: test-e2e
test-e2e: ## Run end-to-end tests
	@echo "$(YELLOW)Running E2E tests...$(RESET)"
	$(GO) test ./tests/e2e/... -count=1 -timeout=300s -tags=e2e

.PHONY: test-load
test-load: ## Run k6 baseline load test (1,000 RPS / 5 min)
	@echo "$(YELLOW)Running baseline load test...$(RESET)"
	k6 run tests/load/baseline.js

.PHONY: test-load-stress
test-load-stress: ## Run k6 stress test (ramp to 10,000 RPS)
	k6 run tests/load/stress.js

.PHONY: test-load-spike
test-load-spike: ## Run k6 spike test (instant surge to 5,000 RPS)
	k6 run tests/load/spike.js

.PHONY: test-load-soak
test-load-soak: ## Run k6 soak test (500 RPS × 30 min)
	k6 run tests/load/soak.js

.PHONY: test-all
test-all: test bdd test-integration test-e2e ## Run full test suite: unit + BDD + integration + E2E

.PHONY: coverage
coverage: ## Generate HTML test coverage report
	$(GO) test ./internal/... ./pkg/... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Report: coverage.html$(RESET)"

##@ Build

.PHONY: build
build: build-redirect build-api build-worker ## Build all Lambda binaries (linux/arm64)
	@echo "$(GREEN)All binaries built → $(BUILD_DIR)/$(RESET)"

.PHONY: build-redirect
build-redirect: ## Build redirect Lambda binary
	@mkdir -p $(BUILD_DIR)
	GOARCH=$(GOARCH) GOOS=$(GOOS) CGO_ENABLED=0 \
		$(GO) build -ldflags="-s -w" -o $(BUILD_DIR)/redirect ./$(CMD_REDIRECT)
	cd $(BUILD_DIR) && zip redirect.zip redirect

.PHONY: build-api
build-api: ## Build API Lambda binary
	@mkdir -p $(BUILD_DIR)
	GOARCH=$(GOARCH) GOOS=$(GOOS) CGO_ENABLED=0 \
		$(GO) build -ldflags="-s -w" -o $(BUILD_DIR)/api ./$(CMD_API)
	cd $(BUILD_DIR) && zip api.zip api

.PHONY: build-worker
build-worker: ## Build worker Lambda binary
	@mkdir -p $(BUILD_DIR)
	GOARCH=$(GOARCH) GOOS=$(GOOS) CGO_ENABLED=0 \
		$(GO) build -ldflags="-s -w" -o $(BUILD_DIR)/worker ./$(CMD_WORKER)
	cd $(BUILD_DIR) && zip worker.zip worker

.PHONY: build-clean
build-clean: ## Remove all build artifacts
	rm -rf $(BUILD_DIR)

##@ Code Quality

.PHONY: lint
lint: ## Run golangci-lint
	@echo "$(YELLOW)Linting...$(RESET)"
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format code (gofmt + goimports)
	gofmt -w .
	goimports -w .

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: security-scan
security-scan: ## Run gosec security scanner
	@echo "$(YELLOW)Running security scan...$(RESET)"
	gosec ./...

.PHONY: check
check: fmt vet lint security-scan ## Run all quality checks (fmt + vet + lint + security)

##@ Infrastructure

.PHONY: tf-init
tf-init: ## Terraform init for ENV (default: dev)
	terraform -chdir=$(TF_DIR)/environments/$(ENV) init

.PHONY: tf-plan-dev
tf-plan-dev: ## Terraform plan — dev environment
	terraform -chdir=$(TF_DIR)/environments/dev plan

.PHONY: tf-apply-dev
tf-apply-dev: ## Terraform apply — dev environment
	terraform -chdir=$(TF_DIR)/environments/dev apply

.PHONY: tf-plan-prod
tf-plan-prod: ## Terraform plan — prod environment
	terraform -chdir=$(TF_DIR)/environments/prod plan

.PHONY: tf-apply-prod
tf-apply-prod: ## Terraform apply — prod environment (requires confirmation)
	@echo "$(RED)WARNING: Applying to PRODUCTION. Continue? [y/N]$(RESET)" \
		&& read ans && [ "$${ans}" = "y" ]
	terraform -chdir=$(TF_DIR)/environments/prod apply

.PHONY: tf-destroy-dev
tf-destroy-dev: ## Destroy dev infrastructure (requires confirmation)
	@echo "$(RED)WARNING: Destroying dev infra. Continue? [y/N]$(RESET)" \
		&& read ans && [ "$${ans}" = "y" ]
	terraform -chdir=$(TF_DIR)/environments/dev destroy

##@ Deploy

.PHONY: deploy-dev
deploy-dev: build ## Build and deploy all Lambdas to dev
	@echo "$(YELLOW)Deploying to dev...$(RESET)"
	bash deploy/scripts/deploy.sh dev
	@echo "$(GREEN)Deployed to dev$(RESET)"

.PHONY: deploy-prod
deploy-prod: build ## Build and deploy all Lambdas to prod (requires confirmation)
	@echo "$(RED)WARNING: Deploying to PRODUCTION. Continue? [y/N]$(RESET)" \
		&& read ans && [ "$${ans}" = "y" ]
	bash deploy/scripts/deploy.sh prod
	@echo "$(GREEN)Deployed to prod$(RESET)"

##@ Utilities

.PHONY: seed-local
seed-local: ## Seed local DynamoDB with test data
	$(GO) run deploy/scripts/seed/main.go

.PHONY: migrate
migrate: ## Apply DynamoDB schema migrations (ENV=dev|prod)
	$(GO) run deploy/scripts/migrate/main.go --env=$(ENV)

.PHONY: gen-mocks
gen-mocks: ## Generate interface mocks (mockery)
	mockery --all --keeptree --dir internal --output internal/mocks

.PHONY: gen-openapi
gen-openapi: spec-gen ## Alias for spec-gen (validate + generate stubs)

.PHONY: install-tools
install-tools: ## Install all required dev tools
	@echo "$(YELLOW)Installing Go tools...$(RESET)"
	$(GO) install github.com/air-verse/air@latest
	$(GO) install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	$(GO) install github.com/vektra/mockery/v2@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install github.com/cucumber/godog/cmd/godog@latest
	@echo "$(YELLOW)Installing system tools (brew)...$(RESET)"
	brew install golangci-lint k6 terraform
	@echo "$(YELLOW)Installing npm tools...$(RESET)"
	npm install -g @redocly/cli cucumber-html-reporter
	@echo "$(GREEN)All tools installed$(RESET)"

##@ Aliases

.PHONY: up
up: dev-up ## → dev-up

.PHONY: down
down: dev-down ## → dev-down

.PHONY: r
r: run-api ## → run-api

.PHONY: b
b: build ## → build

.PHONY: t
t: test ## → test (unit)

.PHONY: ti
ti: test-integration ## → test-integration

.PHONY: te
te: test-e2e ## → test-e2e

.PHONY: tl
tl: test-load ## → test-load

.PHONY: l
l: lint ## → lint

.PHONY: f
f: fmt ## → fmt

.PHONY: s
s: spec-validate ## → spec-validate

.PHONY: sg
sg: spec-gen ## → spec-gen

.PHONY: dd
dd: deploy-dev ## → deploy-dev

.PHONY: dp
dp: deploy-prod ## → deploy-prod
