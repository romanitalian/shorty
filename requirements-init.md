# Shorty — URL Shortener Service
## Requirements & Architecture Document

---

## 1. Product Vision

**Shorty** is an enterprise-grade, high-performance URL shortener service.  
Core principles: **performance**, **reliability**, **security**, **observability**.

**Target audience:** developers, marketers, enterprise users.

---

## 2. Functional Requirements

### 2.1 Link Creation

| Parameter | Description |
|---|---|
| Code generation | Automatic (Base62, 7–8 chars) or user-defined custom alias |
| Time-based TTL | Optional: expiration date/time (Unix timestamp) |
| Click-based TTL | Optional: maximum number of redirects (link deactivates after limit) |
| Password | Optional: bcrypt-hashed password; redirect requires password entry |
| UTM parameters | Optional: auto-append utm_source/utm_medium/utm_campaign |
| Title | Optional: human-readable label for the link |

### 2.2 Redirect

- Redirect: HTTP 301 (cacheable) or 302 (analytics). Default — 302.
- TTL check before redirect.
- Password check before redirect (HTML form with CSRF token).
- Click counter increment (asynchronous, via SQS → Lambda).
- Click event recording: IP (hashed), User-Agent, Referer, country (GeoIP), timestamp.

### 2.3 User Dashboard

- **Link list:** pagination, filters (active/expired), sorting.
- **Per-link statistics:**
  - Unique/total clicks (by day, week, month)
  - Geography (top countries)
  - Traffic sources (Referer)
  - Device types (Desktop/Mobile/Bot)
  - Time-series chart
- **Management:** edit, deactivate, delete, copy.
- **Quotas:** usage display (links today / limit, total active / limit).

### 2.4 Authentication & Authorization

- **SSO via AWS Cognito:**
  - Google OAuth 2.0
  - GitHub OAuth 2.0
  - Email/Password (Cognito User Pool)
- JWT tokens (Access + Refresh), stored in httpOnly cookie.
- Guest mode: create links without registration (strict quotas apply).

---

## 3. Non-Functional Requirements

### 3.1 Performance

| Metric | Target |
|---|---|
| Redirect latency (p50) | < 20 ms |
| Redirect latency (p99) | < 100 ms |
| Link creation latency (p99) | < 300 ms |
| Throughput | 10,000 RPS (redirects) |
| Lambda cold start | < 500 ms (SnapStart / ARM Graviton) |

### 3.2 Reliability

| Metric | Target |
|---|---|
| Availability | 99.9% (≈ 8.7 h downtime/year) |
| RTO | < 5 min |
| RPO | < 1 min |
| Data replication | DynamoDB global tables (multi-region) |

### 3.3 Security

- All data encrypted in transit (TLS 1.3) and at rest (DynamoDB SSE, KMS).
- No PII stored in plain text (IP — SHA-256 + salt).
- OWASP Top 10 mitigation.
- Secrets Management — AWS Secrets Manager / SSM Parameter Store.

---

## 4. Technology Stack

### 4.1 AWS Production

```
Internet
    │
    ▼
AWS CloudFront (CDN, TLS termination, WAF)
    │
    ▼
AWS WAF (bot control, rate limiting L7, geo-blocking)
    │
    ▼
AWS API Gateway v2 (HTTP API)
    │
    ├──► Lambda: redirect        (Go, ARM64, SnapStart)
    ├──► Lambda: api-create      (Go, ARM64)
    ├──► Lambda: api-stats       (Go, ARM64)
    └──► Lambda: api-auth        (Go, ARM64, Cognito authorizer)
         │
         ├──► DynamoDB           (links, clicks, users tables)
         ├──► ElastiCache Redis  (rate limiter, short-code cache, session)
         ├──► SQS FIFO           (click events queue)
         ├──► Lambda: click-processor  (SQS consumer)
         ├──► S3                 (static assets, Lambda artifacts)
         ├──► Cognito            (SSO)
         ├──► CloudWatch + X-Ray (metrics, traces, logs)
         └──► Secrets Manager    (API keys, salts)
```

### 4.2 Local Development

```
Docker Compose
    ├── LocalStack   (DynamoDB, SQS, S3, SecretsManager, IAM)
    ├── Redis        (rate limiter, cache)
    ├── Grafana      (dashboards: metrics + logs)
    ├── Prometheus   (scrape Go metrics)
    ├── Jaeger       (distributed tracing, OpenTelemetry)
    ├── Loki         (log aggregation)
    └── app          (Go binary, hot reload via Air)
```

### 4.3 Go Services

| Service | Description |
|---|---|
| `cmd/redirect` | Lambda: handle GET /{code} → redirect |
| `cmd/api` | Lambda: REST API (create, read, update, delete, stats) |
| `cmd/worker` | Lambda: click queue processor |
| `internal/shortener` | Code generation and validation |
| `internal/store` | DynamoDB repository |
| `internal/cache` | Redis adapter |
| `internal/ratelimit` | Rate limiter (sliding window, token bucket) |
| `internal/auth` | JWT validation, Cognito integration |
| `internal/telemetry` | OpenTelemetry setup (traces + metrics) |

---

## 5. Data Schema (DynamoDB)

### Table: `links`

| Attribute | Type | Notes |
|---|---|---|
| `PK` | S | `LINK#{code}` |
| `SK` | S | `META` |
| `owner_id` | S | user_id or `ANON#{ip_hash}` |
| `original_url` | S | original URL |
| `code` | S | short code |
| `title` | S | nullable |
| `password_hash` | S | nullable, bcrypt |
| `expires_at` | N | Unix timestamp, nullable (DynamoDB TTL attribute) |
| `max_clicks` | N | nullable |
| `click_count` | N | atomic counter |
| `is_active` | BOOL | |
| `created_at` | N | Unix timestamp |
| `updated_at` | N | Unix timestamp |

GSI: `owner_id-created_at-index` — for dashboard (all user links).

### Table: `clicks`

| Attribute | Type | Notes |
|---|---|---|
| `PK` | S | `LINK#{code}` |
| `SK` | S | `CLICK#{timestamp}#{uuid}` |
| `ip_hash` | S | SHA-256(IP + secret_salt) |
| `country` | S | ISO 3166-1 alpha-2 |
| `device_type` | S | desktop/mobile/bot |
| `referer_domain` | S | nullable |
| `user_agent_hash` | S | SHA-256 |
| `created_at` | N | TTL: 90 days |

GSI: `code-date-index` — for daily aggregation.

### Table: `users`

| Attribute | Type | Notes |
|---|---|---|
| `PK` | S | `USER#{cognito_sub}` |
| `SK` | S | `PROFILE` |
| `email` | S | |
| `plan` | S | `free` / `pro` / `enterprise` |
| `daily_link_quota` | N | default: 50 |
| `total_link_quota` | N | default: 500 |
| `created_at` | N | |

---

## 6. API Design

### Public

```
GET  /{code}        # redirect
POST /p/{code}      # password entry (form submit)
```

### API v1 (JWT required)

```
POST   /api/v1/links                        # create link
GET    /api/v1/links                        # list links (paginated)
GET    /api/v1/links/{code}                 # link details
PATCH  /api/v1/links/{code}                 # update link
DELETE /api/v1/links/{code}                 # delete link

GET    /api/v1/links/{code}/stats           # aggregate statistics
GET    /api/v1/links/{code}/stats/timeline  # click time series
GET    /api/v1/links/{code}/stats/geo       # geography breakdown
GET    /api/v1/links/{code}/stats/referrers # referrer sources

GET    /api/v1/me                           # profile + quota usage
```

### Guest (IP rate-limited)

```
POST   /api/v1/shorten      # create link without auth (strict limits)
```

---

## 7. Rate Limiting & Protection

### 7.1 Rate Limit Tiers

| Scope | Limit | Algorithm |
|---|---|---|
| IP (anonymous), redirect | 200 req/min | Sliding window (Redis) |
| IP (anonymous), creation | 5 links/hour | Token bucket (Redis) |
| User (free), creation | 50 links/day, 500 total | Quota (DynamoDB atomic counter) |
| User (pro), creation | 500 links/day, 10,000 total | |
| User (enterprise) | custom | |
| IP, global | 1,000 req/min | AWS WAF |

### 7.2 Anti-Bot & Anti-Abuse

- **AWS WAF Bot Control** — managed rule group, blocks known bots.
- **AWS WAF Rate-Based Rules** — auto-block IP on threshold breach.
- **CAPTCHA (AWS WAF CAPTCHA)** — triggered on suspicious activity at /api/v1/shorten.
- **Honeypot field** — hidden field in the link creation form.
- **Pattern analysis:** single IP creating > 3 links/min → temporary 1-hour block.
- **User-Agent validation** — WAF-level blocking of obvious bot UAs.
- **Geo-blocking** — optional, via WAF.

### 7.3 DDoS Protection

- **AWS Shield Standard** — baseline protection (included free).
- **CloudFront** — edge traffic absorption, caching of 301 redirects.
- **API Gateway throttling** — 10,000 req/s burst, 5,000 req/s steady-state.
- **Lambda concurrency limits** — prevent runaway scaling costs.

### 7.4 Storage Abuse Prevention

- Anonymous user links: TTL max 24 hours, max 5 links/IP/day.
- Expired link cleanup: DynamoDB TTL (automatic).
- Original URL max length: 2,048 characters.
- Malicious URL blocking: Google Safe Browsing API integration.

---

## 8. Observability

### 8.1 Metrics (Prometheus / CloudWatch)

- `shorty_redirects_total{code, status}` — redirect counter
- `shorty_redirect_duration_seconds` — latency histogram
- `shorty_links_created_total{plan}` — link creation counter
- `shorty_rate_limit_hits_total{type}` — rate limiter activations
- `shorty_cache_hit_ratio` — Redis hit/miss ratio
- `shorty_active_links_total` — total active links
- Lambda: duration, errors, throttles, cold starts

### 8.2 Traces (Jaeger / AWS X-Ray)

- OpenTelemetry SDK in every Go service.
- Trace propagation: CloudFront → API Gateway → Lambda → DynamoDB/Redis/SQS.
- Span attributes: `link.code`, `user.id`, `cache.hit`.

### 8.3 Logs

- Structured JSON (zerolog/zap).
- Fields: `level`, `timestamp`, `trace_id`, `span_id`, `service`, `event`, `duration_ms`.
- Local: Loki + Grafana. Production: CloudWatch Logs + Insights.

### 8.4 Grafana Dashboards (Local)

1. **Overview** — RPS, latency, error rate, active links
2. **Rate Limiting** — hits, blocked IPs by limiter type
3. **Cache** — hit ratio, evictions
4. **Business** — new links/day, clicks/day, DAU
5. **Infrastructure** — Lambda duration/errors/cold starts

### 8.5 Alerts

- Error rate > 1% over 5 min → PagerDuty/Slack
- p99 latency > 500 ms → Warning
- DynamoDB throttling > 0 → Warning
- Rate limiter > 1,000 hits/min → Warning (potential attack)

---

## 9. Project Structure

```
shorty/
├── Makefile                          # all operations via make (SHELL=bash, help target)
├── docker-compose.yml                # local dev environment (LocalStack, Redis, app)
├── docker-compose.infra.yml          # observability stack (Grafana, Jaeger, Prometheus, Loki)
├── .env.example
├── .air.api.toml                     # Air hot-reload config for API service
├── .air.redirect.toml                # Air hot-reload config for redirect service
├── .golangci.yml                     # golangci-lint config
│
├── docs/
│   ├── api/
│   │   ├── openapi.yaml              # OpenAPI 3.0 spec — SOURCE OF TRUTH (spec-first)
│   │   └── openapi.prev.yaml         # previous release spec (for breaking-change diff)
│   ├── adr/                          # Architecture Decision Records
│   ├── design/                       # wireframes, design system, user flows
│   ├── product/                      # backlog, MVP scope, acceptance criteria
│   ├── sre/                          # SLO, runbooks, incident response
│   └── qa/                           # test plan, quality gates
│
├── config/
│   ├── oapi-codegen.yaml             # oapi-codegen config (types + server stubs)
│   ├── grafana/dashboards/           # Grafana dashboard JSON files
│   ├── prometheus/
│   │   ├── prometheus.yml
│   │   └── alerts.yml
│   └── jaeger/
│
├── cmd/
│   ├── redirect/main.go              # Lambda: redirect handler
│   ├── api/main.go                   # Lambda: REST API
│   └── worker/main.go                # Lambda: SQS click processor
│
├── internal/
│   ├── shortener/                    # code generation (Base62, collision retry)
│   ├── store/                        # DynamoDB repository
│   ├── cache/                        # Redis adapter
│   ├── ratelimit/                    # sliding window + token bucket (Redis Lua)
│   ├── auth/                         # JWT validation, Cognito integration
│   ├── geo/                          # GeoIP lookup
│   ├── telemetry/                    # OpenTelemetry setup
│   ├── validator/                    # URL validation, Safe Browsing API
│   └── mocks/                        # generated mocks (mockery)
│
├── pkg/
│   └── apierr/                       # standardized API error types
│
├── tests/
│   ├── bdd/                          # BDD feature tests (godog + Gherkin)
│   │   ├── features/
│   │   │   ├── redirect.feature
│   │   │   ├── create_link.feature
│   │   │   ├── rate_limit.feature
│   │   │   ├── password_link.feature
│   │   │   ├── ttl_expiry.feature
│   │   │   └── stats.feature
│   │   └── steps/                    # Go step definitions
│   ├── integration/                  # Go integration tests (LocalStack)
│   ├── e2e/                          # End-to-end tests (full AWS flow)
│   └── load/                         # k6 load scenarios
│       ├── baseline.js               # 1,000 RPS / 5 min
│       ├── stress.js                 # ramp to 10,000 RPS
│       ├── spike.js                  # instant 5,000 RPS surge
│       └── soak.js                   # 500 RPS × 30 min
│
└── deploy/
    ├── terraform/
    │   ├── modules/
    │   │   ├── lambda/
    │   │   ├── dynamodb/
    │   │   ├── cognito/
    │   │   ├── cloudfront/
    │   │   ├── waf/
    │   │   ├── elasticache/
    │   │   ├── sqs/
    │   │   └── monitoring/
    │   ├── environments/
    │   │   ├── dev/
    │   │   └── prod/
    │   └── main.tf
    └── scripts/
        ├── bootstrap.sh              # first-time AWS account setup
        ├── deploy.sh                 # Lambda artifact upload
        ├── seed/main.go              # seed local DynamoDB
        └── migrate/main.go           # DynamoDB schema migrations
```

---

## 10. Makefile — Key Targets

The Makefile uses `SHELL := bash`, color output, `.DEFAULT_GOAL := help`,
and the `##@` section / `##` inline-comment convention for auto-generated help.

```
Development        dev-up, dev-down, dev-logs, run-api, run-redirect
Specification      spec-validate, spec-gen, spec-docs, spec-diff
BDD                bdd, bdd-feature FEATURE=<tag>, bdd-report
Testing            test, test-integration, test-e2e, test-load, test-all, coverage
Build              build, build-redirect, build-api, build-worker, build-clean
Code Quality       lint, fmt, vet, security-scan, check
Infrastructure     tf-init, tf-plan-dev, tf-apply-dev, tf-plan-prod, tf-apply-prod, tf-destroy-dev
Deploy             deploy-dev, deploy-prod
Utilities          seed-local, migrate, gen-mocks, gen-openapi, install-tools
Aliases            up, down, r, b, t, ti, te, tl, l, f, s, sg, dd, dp
```

---

## 11. Development Process

### 11.1 Engineering Flow (Spec-Driven + BDD + E2E First)

Every feature follows this strict sequence — no code is written before the earlier stages are done:

```
┌─────────────────────────────────────────────────────────────────┐
│  1. SPEC       Write / update OpenAPI spec (docs/api/openapi.yaml)
│                make spec-validate  →  make spec-docs
│                                                                  │
│  2. PLAN       PLANNER breaks spec into tasks, assigns roles     │
│                                                                  │
│  3. ARCHITECT  ADR for new patterns; data model / IAM updates    │
│                                                                  │
│  4. BDD        Write Gherkin .feature files (tests/bdd/features/)│
│                make bdd  →  all scenarios FAIL (red)             │
│                                                                  │
│  5. E2E        Write E2E test skeletons (tests/e2e/)             │
│                make test-e2e  →  compile-pass, runtime FAIL      │
│                                                                  │
│  6. IMPLEMENT  Go Developer writes code until BDD + E2E go green │
│                make spec-gen  →  implement against generated stubs│
│                                                                  │
│  7. REVIEW     Code Reviewer: correctness, security, perf, idioms│
│                                                                  │
│  8. SRE        Update dashboards, runbooks, SLO error budget     │
└─────────────────────────────────────────────────────────────────┘
```

**Rules:**
- `docs/api/openapi.yaml` is the **single source of truth**. Server stubs and client types are always generated from it (`make spec-gen`). Hand-editing generated files is forbidden.
- BDD `.feature` files are committed **before** any implementation code in the same PR.
- E2E tests must pass in CI against the dev AWS environment before a PR can merge to `main`.
- `make test-all` (unit + BDD + integration + E2E) is a required CI gate.

### 11.2 BDD Feature Structure

Feature files live in `tests/bdd/features/`. Each file maps to a domain concept:

```gherkin
# tests/bdd/features/redirect.feature
Feature: URL redirect
  As a visitor
  I want short links to redirect me to the original URL
  So that I can reach the destination quickly

  Scenario: Redirect to active link
    Given a short link "abc123" pointing to "https://example.com"
    When I visit "/{short_domain}/abc123"
    Then I am redirected to "https://example.com" with status 302

  Scenario: Expired link returns 410
    Given a short link "xyz" with TTL expired 1 hour ago
    When I visit "/{short_domain}/xyz"
    Then I receive HTTP status 410

  Scenario: Click-limited link deactivates after limit
    Given a short link "lim1" with max_clicks=1
    When I visit it for the first time
    Then I am redirected successfully
    When I visit it for the second time
    Then I receive HTTP status 410
```

Step definitions are implemented in Go using `godog` (`tests/bdd/steps/`).

### 11.3 Local Development Setup

```bash
make install-tools   # one-time: install all CLI tools
cp .env.example .env # configure local environment
make dev-up          # start LocalStack + Redis + Grafana + Jaeger
make seed-local      # populate local DynamoDB with test data
make run-api         # API with hot reload  → http://localhost:8080
make run-redirect    # Redirect with hot reload → http://localhost:8081
# Grafana:  http://localhost:3000
# Jaeger:   http://localhost:16686
# API docs: make spec-docs → http://localhost:8082
```

### 11.4 CI/CD Pipeline (GitHub Actions)

```
Pull Request:
  → spec-validate                    (OpenAPI spec must be valid)
  → check (fmt + vet + lint + gosec)
  → test (unit, race detector)
  → bdd (godog, all scenarios green)
  → test-integration (LocalStack)
  → build (linux/arm64 Lambda zips)

Merge to main:
  → all PR gates above
  → deploy to dev (Terraform + Lambda update)
  → test-e2e (against dev AWS environment)
  → test-load baseline (k6, 1,000 RPS, p99 ≤ 100 ms)

Tag (semver vX.Y.Z):
  → deploy to prod (approval gate required)
  → canary: 10% traffic via Lambda alias weighted routing
  → promote to 100% if error rate < 0.1% for 10 min
  → automatic rollback if error rate > 1%
```

### 11.5 AWS Deployment

- **Terraform** manages all infrastructure (no manual console changes).
- Lambda deployed via `aws lambda update-function-code` + alias shift.
- Canary deployment: Lambda aliases + API Gateway weighted routing.
- DynamoDB schema changes: blue/green table swap + data migration script.

---

## 12. Quotas & Limits Summary

| Parameter | Anonymous | Free | Pro | Enterprise |
|---|---|---|---|---|
| Links/day | 5 | 50 | 500 | Unlimited |
| Total active links | 10 | 500 | 10,000 | Unlimited |
| Max link TTL | 24 h | 1 year | No expiry | No expiry |
| Custom alias | - | + | + | + |
| Password-protected links | - | + | + | + |
| Stats history depth | - | 30 days | 1 year | Unlimited |
| API access | - | - | + | + |
| Redirects/min (per IP) | 200 | 500 | 2,000 | Custom |
| Creations/hour (per IP) | 5 | 50 | 500 | Custom |

---

## 13. Security Threat Matrix

| Threat | Mitigation |
|---|---|
| DDoS L3/L4 | AWS Shield Standard |
| DDoS L7 / HTTP flood | CloudFront + WAF Rate-Based Rules |
| Bot scraping | WAF Bot Control |
| Storage abuse | Quotas, anonymous TTL, Safe Browsing |
| NoSQL injection | Parameterized DynamoDB SDK queries |
| XSS | CSP headers, output encoding |
| CSRF | SameSite=Strict cookie, CSRF token on forms |
| Password brute-force | Rate limit on /p/{code}: 5 attempts/15 min/IP |
| Open redirect abuse | URL validation + Safe Browsing API |
| Data leakage | IP hashing, no PII in logs |
| Secrets exposure | AWS Secrets Manager, no secrets in code |
| Privilege escalation | IAM least privilege per Lambda |

---

## 14. MVP Scope & Technical Debt

**MVP (v1.0):**
- Basic redirects, link creation, dashboard, statistics
- Email/password + Google SSO
- Free tier quotas only

**Post-MVP:**
- GitHub SSO
- Pro/Enterprise plans + billing (Stripe)
- Bulk import (CSV)
- QR code generation
- Developer API (API Key management)
- White-label (custom domain for short links)
- A/B redirect testing (split traffic)
- Webhooks on events (click, expiration)
