# Shorty

[![CI](https://github.com/romanitalian/shorty/actions/workflows/ci.yml/badge.svg)](https://github.com/romanitalian/shorty/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.23+-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Coverage](https://img.shields.io/badge/coverage-report-blue)](coverage.html)
[![OpenAPI](https://img.shields.io/badge/API-OpenAPI%203.0-green)](docs/api/openapi.yaml)

High-performance URL shortener built for scale. Redirect p99 < 100 ms at 10,000 RPS.

**Stack:** AWS Lambda (Go, ARM64) · API Gateway v2 · DynamoDB · ElastiCache Redis · SQS FIFO · CloudFront + WAF · Cognito SSO · Terraform IaC

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [API](#api)
- [Development](#development)
- [Testing](#testing)
- [Deployment](#deployment)
- [Performance](#performance)
- [Security](#security)
- [Contributing](#contributing)
- [Roadmap](#roadmap)
- [License](#license)

---

## Features

| Feature | Details |
|---|---|
| **Fast redirect** | Cache-first: Redis → DynamoDB. Click events processed async via SQS — never blocks the redirect |
| **Link management** | Custom aliases, time/click-based TTL, password protection, UTM auto-append |
| **Analytics** | Per-link stats: clicks over time, geography, referrers, device types |
| **Auth** | AWS Cognito SSO (Google OAuth, GitHub OAuth, email/password); JWT in httpOnly cookie |
| **Rate limiting** | Sliding-window per IP/user via Redis Lua script; WAF rules at the edge |
| **Observability** | OpenTelemetry traces → Jaeger/X-Ray; Prometheus metrics → Grafana; structured JSON logs → Loki |
| **Infrastructure** | 100% Terraform. Canary deployments via Lambda aliases. DynamoDB single-table design |

---

## Architecture

```
Internet
    │
    ▼
CloudFront  ──  WAF (Bot Control, Rate-Based Rules, CAPTCHA)
    │
    ▼
API Gateway v2
    ├──► Lambda: redirect   GET /{code}         ← hot path, provisioned concurrency
    ├──► Lambda: api        REST CRUD + stats
    └──► Lambda: worker     SQS click consumer
         │
         ├── DynamoDB        (links · clicks · users)
         ├── ElastiCache Redis  (cache · rate limiter · sessions)
         ├── SQS FIFO        (click events)
         ├── Cognito         (SSO)
         └── CloudWatch + X-Ray
```

**Local stack** (Docker Compose): LocalStack · Redis · Grafana · Prometheus · Jaeger · Loki

Three Lambda entry points in `cmd/`; shared logic in `internal/`:

| Package | Responsibility |
|---|---|
| `internal/shortener` | Base62 code generation, collision retry + exponential backoff |
| `internal/store` | DynamoDB repository; all access patterns keyed on `LINK#{code}` |
| `internal/cache` | Redis adapter for redirect-path cache |
| `internal/ratelimit` | Sliding-window + token bucket, Redis Lua for atomicity |
| `internal/auth` | JWT validation, Cognito Lambda authorizer |
| `internal/telemetry` | Single OpenTelemetry setup → Jaeger (local) / X-Ray (AWS) |

---

## Quick Start

### Prerequisites

- Go 1.23+
- Docker + Docker Compose
- `make`

### Run locally

```bash
git clone https://github.com/romanitalian/shorty
cd shorty
cp .env.example .env          # fill in local values
make install-tools            # installs Go/npm/brew tooling (one-time)
make dev-up                   # starts LocalStack + Redis + Grafana + Jaeger
make seed-local               # seeds DynamoDB with test data
make run-api                  # API → http://localhost:8080
make run-redirect             # redirect service → http://localhost:8081
```

| Service | URL |
|---|---|
| API | http://localhost:8080 |
| Redirect | http://localhost:8081 |
| API docs (Redoc) | `make spec-docs` → http://localhost:8082 |
| Grafana | http://localhost:3000 |
| Jaeger | http://localhost:16686 |

### Create your first short link

```bash
curl -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/very/long/path"}'
# → {"code":"abc1234","short_url":"http://localhost:8081/abc1234"}
```

---

## Configuration

Copy `.env.example` to `.env` and set the values:

```dotenv
# DynamoDB (LocalStack in dev)
DYNAMODB_ENDPOINT=http://localhost:4566
DYNAMODB_REGION=us-east-1

# Redis
REDIS_ADDR=localhost:6379

# Auth
COGNITO_USER_POOL_ID=...
COGNITO_CLIENT_ID=...
JWT_SECRET=...

# Security
IP_HASH_SALT=...          # SHA-256 salt for IP anonymization

# Feature flags
SAFE_BROWSING_API_KEY=... # Google Safe Browsing (optional)
```

All secrets in production are stored in **AWS Secrets Manager** — never in environment variables or code.

---

## API

The OpenAPI 3.0 spec at [`docs/api/openapi.yaml`](docs/api/openapi.yaml) is the **single source of truth**. Go server stubs are generated from it — never hand-edit generated files.

```bash
make spec-validate   # validate spec (first CI gate)
make spec-gen        # regenerate Go stubs
make spec-docs       # serve interactive docs at :8082
```

### Endpoints at a glance

```
GET  /{code}                              # redirect (public)
POST /p/{code}                            # password entry

POST   /api/v1/links                      # create link
GET    /api/v1/links                      # list links (paginated)
GET    /api/v1/links/{code}               # link details
PATCH  /api/v1/links/{code}               # update link
DELETE /api/v1/links/{code}               # delete link

GET    /api/v1/links/{code}/stats         # aggregate stats
GET    /api/v1/links/{code}/stats/timeline
GET    /api/v1/links/{code}/stats/geo
GET    /api/v1/links/{code}/stats/referrers

GET    /api/v1/me                         # profile + quota
POST   /api/v1/shorten                    # guest creation (rate-limited)
```

Full request/response schemas, error codes, and examples are in the spec.

---

## Development

### Common commands

```bash
make dev-up          # start local stack
make run-api         # API hot reload
make run-redirect    # redirect hot reload
make spec-validate   # validate OpenAPI spec
make spec-gen        # generate Go stubs
make check           # fmt + vet + lint + security scan
make build           # build all Lambda binaries (linux/arm64)
make help            # list all targets
```

### Engineering flow (enforced order)

Every change follows this sequence — CI enforces it:

```
1. SPEC      Update docs/api/openapi.yaml → make spec-validate
2. BDD       Write .feature file, confirm it FAILS → make bdd
3. IMPLEMENT Code until BDD is green → make bdd && make test-all
4. REVIEW    make check passes
```

### Project structure

```
shorty/
├── cmd/
│   ├── redirect/main.go   # Lambda: hot-path redirect
│   ├── api/main.go        # Lambda: REST API
│   └── worker/main.go     # Lambda: SQS click processor
├── internal/              # shared packages (shortener, store, cache, …)
├── pkg/apierr/            # standardized API error types
├── tests/
│   ├── bdd/features/      # Gherkin .feature files
│   ├── integration/       # LocalStack integration tests
│   ├── e2e/               # full AWS E2E tests
│   └── load/              # k6 load scenarios
├── docs/
│   ├── api/openapi.yaml   # OpenAPI spec (source of truth)
│   ├── adr/               # Architecture Decision Records
│   └── rfcs/              # RFC documents for XL changes
├── deploy/terraform/      # all AWS infrastructure
└── config/                # Grafana dashboards, Prometheus alerts, …
```

---

## Testing

```bash
make test                # unit tests (race detector)
make bdd                 # BDD scenarios (godog)
make test-integration    # integration tests against LocalStack
make test-e2e            # E2E tests against dev AWS
make test-all            # full suite
make coverage            # HTML coverage report → coverage.html
```

Single test or feature:

```bash
go test ./internal/shortener/... -run TestGenerate -v
make bdd-feature FEATURE=redirect
```

### CI gates (all required before merge)

| Gate | Command |
|---|---|
| OpenAPI valid | `make spec-validate` |
| Code quality | `make check` (fmt + vet + golangci-lint + gosec) |
| Unit tests | `make test` |
| BDD scenarios | `make bdd` |
| Integration | `make test-integration` |
| Build | `make build` (linux/arm64) |

---

## Deployment

### Infrastructure

All AWS resources are managed by Terraform — no manual console changes.

```bash
make tf-plan-dev     # preview dev changes
make tf-apply-dev    # apply to dev
make tf-plan-prod    # preview prod changes (requires approval)
make tf-apply-prod   # apply to prod
```

### Lambda deployment

```bash
make deploy-dev      # build + deploy to dev
make deploy-prod     # build + deploy to prod (canary)
```

**Canary strategy:** new code starts at 10% traffic via Lambda alias weighted routing. Auto-promotes to 100% if `error_rate < 0.1%` for 10 minutes; auto-rolls back if `error_rate > 1%`.

### Environments

| Environment | Branch | Notes |
|---|---|---|
| Local | any | Docker Compose + LocalStack |
| Dev | `main` | Auto-deployed on merge; E2E + load tests run here |
| Prod | `vX.Y.Z` tag | Canary deploy, manual approval gate |

---

## Performance

| Metric | Target |
|---|---|
| Redirect p50 | < 20 ms |
| Redirect p99 | < 100 ms |
| Link creation p99 | < 300 ms |
| Throughput | 10,000 RPS |
| Lambda cold start | < 500 ms |

The redirect Lambda uses `provisioned_concurrency = 2` (Go has no SnapStart). Any change to the redirect critical path must include benchmark results in the PR.

---

## Security

- IP addresses are **never stored in plain text** — `SHA-256(IP + secret_salt)`.
- All data encrypted in transit (TLS 1.3) and at rest (DynamoDB SSE + KMS).
- OWASP Top 10 mitigations; WAF Bot Control + Rate-Based Rules at the edge.
- Secrets live in AWS Secrets Manager; never in code or environment variables.

**Found a vulnerability?** Please use [GitHub Security Advisories](../../security/advisories/new) — not a public issue.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full guide. Key points:

- Every PR must close a GitHub Issue; PRs without a linked issue are not reviewed.
- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat(redirect):`, `fix(ratelimit):`, `perf(api):` …
- XL features require an RFC in `docs/rfcs/` before implementation.
- `make test-all` and `make check` must pass locally before requesting review.

```bash
# Fork + clone, then:
git checkout -b feat/your-feature
# … make changes …
make check && make test-all
git commit -m "feat(api): add QR code endpoint (closes #42)"
gh pr create
```

---

## Roadmap

**v1.0 MVP**
- [x] Core redirect, creation, and dashboard
- [x] Email/password + Google SSO
- [x] Free tier quotas
- [ ] Full BDD suite green
- [ ] E2E tests in CI

**Post-MVP**
- [ ] GitHub SSO
- [ ] Pro/Enterprise plans + Stripe billing
- [ ] QR code generation
- [ ] Developer API with API key management
- [ ] White-label (custom short domains)
- [ ] A/B redirect testing (split traffic)
- [ ] Webhooks (click, expiration events)
- [ ] Bulk import via CSV

See the [GitHub Milestones](../../milestones) for sprint-by-sprint breakdown.

---

## License

[MIT](LICENSE) © romanitalian
