# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**Shorty** — high-performance URL shortener. Stack: AWS Lambda (Go, ARM64), API Gateway v2, DynamoDB, ElastiCache Redis, SQS FIFO, CloudFront + WAF, Cognito SSO. Terraform IaC. Local dev via LocalStack + Docker Compose.

Full requirements: `requirements-init.md`. Subagent role definitions: `requirements-subagents.md`.

## Common Commands

```bash
make dev-up          # start full local stack (LocalStack + Redis + Grafana + Jaeger)
make run-api         # API service with hot reload → http://localhost:8080
make run-redirect    # redirect service with hot reload → http://localhost:8081
make spec-validate   # validate OpenAPI spec (must pass before any implementation)
make spec-gen        # generate Go stubs from spec (run this before writing handlers)
make bdd             # run BDD tests (godog); must be GREEN before merging
make test            # unit tests with race detector
make test-integration # integration tests against LocalStack
make test-e2e        # E2E tests against dev AWS environment
make test-all        # full suite: unit + BDD + integration + E2E
make check           # fmt + vet + lint + security scan (required CI gate)
make build           # build all Lambda binaries (linux/arm64 zips)
make deploy-dev      # build + deploy to dev AWS
make coverage        # HTML coverage report → coverage.html
make help            # list all targets with descriptions
```

Single BDD feature: `make bdd-feature FEATURE=redirect`  
Single test package: `go test ./internal/shortener/... -run TestGenerate -v`

## Engineering Flow (enforced order)

**Spec → BDD/E2E (red) → Implement (green) → Review → SRE**

1. `docs/api/openapi.yaml` is the **single source of truth**. Run `make spec-gen` before writing any handler. Never hand-edit generated files.
2. BDD `.feature` files (`tests/bdd/features/`) and E2E skeletons are written **before** implementation and must fail (red) first.
3. Implementation iterates until `make bdd` and `make test-e2e` are both green.
4. `make spec-validate` is the first CI gate — nothing else runs if the spec is invalid.

## Architecture

Three Lambda entry points in `cmd/`:
- `cmd/redirect` — hot path: `GET /{code}`. Cache-first (Redis → DynamoDB). Publishes click events to SQS **asynchronously** (goroutine + timeout) so it never blocks the redirect.
- `cmd/api` — REST CRUD + stats endpoints. Implements the oapi-codegen-generated `ServerInterface`.
- `cmd/worker` — SQS FIFO consumer. Batch-processes click events into the `clicks` DynamoDB table.

`internal/` packages are shared across all three Lambdas:
- `store/` — DynamoDB repository; all access patterns keyed on `LINK#{code}` PK.
- `cache/` — Redis adapter; the redirect Lambda always checks here first.
- `ratelimit/` — sliding-window rate limiter using a Redis Lua script for atomicity.
- `auth/` — JWT validation + Cognito integration; used as Lambda authorizer middleware.
- `telemetry/` — single OpenTelemetry setup wired to Jaeger locally and X-Ray in AWS.
- `shortener/` — Base62 code generation with collision retry + exponential backoff.

Lambda initialization (AWS SDK clients, Redis pool, OTel tracer) happens **outside** the handler function to survive across warm invocations.

## Data Model

DynamoDB single-table design with two physical tables (`links`, `clicks`) and one `users` table. Key patterns:
- `LINK#{code}` / `META` — link record; DynamoDB TTL on `expires_at` handles time-based expiry.
- `LINK#{code}` / `CLICK#{ts}#{uuid}` — per-click events; 90-day TTL.
- Click-count limit (`max_clicks`) is enforced via a DynamoDB `ConditionalExpression` on `click_count`.
- GSI `owner_id-created_at-index` on `links` powers the user dashboard list.

## Local Observability

| Service | URL |
|---|---|
| Grafana | http://localhost:3000 |
| Jaeger | http://localhost:16686 |
| API docs (Redoc) | `make spec-docs` → http://localhost:8082 |

Grafana dashboards are provisioned automatically from `config/grafana/dashboards/`.

## Key Constraints

- Redirect p99 target: **< 100 ms**. Any change to the redirect critical path must be benchmarked.
- Rate limiting is enforced **before** business logic in every Lambda handler.
- IP addresses are never stored in plain text — always SHA-256(IP + secret_salt).
- Lambda binaries must be built for `GOARCH=arm64 GOOS=linux CGO_ENABLED=0` (SnapStart, Graviton).
- Terraform manages all AWS resources. No manual console changes.
