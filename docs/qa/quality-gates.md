# Shorty Quality Gates

**Version:** 1.0
**Date:** 2026-04-05
**Author:** QA Automation Engineer (S2-T05)
**Status:** Active

---

## Overview

Quality gates are mandatory checkpoints in the CI/CD pipeline. Each gate must pass before the pipeline advances. Gates are ordered by cost (cheapest/fastest first) to provide fast feedback.

---

## Gate Definitions

### Gate 1: Spec Validity

| Attribute | Value |
|---|---|
| Stage | PR |
| Command | `make spec-validate` |
| Pass criteria | Exit code 0; OpenAPI spec is syntactically and semantically valid |
| Blocks | Everything -- no other gate runs if the spec is invalid |
| Timeout | 30s |

The OpenAPI spec (`docs/api/openapi.yaml`) is the single source of truth. If the spec is broken, all generated stubs and contract tests are invalid. This gate runs first and blocks all subsequent gates.

### Gate 2: Code Quality

| Attribute | Value |
|---|---|
| Stage | PR |
| Command | `make check` |
| Pass criteria | Exit code 0; all sub-checks pass |
| Blocks | Merge to any branch |
| Timeout | 5m |

Sub-checks executed by `make check`:

| Check | Tool | Criteria |
|---|---|---|
| Format | `gofmt -l` | No unformatted files |
| Vet | `go vet ./...` | 0 findings |
| Lint | `golangci-lint run` | 0 errors (warnings allowed per `.golangci.yml`) |
| Security scan | `gosec ./...` | 0 HIGH or CRITICAL findings |

### Gate 3: Unit Tests

| Attribute | Value |
|---|---|
| Stage | PR |
| Command | `make test` |
| Pass criteria | Exit code 0; coverage >= 80% overall; race detector passes |
| Blocks | Merge to any branch |
| Timeout | 10m |

Coverage thresholds enforced per critical package:

| Package | Minimum Coverage |
|---|---|
| Overall | 80% |
| `internal/shortener` | 90% |
| `internal/ratelimit` | 90% |
| `internal/auth` | 90% |
| `internal/validator` | 90% |

### Gate 4: BDD Tests

| Attribute | Value |
|---|---|
| Stage | PR |
| Command | `make bdd` |
| Pass criteria | All Gherkin scenarios pass (exit code 0) |
| Blocks | Merge to any branch |
| Timeout | 10m |

Feature files: `redirect`, `create_link`, `rate_limit`, `password_link`, `ttl_expiry`, `stats`.

### Gate 5: Integration Tests

| Attribute | Value |
|---|---|
| Stage | PR |
| Command | `make test-integration` |
| Pass criteria | Exit code 0; all tests pass |
| Blocks | Deploy to any environment |
| Timeout | 15m |
| Dependencies | LocalStack, Redis (CI service containers) |

### Gate 6: Build

| Attribute | Value |
|---|---|
| Stage | PR |
| Command | `make build` |
| Pass criteria | 3 Lambda ZIP artifacts produced (`redirect.zip`, `api.zip`, `worker.zip`); each < 50MB; target `GOOS=linux GOARCH=arm64 CGO_ENABLED=0` |
| Blocks | Deploy to any environment |
| Timeout | 5m |

### Gate 7: E2E Tests

| Attribute | Value |
|---|---|
| Stage | Post-merge |
| Command | `make test-e2e` |
| Pass criteria | Exit code 0; all tests pass against dev AWS environment |
| Blocks | Promote to production |
| Timeout | 15m |
| Dependencies | Dev AWS environment deployed |

### Gate 8: Load Baseline

| Attribute | Value |
|---|---|
| Stage | Post-merge |
| Command | `make test-load` |
| Pass criteria | See thresholds below |
| Blocks | Promote to production |
| Timeout | 10m |

Load test thresholds:

| Metric | Threshold |
|---|---|
| Redirect p99 latency | <= 100ms at 1,000 RPS |
| Create p99 latency | <= 300ms |
| Error rate | < 0.1% |
| Redirect p50 latency | <= 20ms |

### Gate 9: Security Scan

| Attribute | Value |
|---|---|
| Stage | Release (tag) |
| Command | `gosec ./...` + `govulncheck ./...` |
| Pass criteria | 0 HIGH or CRITICAL findings from gosec; 0 known vulnerabilities from govulncheck |
| Blocks | Release to production |
| Timeout | 10m |

---

## Gate Summary Matrix

| # | Gate | Command | Pass Criteria | Blocks | Stage |
|---|---|---|---|---|---|
| 1 | Spec Valid | `make spec-validate` | exit 0 | everything | PR |
| 2 | Code Quality | `make check` | exit 0, 0 HIGH from gosec | merge | PR |
| 3 | Unit Tests | `make test` | exit 0, coverage >= 80%, race clean | merge | PR |
| 4 | BDD | `make bdd` | all scenarios green | merge | PR |
| 5 | Integration | `make test-integration` | exit 0 | deploy | PR |
| 6 | Build | `make build` | 3 zips produced | deploy | PR |
| 7 | E2E | `make test-e2e` | exit 0 | promote | post-merge |
| 8 | Load Baseline | `make test-load` | p99 <= 100ms redirect | promote | post-merge |
| 9 | Security | gosec + govulncheck | 0 HIGH findings | release | tag |

---

## Pipeline Flow

```
PR opened/updated
    |
    v
[Gate 1: Spec Valid] --fail--> BLOCK all
    |
    v (pass)
[Gate 2: Code Quality] ---|
[Gate 3: Unit Tests]   ---|--parallel--> any fail --> BLOCK merge
[Gate 4: BDD]          ---|
    |
    v (all pass)
[Gate 5: Integration] --fail--> BLOCK merge
    |
    v (pass)
[Gate 6: Build] --fail--> BLOCK merge
    |
    v (pass)
PR approved + merged to main
    |
    v
Deploy to dev (Terraform + Lambda)
    |
    v
[Gate 7: E2E] --fail--> BLOCK promotion, alert on-call
    |
    v (pass)
[Gate 8: Load Baseline] --fail--> BLOCK promotion, perf review
    |
    v (pass)
Ready for release
    |
    v
Tag vX.Y.Z
    |
    v
[Gate 9: Security] --fail--> BLOCK release
    |
    v (pass)
Deploy to prod (canary 10% -> observe 10min -> promote 100%)
```

---

## Failure Response

| Gate Failure | Response | SLA |
|---|---|---|
| Spec Valid | PR author fixes spec | Before merge |
| Code Quality | PR author fixes lint/format/security issues | Before merge |
| Unit Tests | PR author fixes tests or code | Before merge |
| BDD | PR author investigates; may need to update implementation | Before merge |
| Integration | PR author + infra team investigate | Before merge |
| Build | PR author fixes build errors | Before merge |
| E2E | On-call engineer investigates; rollback if needed | 1 hour |
| Load Baseline | Performance review; block promotion until resolved | 4 hours |
| Security | Security team reviews; Critical blocks release immediately | 24 hours |

---

## Nightly Gates

A nightly CI job runs the extended test suite:

| Gate | Command | Purpose |
|---|---|---|
| Full unit + BDD | `make test && make bdd` | Catch flaky tests |
| Integration | `make test-integration` | Verify infrastructure compatibility |
| Stress load | `k6 run tests/load/stress.js` | Validate capacity headroom |
| Soak load | `k6 run tests/load/soak.js` | Detect memory leaks, pool exhaustion |
| Full security | `go test ./tests/security/... -v` | Dynamic security validation |

Nightly failures trigger a Slack notification to the `#shorty-ci` channel.
