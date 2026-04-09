# Shorty Sprint Plan

## Overview

7 sprints (0-6), 44-46 tasks, 13 agent roles. Follows strict Spec -> BDD (red) -> Implement (green) -> Review -> SRE flow.

**Estimated total duration:** 10-15 sessions (depending on token budget per session).

---

## Sprint 0 -- Foundation

**Goal:** Produce all specification documents; zero implementation code.

**Duration estimate:** 2-3 sessions

### Tasks

| ID | Agent | Description | Inputs | Outputs | Effort |
|---|---|---|---|---|---|
| S0-T01 | pm | Product backlog, MVP scope, acceptance criteria, KPIs | `requirements-init.md` | `docs/product/{backlog,mvp-scope,acceptance-criteria,kpis}.md` | M |
| S0-T02 | architect | OpenAPI 3.0 spec (13 endpoints) + oapi-codegen config | `requirements-init.md` | `docs/api/openapi.yaml`, `config/oapi-codegen.yaml` | L |
| S0-T03 | architect | Architecture Decision Records (ADR-001..010) | `docs/api/openapi.yaml`, `requirements-init.md` | `docs/adr/001-*.md` .. `docs/adr/010-*.md` | M |
| S0-T04 | architect | Data model, IAM matrix, sequence diagrams | `docs/api/openapi.yaml` | `docs/architecture/{data-model,iam-matrix}.md`, `docs/architecture/sequence-diagrams/` | M |
| S0-T05 | dba | DynamoDB access patterns, schema, atomic patterns | `docs/architecture/data-model.md` | `docs/db/{dynamodb-access-patterns,dynamodb-schema,atomic-patterns}.md` | M |
| S0-T06 | dba | Redis design, capacity planning, migration strategy | `docs/architecture/data-model.md` | `docs/db/{redis-design,capacity-planning,migration-strategy}.md` | M |
| S0-T07 | planner | Sprint plan, dependency graph, decisions log | `docs/product/backlog.md`, `docs/api/openapi.yaml` | `plan/{sprint-plan,dependency-graph,decisions-log}.md` | M |

### Parallelization

- **Wave 1:** T01 || T02 (no dependencies)
- **Wave 2:** T03 || T04 (both depend on T02 only)
- **Wave 3:** T05 || T06 || T07 (T05/T06 depend on T04; T07 depends on T01+T02)

### Backlog Coverage

- T01 creates the backlog itself (all US-xxx items)
- T02 covers the API contract for US-001, US-002, US-010..US-015, US-020..US-024, US-030..US-033, US-040..US-043, US-050..US-053
- T03/T04 establish architecture backing US-060..US-062 (observability), US-080 (IaC)
- T05/T06 define data layer for US-001, US-002, US-010..US-014, US-020..US-021, US-030, US-050..US-052

### Gate Conditions

- [ ] `make spec-validate` passes (OpenAPI spec is valid)
- [ ] All `docs/product/` files exist and are non-empty (backlog.md, mvp-scope.md, acceptance-criteria.md, kpis.md)
- [ ] All `docs/adr/` ADR files exist (ADR-001 through ADR-010)
- [ ] `docs/architecture/data-model.md` and `docs/architecture/iam-matrix.md` exist
- [ ] All `docs/db/` files exist (6 files)
- [ ] `plan/sprint-plan.md`, `plan/dependency-graph.md`, `plan/decisions-log.md` exist
- [ ] DBA confirms no full-table-scan patterns in DynamoDB access patterns

### Risk Factors

- OpenAPI spec complexity may require iteration between architect and PM
- Data model decisions affect every downstream sprint; errors here cascade

---

## Sprint 1 -- Infrastructure + Security + Design

**Goal:** Build all infrastructure scaffolding, security documentation, and design artifacts; still zero application code.

**Duration estimate:** 2-3 sessions

### Tasks

| ID | Agent | Description | Inputs | Outputs | Effort |
|---|---|---|---|---|---|
| S1-T01 | aws-specialist | Lambda config, API Gateway limits | S0 outputs | `docs/aws/{lambda-config,apigw-config}.md` | M |
| S1-T02 | aws-specialist | CloudFront, WAF, Cognito config | S0 outputs | `docs/aws/{cloudfront-config,waf-config,cognito-config}.md` | M |
| S1-T03 | aws-specialist | IAM policies, cost optimization, Well-Architected review | S0 outputs | `docs/aws/{iam-policies,cost-optimization,well-architected}.md` | M |
| S1-T04 | security-engineer | STRIDE threat model, security architecture | S0 outputs, `requirements-init.md` section 13 | `docs/security/{threat-model,security-architecture}.md` | L |
| S1-T05 | security-engineer | Secrets mgmt, GDPR/privacy, scanning config, security tests | S1-T04 outputs | `docs/security/{secrets-management,privacy,scanning,security-tests}.md` | M |
| S1-T06 | devops | Docker Compose files, .env.example, Air configs | S1-T01 | `docker-compose.yml`, `docker-compose.infra.yml`, `.env.example`, `.air.*.toml` | L |
| S1-T07 | devops | Terraform modules (9 modules: lambda, dynamodb, cognito, cloudfront, waf, elasticache, sqs, monitoring, apigateway) | S1-T01..T03 | `deploy/terraform/modules/*/` | XL |
| S1-T08 | devops | Terraform environments (dev/prod) + GitHub Actions CI/CD | S1-T07 | `deploy/terraform/environments/`, `.github/workflows/` | L |
| S1-T09 | designer | Wireframes, design system, user flows, accessibility | S0 outputs | `docs/design/{wireframes,design-system,user-flows,accessibility}.md` | M |

### Parallelization

- **Wave 1:** T01 || T02 || T03 || T04 || T09 (all depend only on S0 gate)
- **Wave 2:** T05 (after T04), T06 (after T01), T07 (after T01+T02+T03)
- **Wave 3:** T08 (after T07)

### Backlog Coverage

- T01-T03: infrastructure backing for US-080 (Terraform IaC)
- T04-T05: security foundation for US-070 (Safe Browsing), US-071 (CSRF), US-072 (security headers)
- T06: US-082 (local dev environment)
- T07-T08: US-080 (Terraform IaC), US-081 (CI/CD pipeline)
- T09: design for user dashboard (US-020..US-024), password form (US-013)

### Gate Conditions

- [ ] `docker-compose.yml` and `docker-compose.infra.yml` exist
- [ ] 9 Terraform module directories exist under `deploy/terraform/modules/`
- [ ] `.github/workflows/ci.yml` (or equivalent) exists
- [ ] All `docs/aws/` files exist (7 files)
- [ ] All `docs/security/` files exist (6 files)
- [ ] All `docs/design/` files exist (4 files)
- [ ] `.env.example`, `.air.api.toml`, `.air.redirect.toml` exist

### Risk Factors

- Terraform modules for 9 services is the largest single task (XL); may need to split across sessions
- Security review findings could require backtracking to S0 architecture decisions

---

## Sprint 2 -- BDD & E2E Red State

**Goal:** Write all BDD feature files, step definitions, and E2E test skeletons; confirm they fail (red).

**Duration estimate:** 1-2 sessions

### Tasks

| ID | Agent | Description | Inputs | Outputs | Effort |
|---|---|---|---|---|---|
| S2-T01 | go-developer | Go module init, project skeleton (`cmd/`, `internal/`, `pkg/`) | S1 outputs | `go.mod`, `cmd/*/main.go` (stubs), `pkg/apierr/errors.go` | M |
| S2-T02 | qa-automation | BDD features: redirect, create_link, rate_limit | `docs/product/backlog.md`, `docs/api/openapi.yaml` | `tests/bdd/features/{redirect,create_link,rate_limit}.feature` | M |
| S2-T03 | qa-automation | BDD features: password_link, ttl_expiry, stats | `docs/product/backlog.md`, `docs/api/openapi.yaml` | `tests/bdd/features/{password_link,ttl_expiry,stats}.feature` | M |
| S2-T04 | qa-automation | BDD step definitions + E2E test skeletons | S2-T02, S2-T03 | `tests/bdd/steps/*.go`, `tests/e2e/*_test.go` | L |
| S2-T05 | qa-automation | Test plan, quality gates doc, k6 load scripts | `docs/product/mvp-scope.md` | `docs/qa/`, `tests/load/*.js` | M |

### Parallelization

- **Wave 1:** T01 (skeleton must exist first)
- **Wave 2:** T02 || T03 || T05 (all after T01)
- **Wave 3:** T04 (after T02+T03)

### Backlog Coverage

- T02: US-001 (create anonymous), US-010 (redirect), US-015 (404), US-050 (rate limit redirects), US-051 (rate limit creation)
- T03: US-011 (TTL expiry), US-012 (click expiry), US-013 (password), US-030 (stats)
- T04: step definitions wire BDD to code for all Must Have user stories
- T05: load test scripts validate US-050 rate limits and p99 < 100ms NFR

### Gate Conditions

- [ ] 6 `.feature` files exist in `tests/bdd/features/`
- [ ] `go build ./...` compiles without errors
- [ ] BDD tests FAIL (red state confirmed -- scenarios exist but step implementations return "pending")
- [ ] k6 load test scripts exist (`tests/load/{baseline,stress,spike,soak}.js`)
- [ ] `docs/qa/test-plan.md` exists

### Risk Factors

- Step definitions need careful design to be reusable across features
- Feature files must be precise enough to serve as acceptance criteria (map 1:1 with backlog ACs)

---

## Sprint 3 -- Core Implementation

**Goal:** Implement redirect Lambda, API CRUD Lambda, store, cache, rate limiter, and shortener; make BDD tests go green.

**Duration estimate:** 3-4 sessions (critical path, largest implementation sprint)

### Tasks

| ID | Agent | Description | Inputs | Outputs | Effort |
|---|---|---|---|---|---|
| S3-T01 | go-developer | `make spec-gen` + `internal/store` (DynamoDB repo) + `internal/cache` (Redis adapter) | `docs/api/openapi.yaml`, `docs/db/*` | `internal/api/generated/`, `internal/store/`, `internal/cache/` | L |
| S3-T02 | go-developer | `internal/shortener` (Base62 + collision retry + exponential backoff) + `internal/validator` (URL validation) | S3-T01 | `internal/shortener/`, `internal/validator/` | M |
| S3-T03 | go-developer | `internal/ratelimit` (Redis Lua sliding window, token bucket) | S3-T01 | `internal/ratelimit/` | M |
| S3-T04 | go-developer | `cmd/redirect` Lambda (cache-first lookup, TTL checks, async SQS click events, password flow) | S3-T01..T03 | `cmd/redirect/main.go` (full implementation) | L |
| S3-T05 | go-developer | `cmd/api` Lambda (CRUD via oapi-codegen ServerInterface, anonymous shorten endpoint) | S3-T01..T03 | `cmd/api/main.go` (full implementation) | L |
| S3-T06 | go-developer | `internal/telemetry` (OTel) + `internal/geo` (GeoIP) + mocks + iterate until BDD green | S3-T04, S3-T05 | `internal/telemetry/`, `internal/geo/`, `internal/mocks/` | M |

### Parallelization

- **Wave 1:** T01 (foundation -- store + cache + generated code)
- **Wave 2:** T02 || T03 (both depend only on T01)
- **Wave 3:** T04 || T05 (both depend on T01+T02+T03)
- **Wave 4:** T06 (after T04+T05, integrates everything)

### Backlog Coverage

- T01: data access layer for US-001, US-002, US-010..US-015, US-020..US-023
- T02: code generation for US-001 (Base62), US-003 (custom alias validation)
- T03: rate limiting for US-050, US-051, US-052, US-053
- T04: redirect flow for US-010, US-011, US-012, US-013, US-014, US-015
- T05: API endpoints for US-001, US-002, US-003, US-020..US-024
- T06: observability for US-060 (structured logging), US-061 (tracing)

### Gate Conditions

- [ ] `make bdd` passes (all BDD scenarios GREEN)
- [ ] `make test` passes (unit tests with race detector)
- [ ] `make test-integration` passes (against LocalStack)
- [ ] Code coverage >= 80%
- [ ] `make build` produces 3 Lambda zips (`cmd/redirect`, `cmd/api`, `cmd/worker`)

### Risk Factors

- **Critical path sprint** -- longest and most complex; token exhaustion risk is highest here
- DynamoDB conditional expressions for click-count limit (US-012) require careful testing
- Redis Lua script for sliding window rate limiter is error-prone
- Redirect p99 < 100ms target must be validated with benchmarks during this sprint

---

## Sprint 4 -- Review + Observability

**Goal:** Code review Sprint 3 output, set up SRE observability stack, run performance analysis; fix any blockers.

**Duration estimate:** 1-2 sessions

### Tasks

| ID | Agent | Description | Inputs | Outputs | Effort |
|---|---|---|---|---|---|
| S4-T01 | code-reviewer | Review all Sprint 3 code for correctness, security, performance, Go idioms | Sprint 3 source code | `docs/review/review-sprint-3.md` | L |
| S4-T02 | sre | SLO/SLI definitions, incident response procedures | `requirements-init.md` NFRs | `docs/sre/{slo,incident-response}.md` | M |
| S4-T03 | sre | Grafana dashboards (5), Prometheus alerts, runbooks | S4-T02 | `config/grafana/dashboards/`, `config/prometheus/alerts.yml`, `docs/sre/runbooks/` | L |
| S4-T04 | performance-engineer | Critical path analysis, benchmarks, allocation profiling, Lambda sizing | Sprint 3 source code | `docs/performance/{critical-path,benchmarks,allocations,lambda-sizing}.md` | M |
| S4-T05 | performance-engineer | Redis/DynamoDB/CloudFront perf analysis, load test guide | S4-T04 | `docs/performance/{redis,dynamodb,cloudfront-cache,load-test-guide}.md` | M |
| S4-T06 | go-developer | **CONDITIONAL:** Fix BLOCKER findings from code review | S4-T01 (if blockers exist) | code fixes | S-L |

### Parallelization

- **Wave 1:** T01 || T02 || T04 (all depend only on S3 gate)
- **Wave 2:** T03 (after T02), T05 (after T04), T06 (after T01, only if blockers)

### Backlog Coverage

- T02-T03: US-060 (structured logging validation), US-061 (tracing validation), US-062 (Grafana dashboards)
- T04-T05: NFR validation -- p99 < 100ms, 10K RPS capacity analysis

### Gate Conditions

- [ ] Zero BLOCKER findings in code review (or all fixed by T06)
- [ ] 5 Grafana dashboard JSON files exist in `config/grafana/dashboards/`
- [ ] `config/prometheus/alerts.yml` exists
- [ ] All `docs/performance/` files exist (7 files)
- [ ] All `docs/sre/` files exist (slo.md, incident-response.md, runbooks/)
- [ ] `make test-all` still passes after any S4-T06 fixes

### Risk Factors

- Blocker findings from review may require significant rework, extending this sprint
- Performance benchmarks may reveal p99 violation, requiring optimization before proceeding

---

## Sprint 5 -- Stats, Auth, Worker

**Goal:** Implement SQS worker Lambda, authentication middleware, and statistics API; complete all remaining Must Have user stories.

**Duration estimate:** 2-3 sessions

### Tasks

| ID | Agent | Description | Inputs | Outputs | Effort |
|---|---|---|---|---|---|
| S5-T01 | go-developer | `cmd/worker` Lambda (SQS FIFO batch consumer -> clicks table) | S4 gate outputs | `cmd/worker/main.go` (full implementation) | M |
| S5-T02 | go-developer | `internal/auth` (JWT validation + Cognito middleware + Lambda authorizer) | S4 gate outputs | `internal/auth/` | L |
| S5-T03 | go-developer | Stats API endpoints (aggregate, timeline, geo, referrers) + performance optimizations | S5-T01, S5-T02 | stats handlers in `cmd/api/`, store query methods | L |
| S5-T04 | qa-automation | Integration tests for stats + security test cases (SEC-001..010) | S5-T03 | `tests/integration/`, updated BDD steps | M |
| S5-T05 | code-reviewer | Review Sprint 5 code | S5-T03, S5-T04 | `docs/review/review-sprint-5.md` | M |
| S5-T06 | go-developer | **CONDITIONAL:** Fix BLOCKER findings | S5-T05 (if blockers exist) | code fixes | S-L |

### Parallelization

- **Wave 1:** T01 || T02 (both depend only on S4 gate)
- **Wave 2:** T03 (after T01+T02)
- **Wave 3:** T04 (after T03)
- **Wave 4:** T05 (after T03+T04)
- **Wave 5:** T06 (after T05, conditional)

### Backlog Coverage

- T01: US-014 (click event recording -- worker side)
- T02: US-040 (email/password auth), US-041 (Google SSO), US-043 (JWT refresh)
- T03: US-030 (aggregate stats), US-031 (timeline), US-032 (geo), US-033 (referrers), US-024 (quota display)
- T04: validates US-030..US-033, US-070 (URL validation), US-071 (CSRF), US-072 (security headers)

### Gate Conditions

- [ ] `make test-all` passes (unit + BDD + integration + E2E)
- [ ] Zero BLOCKER findings in review (or all fixed by T06)
- [ ] Worker Lambda processes SQS messages correctly (integration test)
- [ ] Auth middleware correctly validates JWTs (integration test)
- [ ] Stats endpoints return expected aggregations (BDD green)

### Risk Factors

- Cognito integration complexity -- local testing requires careful mocking
- Stats queries on DynamoDB clicks table may be slow without proper GSI design (depends on S0-T05 quality)

---

## Sprint 6 -- Hardening & Launch Readiness

**Goal:** Load testing, security scanning, production Terraform, capacity planning, and final verification that all requirements are met.

**Duration estimate:** 2-3 sessions

### Tasks

| ID | Agent | Description | Inputs | Outputs | Effort |
|---|---|---|---|---|---|
| S6-T01 | qa-automation | Run k6 load tests (baseline 1K RPS, stress 10K RPS, spike 5K RPS, soak 500 RPS x 30min) | `tests/load/*.js` | `tests/load/results/`, `docs/qa/load-test-report.md` | L |
| S6-T02 | qa-automation | Security scans: OWASP ZAP + gosec + govulncheck | source code | `docs/qa/security-scan-report.md` | M |
| S6-T03 | performance-engineer | Interpret load test results, bottleneck analysis, optimization recommendations | S6-T01 results | `docs/performance/load-test-analysis.md` | M |
| S6-T04 | sre | Capacity planning, chaos experiment design, runbook updates | S6-T01, S6-T03 | `docs/sre/{capacity-planning,chaos-experiments}.md` | M |
| S6-T05 | devops | Production Terraform validation, canary pipeline, deploy scripts | S5 gate | `deploy/terraform/environments/prod/`, `deploy/scripts/deploy.sh` | L |
| S6-T06 | devops | Seed script, migration script | S6-T05 | `deploy/scripts/seed/main.go`, `deploy/scripts/migrate/main.go` | M |
| S6-T07 | planner | Final gate review: requirements coverage, all CI gates, launch checklist | ALL previous outputs | `plan/final-review.md` | M |

### Parallelization

- **Wave 1:** T01 || T02 || T05 (all depend only on S5 gate)
- **Wave 2:** T03 (after T01), T06 (after T05)
- **Wave 3:** T04 (after T01+T03)
- **Wave 4:** T07 (after ALL)

### Backlog Coverage

- T01: validates NFR targets (p99 < 100ms @ 1K RPS, capacity to 10K RPS)
- T02: validates US-070 (URL validation/Safe Browsing), US-071 (CSRF), US-072 (security headers)
- T05-T06: US-080 (Terraform IaC -- production), US-081 (CI/CD -- canary pipeline)

### Gate Conditions (Final Launch Gate)

- [ ] `make test-all` passes
- [ ] `make check` passes (fmt + vet + lint + security scan)
- [ ] `make build` produces 3 Lambda zips
- [ ] Redirect p99 <= 100 ms at 1,000 RPS (k6 baseline test)
- [ ] Zero CRITICAL/HIGH findings from gosec/govulncheck
- [ ] All 6 BDD feature files pass (all scenarios green)
- [ ] All Must Have user stories (P0) have corresponding passing tests
- [ ] All documentation artifacts exist per project structure
- [ ] Production Terraform plan is clean (`make tf-plan-prod` succeeds)

### Risk Factors

- Load test infrastructure (k6 + LocalStack) may not accurately represent AWS performance
- gosec findings may require code changes that ripple back through earlier sprints
- Production Terraform may surface resource limits or permission issues not caught in dev

---

## Backlog-to-Sprint Mapping Summary

| User Story | Sprint | Task(s) | Priority |
|---|---|---|---|
| US-001 (anonymous create) | S3 | T01, T02, T05 | P0 |
| US-002 (auth create) | S3, S5 | S3-T05, S5-T02 | P0 |
| US-003 (custom alias) | S3 | T02, T05 | P1 |
| US-004 (UTM params) | S3 | T05 | P2 |
| US-010 (redirect) | S3 | T01, T04 | P0 |
| US-011 (time TTL) | S3 | T04 | P0 |
| US-012 (click TTL) | S3 | T04 | P0 |
| US-013 (password) | S3 | T04 | P1 |
| US-014 (click events) | S3, S5 | S3-T04, S5-T01 | P0 |
| US-015 (404) | S3 | T04 | P0 |
| US-020 (list links) | S3 | T05 | P0 |
| US-021 (link details) | S3 | T05 | P0 |
| US-022 (update link) | S3 | T05 | P1 |
| US-023 (delete link) | S3 | T05 | P1 |
| US-024 (quota display) | S5 | T03 | P1 |
| US-030 (aggregate stats) | S5 | T03 | P0 |
| US-031 (timeline) | S5 | T03 | P1 |
| US-032 (geo breakdown) | S5 | T03 | P1 |
| US-033 (referrer sources) | S5 | T03 | P1 |
| US-040 (email/password auth) | S5 | T02 | P0 |
| US-041 (Google SSO) | S5 | T02 | P0 |
| US-043 (JWT refresh) | S5 | T02 | P0 |
| US-050 (rate limit redirect) | S3 | T03, T04 | P0 |
| US-051 (rate limit creation) | S3 | T03, T05 | P0 |
| US-052 (user quotas) | S3, S5 | S3-T05, S5-T02 | P0 |
| US-053 (password rate limit) | S3 | T03, T04 | P1 |
| US-060 (structured logging) | S3 | T06 | P0 |
| US-061 (distributed tracing) | S3 | T06 | P1 |
| US-062 (Grafana dashboards) | S4 | T03 | P1 |
| US-070 (URL validation) | S3, S6 | S3-T02, S6-T02 | P1 |
| US-071 (CSRF) | S3 | T04 | P0 |
| US-072 (security headers) | S3 | T04, T05 | P1 |
| US-080 (Terraform IaC) | S1, S6 | S1-T07/T08, S6-T05 | P0 |
| US-081 (CI/CD) | S1 | T08 | P0 |
| US-082 (local dev env) | S1 | T06 | P0 |
