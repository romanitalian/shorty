# Contributing to Shorty

Thank you for your interest in contributing! This document explains the process,
conventions, and quality gates you need to know before opening a PR.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How to Contribute](#how-to-contribute)
- [Engineering Flow](#engineering-flow)
- [Development Setup](#development-setup)
- [Commit Conventions](#commit-conventions)
- [Pull Request Process](#pull-request-process)
- [RFC Process](#rfc-process)
- [Issue Labels](#issue-labels)

---

## Code of Conduct

Be respectful. Harassment, discriminatory language, and personal attacks will not be
tolerated. This project follows the [Contributor Covenant v2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/).

---

## How to Contribute

| What | Where to start |
|---|---|
| Report a bug | [Bug Report issue template](.github/ISSUE_TEMPLATE/bug.yml) |
| Request a feature | [Feature Request issue template](.github/ISSUE_TEMPLATE/feature.yml) |
| Propose a large change | [RFC issue template](.github/ISSUE_TEMPLATE/rfc.yml) + `docs/rfcs/` |
| Security vulnerability | [GitHub Security Advisories](../../security/advisories/new) — **not a public issue** |
| Ask a question | [GitHub Discussions](../../discussions) |

**Before opening a PR**, make sure an issue exists and is assigned to you (or assign yourself).
PRs without a linked issue may be closed without review.

---

## Engineering Flow

Every contribution follows this sequence. Skipping a step is a reason for a PR to be returned:

```
1. SPEC     docs/api/openapi.yaml updated (if API changes)  →  make spec-validate
2. BDD      .feature file updated/added and FAILS (red)     →  make bdd
3. IMPL     code written until BDD is GREEN                 →  make bdd
4. QUALITY  all gates pass                                  →  make check && make test-all
```

**Non-negotiable rules:**
- `docs/api/openapi.yaml` is the single source of truth. Never hand-edit generated files in `internal/api/generated/`.
- BDD `.feature` files must be committed **in the same PR** as or **before** the implementation.
- `make test-all` must pass locally before requesting review.

---

## Development Setup

### Prerequisites

```bash
go 1.23+
docker + docker compose
make
gh (GitHub CLI)       # for the bootstrap script
node 20+ + npm        # for OpenAPI tooling
terraform 1.7+
k6                    # load testing
```

### First-time setup

```bash
git clone https://github.com/romanitalian/shorty
cd shorty
cp .env.example .env          # fill in local values
make install-tools            # installs all Go + npm + brew tools
make dev-up                   # starts LocalStack, Redis, Grafana, Jaeger
make seed-local               # seeds DynamoDB with test data
make run-api                  # API hot reload → http://localhost:8080
```

### Running tests

```bash
make test                     # unit tests (race detector)
make bdd                      # BDD feature tests (godog)
make test-integration         # integration tests (LocalStack must be running)
make test-all                 # full suite
make coverage                 # HTML coverage report
```

### Single test / single BDD feature

```bash
go test ./internal/shortener/... -run TestGenerate -v
make bdd-feature FEATURE=redirect
```

---

## Commit Conventions

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>

[optional body]

[optional footer: Closes #123]
```

**Types:** `feat`, `fix`, `perf`, `refactor`, `test`, `docs`, `infra`, `spec`, `chore`

**Scopes:** `redirect`, `api`, `worker`, `auth`, `ratelimit`, `shortener`, `store`,
`cache`, `telemetry`, `infra`, `ci`, `spec`

**Examples:**
```
feat(redirect): add click-count TTL enforcement
fix(ratelimit): lua script nil check before type assertion (closes #47)
perf(redirect): replace fmt.Sprintf with strings.Builder in key construction
spec(api): add POST /api/v1/links/{code}/qr endpoint
infra(terraform): add elasticache module with security group
```

Breaking changes must include `BREAKING CHANGE:` in the footer:
```
feat(api): rename original_url field to destination_url

BREAKING CHANGE: `original_url` renamed to `destination_url` in all responses.
Migration: update clients to use `destination_url`. Old field removed in v2.0.
```

---

## Pull Request Process

1. **Fork and branch** — branch name: `<type>/<short-description>` e.g. `feat/qr-code-generation`
2. **Link an issue** — every PR must close or reference an issue
3. **Fill in the PR template** — all checklist items must be addressed
4. **One reviewer minimum** — maintainers review within 3 business days
5. **All CI gates must pass** before merge is possible:
   - `spec-validate` — OpenAPI spec valid
   - `check` — fmt + vet + lint + gosec
   - `test` — unit tests with race detector
   - `bdd` — all BDD scenarios green
   - `build` — Linux ARM64 binaries compile

**Merge strategy:** Squash merge. Your commits will be squashed into one.
Write a clean PR title that will become the squash commit message.

---

## RFC Process

Required for: XL-effort features, breaking API/schema changes, new AWS services, toolchain changes.

1. Open an [RFC issue](.github/ISSUE_TEMPLATE/rfc.yml) to gauge interest
2. Once there is consensus, open a PR adding `docs/rfcs/NNNN-short-title.md`
   (use the next available number — see existing files in `docs/rfcs/`)
3. RFC document is discussed in the PR; revisions happen there
4. RFC is merged → status set to `Accepted`
5. Open implementation issues linked to the RFC number
6. Implementation PRs reference both the RFC doc and the issue: `Implements RFC-0002, closes #89`

RFC template: `docs/rfcs/0000-template.md`

---

## Issue Labels

### Type
| Label | Meaning |
|---|---|
| `type:feature` | New feature or enhancement |
| `type:bug` | Something is broken |
| `type:perf` | Performance improvement |
| `type:security` | Security issue or hardening |
| `type:infra` | Terraform / CI / Docker |
| `type:docs` | Documentation |
| `type:test` | Test coverage |
| `type:rfc` | Request for Comments |
| `type:chore` | Maintenance, dependency updates |

### Priority
`priority:critical` · `priority:high` · `priority:medium` · `priority:low`

### Status
`status:triage` · `status:accepted` · `status:in-progress` · `status:blocked` · `status:discussion`

### Component
`component:redirect` · `component:api` · `component:worker` · `component:auth`
`component:ratelimit` · `component:shortener` · `component:observability` · `component:infra`

### Size
`size:xs` · `size:s` · `size:m` · `size:l` · `size:xl`

### Community
`good first issue` · `help wanted` · `breaking change` · `nfr`
