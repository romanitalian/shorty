# Shorty Test Plan

**Version:** 1.0
**Date:** 2026-04-05
**Author:** QA Automation Engineer (S2-T05)
**Status:** Active

---

## 1. Scope

This test plan covers all testing activities for the Shorty URL shortener service across six test levels: unit, BDD, integration, E2E, load, and security. It defines environments, tooling, data management, coverage targets, naming conventions, and CI pipeline integration.

---

## 2. Test Levels

### 2.1 Unit Tests

| Attribute | Value |
|---|---|
| Tool | `go test` + `testify` + race detector |
| Location | `*_test.go` files alongside source in `internal/`, `pkg/`, `cmd/` |
| Command | `make test` |
| Runs in CI | Every PR, every push |
| Coverage target | 80% overall, 90% for critical paths (`internal/shortener`, `internal/ratelimit`, `internal/auth`) |

Unit tests verify individual functions and methods in isolation. External dependencies (DynamoDB, Redis, SQS) are mocked using interfaces and `internal/mocks/` (generated via mockery).

### 2.2 BDD Tests

| Attribute | Value |
|---|---|
| Tool | godog (Gherkin + Go step definitions) |
| Location | `tests/bdd/features/*.feature`, `tests/bdd/steps/*.go` |
| Command | `make bdd` |
| Single feature | `make bdd-feature FEATURE=redirect` |
| Runs in CI | Every PR |

BDD scenarios are the executable acceptance criteria. Feature files are written BEFORE implementation (red state) and must not be modified after Sprint 2 is complete. Step definitions make HTTP calls against `http://localhost:8080` (API) and `http://localhost:8081` (redirect).

Feature files:
- `redirect.feature` -- redirect flows, cache behavior, expired/deactivated links
- `create_link.feature` -- authenticated and anonymous link creation
- `rate_limit.feature` -- IP and user quota enforcement
- `password_link.feature` -- password-protected link flows, brute-force lockout
- `ttl_expiry.feature` -- time-based and click-based TTL
- `stats.feature` -- click statistics, timeline, geo, referrers

### 2.3 Integration Tests

| Attribute | Value |
|---|---|
| Tool | `go test` with build tag `integration` |
| Location | `tests/integration/` |
| Command | `make test-integration` |
| Dependencies | LocalStack (DynamoDB, SQS, S3, SecretsManager), Redis |
| Runs in CI | Every PR |

Integration tests verify cross-component behavior against real (emulated) AWS services. They cover:
- Full redirect flow (cache miss -> DynamoDB -> cache populate -> cache hit)
- TTL enforcement (time-based and click-based)
- Password-protected link end-to-end
- Rate limiter with real Redis (sliding window Lua script)
- Anonymous quota enforcement
- Concurrent code creation (20 goroutines, exactly 1 success with 201, rest get 409)
- SQS click event publishing and worker consumption

### 2.4 E2E Tests

| Attribute | Value |
|---|---|
| Tool | `go test` with build tag `e2e` |
| Location | `tests/e2e/` |
| Command | `make test-e2e` |
| Target | Dev AWS environment |
| Runs in CI | After merge to main + deploy to dev |

E2E tests exercise the full production-like stack (API Gateway -> Lambda -> DynamoDB/Redis/SQS). Test flow: create link via API -> redirect via GET -> verify click in stats. E2E skeletons are committed in Sprint 2 with `t.Skip`; full implementation in Sprint 5.

### 2.5 Load Tests

| Attribute | Value |
|---|---|
| Tool | k6 |
| Location | `tests/load/` |
| Command | `make test-load` |
| Runs in CI | After merge to main + deploy to dev |

Load test scenarios:
- `baseline.js` -- 1,000 RPS for 5 min; p99 redirect <= 100ms, p99 create <= 300ms
- `stress.js` -- ramp 0 to 10,000 RPS over 2 min, hold 5 min; find breaking point
- `spike.js` -- instant 5,000 RPS surge, hold 1 min, drop to 500 RPS; verify recovery
- `soak.js` -- 500 RPS for 30 min; track memory leaks and connection pool exhaustion

### 2.6 Security Tests

| Attribute | Value |
|---|---|
| Tool | `gosec`, `govulncheck`, Go integration tests |
| Location | `tests/security/` |
| Command | `gosec ./...` + `govulncheck ./...` + `go test ./tests/security/...` |
| Runs in CI | Every PR (static), post-deploy (dynamic) |

Security tests cover SEC-001 through SEC-010 as documented in `docs/security/security-tests.md`:
- Input validation (XSS, SSRF)
- Authentication (missing JWT, expired JWT, algorithm confusion)
- Rate limiting enforcement
- CSRF protection
- Brute-force protection
- Authorization (cross-user access)
- Data leak prevention (no raw IP)

---

## 3. Test Environment Setup

### 3.1 Local Development

```bash
make dev-up          # Start LocalStack + Redis + Grafana + Jaeger
make seed-local      # Populate DynamoDB with test data
make run-api         # API on http://localhost:8080
make run-redirect    # Redirect on http://localhost:8081
```

Docker Compose services:
- **LocalStack** -- DynamoDB, SQS, S3, SecretsManager, IAM (port 4566)
- **Redis** -- rate limiter, cache, session (port 6379)
- **Grafana** -- dashboards (port 3000)
- **Jaeger** -- distributed tracing (port 16686)
- **Prometheus** -- metrics scraping (port 9090)
- **Loki** -- log aggregation (port 3100)

### 3.2 CI Environment

GitHub Actions runners use service containers:
- `localstack/localstack:3.0` -- AWS service emulation
- `redis:7-alpine` -- Redis

Environment variables:
- `SHORTY_API_URL` -- API base URL (default: `http://localhost:8080`)
- `SHORTY_REDIRECT_URL` -- Redirect base URL (default: `http://localhost:8081`)
- `AWS_ENDPOINT_URL` -- LocalStack endpoint (default: `http://localhost:4566`)
- `REDIS_URL` -- Redis connection string (default: `redis://localhost:6379`)

---

## 4. Test Data Management

### 4.1 Seed Scripts

| Script | Purpose | Command |
|---|---|---|
| `deploy/scripts/seed/main.go` | Populate local DynamoDB with fixture data | `make seed-local` |

Seed data includes:
- 10 active links with known codes (`test001` through `test010`)
- 2 expired links (time-based TTL)
- 2 click-exhausted links
- 2 password-protected links (password: `test-password`)
- 2 test users (free plan, pro plan)
- 50 click events distributed across links

### 4.2 Fixtures

Test fixtures are defined as Go constants/variables in `tests/testdata/` or inline in test files. BDD step definitions create their own test data via API calls before each scenario.

### 4.3 Cleanup

- Each integration test cleans up its own data in `t.Cleanup()`.
- BDD scenarios use Background steps to ensure clean state.
- LocalStack data is ephemeral -- `make dev-down && make dev-up` resets everything.

---

## 5. Coverage Targets

| Package | Target | Rationale |
|---|---|---|
| **Overall** | >= 80% | Project baseline |
| `internal/shortener` | >= 90% | Critical path: code generation, collision handling |
| `internal/ratelimit` | >= 90% | Critical path: abuse prevention |
| `internal/auth` | >= 90% | Critical path: security boundary |
| `internal/store` | >= 85% | Data layer, many edge cases |
| `internal/cache` | >= 85% | Cache logic affects redirect latency |
| `internal/validator` | >= 90% | Input validation, security-sensitive |
| `cmd/redirect` | >= 85% | Hot path, performance-sensitive |
| `cmd/api` | >= 80% | CRUD, generated stubs |
| `cmd/worker` | >= 80% | Batch processing |
| `pkg/apierr` | >= 90% | Shared error handling |

Coverage is measured with `go test -coverprofile` and enforced in CI. HTML report: `make coverage` -> `coverage.html`.

---

## 6. Test Naming Conventions

### 6.1 Unit Tests

```
TestFunctionName_Scenario_ExpectedResult
```

Examples:
- `TestGenerateCode_ValidLength_Returns7Chars`
- `TestGenerateCode_CollisionRetry_SucceedsAfterBackoff`
- `TestValidateURL_JavascriptScheme_ReturnsError`

### 6.2 Integration Tests

```
TestIntegration_Feature_Scenario
```

Examples:
- `TestIntegration_RedirectFlow_CacheHit`
- `TestIntegration_TTLByTime_ExpiredReturns410`
- `TestIntegration_ConcurrentCodeCreation_OneSucceeds`

### 6.3 E2E Tests

```
TestE2E_UserJourney
```

Examples:
- `TestE2E_CreateAndRedirect`
- `TestE2E_PasswordProtectedFlow`
- `TestE2E_AnonymousQuotaEnforcement`

### 6.4 Security Tests

```
TestSECxxx_Description
```

Examples:
- `TestSEC001_JavascriptSchemeRejected`
- `TestSEC006_RateLimitExceeded`

### 6.5 BDD Feature Files

Feature file names use snake_case matching the domain concept: `redirect.feature`, `create_link.feature`, `rate_limit.feature`.

Scenario names are descriptive sentences: "Redirect to active link", "Anonymous user exceeds daily quota".

---

## 7. CI Pipeline Integration

### 7.1 Pull Request Gates (blocking merge)

```
1. make spec-validate          # OpenAPI spec validity
2. make check                  # fmt + vet + lint + gosec
3. make test                   # unit tests + race detector + coverage >= 80%
4. make bdd                    # all BDD scenarios green
5. make test-integration       # integration tests against LocalStack
6. make build                  # 3 Lambda zips (redirect, api, worker)
```

All six gates must pass. Failure at any gate blocks the PR from merging.

### 7.2 Post-Merge to main (blocking deploy)

```
7. deploy to dev               # Terraform + Lambda update
8. make test-e2e               # E2E against dev AWS
9. make test-load              # k6 baseline: p99 redirect <= 100ms at 1,000 RPS
```

### 7.3 Release (tag vX.Y.Z)

```
10. gosec ./... + govulncheck  # 0 HIGH/CRITICAL findings
11. deploy to prod             # approval gate required
12. canary 10% traffic         # 10 min observation
13. promote to 100%            # if error rate < 0.1%
```

### 7.4 Which Tests Run When

| Trigger | spec-validate | check | unit | BDD | integration | build | E2E | load | security |
|---|---|---|---|---|---|---|---|---|---|
| PR opened/updated | Y | Y | Y | Y | Y | Y | - | - | static |
| Merge to main | Y | Y | Y | Y | Y | Y | Y | Y | static |
| Tag vX.Y.Z | Y | Y | Y | Y | Y | Y | Y | Y | full |
| Nightly schedule | - | - | Y | Y | Y | - | Y | stress+soak | full |

---

## 8. Defect Tracking and Regression Policy

### 8.1 Defect Tracking

- All defects are tracked as GitHub Issues with the `bug` label.
- Severity labels: `severity/critical`, `severity/high`, `severity/medium`, `severity/low`.
- Critical and high severity bugs block the current sprint until resolved.
- Each bug fix PR must include a regression test that reproduces the original failure.

### 8.2 Regression Policy

- Every bug fix must add a test case that fails without the fix and passes with it.
- Regression tests are permanently included in the test suite (never deleted).
- The full test suite (`make test-all`) runs nightly to catch regressions.
- Flaky tests are tracked with the `flaky-test` label and must be fixed within one sprint.
- A test that fails intermittently more than 3 times in one week is quarantined and assigned to the next sprint.

### 8.3 Test Failure Escalation

1. **Unit/BDD failure on PR** -- PR author fixes before merge.
2. **Integration failure on PR** -- PR author investigates; may involve infrastructure team.
3. **E2E failure post-merge** -- Triggers Slack alert; on-call engineer investigates within 1 hour.
4. **Load test failure post-merge** -- Blocks promotion to prod; performance review required.
5. **Security scan finding** -- Critical/High blocks release; Medium tracked as tech debt.

---

## 9. Tools and Versions

| Tool | Version | Purpose |
|---|---|---|
| Go | 1.22+ | Language runtime |
| godog | latest | BDD test runner |
| testify | latest | Test assertions |
| mockery | latest | Mock generation |
| k6 | latest | Load testing |
| gosec | latest | Static security analysis |
| govulncheck | latest | Dependency vulnerability scan |
| golangci-lint | latest | Linting (configured in `.golangci.yml`) |
| LocalStack | 3.0 | AWS service emulation |
| Redis | 7-alpine | Cache and rate limiter |
| Docker Compose | 2.x | Local environment orchestration |
