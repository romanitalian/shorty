#!/usr/bin/env bash
# =============================================================================
# github-setup.sh — bootstrap GitHub labels, milestones, and issues for Shorty
#
# Usage:
#   ./deploy/scripts/github-setup.sh [--dry-run] [--repo owner/repo]
#
# Requirements:
#   gh CLI authenticated (gh auth login)
#   Repository must exist on GitHub before running
#
# Idempotent: labels and milestones are skipped if they already exist.
# Issues are created only if no open issue with the same title exists.
# =============================================================================
set -euo pipefail

REPO=""
DRY_RUN=false

# ── Parse args ────────────────────────────────────────────────────────────────
for arg in "$@"; do
  case $arg in
    --dry-run) DRY_RUN=true ;;
    --repo=*)  REPO="${arg#*=}" ;;
    --repo)    shift; REPO="$1" ;;
  esac
done

if [[ -z "$REPO" ]]; then
  REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || echo "")
  if [[ -z "$REPO" ]]; then
    echo "Error: could not detect repo. Run from repo root or pass --repo owner/repo"
    exit 1
  fi
fi

echo "Repository: $REPO"
[[ "$DRY_RUN" == "true" ]] && echo "DRY RUN — no changes will be made"

gh_run() {
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "[dry-run] gh $*"
  else
    gh "$@"
  fi
}

# ── Labels ────────────────────────────────────────────────────────────────────
create_label() {
  local name="$1" color="$2" desc="$3"
  if gh label list --repo "$REPO" --limit 200 | grep -q "^${name}$\|^${name}\t"; then
    echo "  label exists: $name"
  else
    gh_run label create "$name" --repo "$REPO" --color "$color" --description "$desc"
    echo "  created label: $name"
  fi
}

echo ""
echo "── Creating labels ──────────────────────────────────────────────────────"

# Type
create_label "type:feature"    "0075ca" "New feature or enhancement"
create_label "type:bug"        "d73a4a" "Something is broken"
create_label "type:perf"       "e4e669" "Performance improvement"
create_label "type:security"   "b60205" "Security issue or hardening"
create_label "type:infra"      "c5def5" "Terraform / CI / Docker changes"
create_label "type:docs"       "0075ca" "Documentation only"
create_label "type:test"       "bfd4f2" "Test coverage"
create_label "type:rfc"        "d4c5f9" "Request for Comments"
create_label "type:chore"      "fef2c0" "Maintenance, dependency updates"

# Priority
create_label "priority:critical" "b60205" "Must fix immediately"
create_label "priority:high"     "e11d48" "High priority"
create_label "priority:medium"   "f97316" "Medium priority"
create_label "priority:low"      "84cc16" "Low priority"

# Status
create_label "status:triage"      "ededed" "Needs triage"
create_label "status:accepted"    "0e8a16" "Accepted, ready to work on"
create_label "status:in-progress" "fbca04" "Work in progress"
create_label "status:blocked"     "b60205" "Blocked by another issue"
create_label "status:discussion"  "d4c5f9" "Needs discussion before work begins"

# Component
create_label "component:redirect"     "bfd4f2" "Redirect Lambda"
create_label "component:api"          "bfd4f2" "API Lambda"
create_label "component:worker"       "bfd4f2" "SQS worker Lambda"
create_label "component:auth"         "bfd4f2" "Authentication & authorization"
create_label "component:ratelimit"    "bfd4f2" "Rate limiting"
create_label "component:shortener"    "bfd4f2" "Code generation"
create_label "component:observability" "bfd4f2" "Metrics, traces, dashboards"
create_label "component:infra"        "c5def5" "IaC and deployment"
create_label "component:db"           "c5def5" "DynamoDB / Redis"
create_label "component:security"     "b60205" "Security & WAF"

# Size
create_label "size:xs" "ffffff" "A few hours"
create_label "size:s"  "e8f5e9" "~1 day"
create_label "size:m"  "c8e6c9" "2–3 days"
create_label "size:l"  "a5d6a7" "~1 week"
create_label "size:xl" "66bb6a" "Needs RFC first"

# Community
create_label "good first issue" "7057ff" "Good for newcomers"
create_label "help wanted"      "008672" "Extra attention needed"
create_label "breaking change"  "b60205" "Introduces a breaking change"
create_label "nfr"              "e4e669" "Non-functional requirement"

# ── Milestones ────────────────────────────────────────────────────────────────
create_milestone() {
  local title="$1" desc="$2"
  if gh api "repos/$REPO/milestones" --jq '.[].title' | grep -qF "$title"; then
    echo "  milestone exists: $title"
  else
    gh_run api "repos/$REPO/milestones" -X POST \
      -f title="$title" \
      -f description="$desc" \
      -f state="open"
    echo "  created milestone: $title"
  fi
}

echo ""
echo "── Creating milestones ──────────────────────────────────────────────────"

create_milestone "Sprint 0: Foundation"              "PM backlog + Architect OpenAPI spec + DBA schema validation. Gate: make spec-validate passes."
create_milestone "Sprint 1: Infrastructure & Design" "AWS Specialist configs + Security threat model + DevOps Terraform + Designer wireframes."
create_milestone "Sprint 2: BDD & E2E (Red)"         "QA writes all BDD .feature files and E2E skeletons. All must FAIL. No implementation code."
create_milestone "Sprint 3: Core Implementation"     "Go Developer implements redirect + API CRUD + rate limiter until BDD goes GREEN."
create_milestone "Sprint 4: Review & Observability"  "Code review Sprint 3 + SRE dashboards + Performance Engineer critical path analysis."
create_milestone "Sprint 5: Stats, Auth & Worker"    "Click-processor + stats API + Cognito auth. Integration tests. Second code review."
create_milestone "Sprint 6: Hardening & Launch"      "Load tests + security scan + capacity planning + prod deploy + canary validation."

# ── Helper: create issue if title not already open ────────────────────────────
get_milestone_number() {
  gh api "repos/$REPO/milestones" --jq ".[] | select(.title | contains(\"$1\")) | .number"
}

create_issue() {
  local title="$1" body="$2" labels="$3" milestone_fragment="$4"

  if gh issue list --repo "$REPO" --state open --limit 300 \
      --json title -q '.[].title' | grep -qF "$title"; then
    echo "  issue exists: $title"
    return
  fi

  local milestone_num
  milestone_num=$(get_milestone_number "$milestone_fragment")

  if [[ "$DRY_RUN" == "true" ]]; then
    echo "[dry-run] create issue: $title [milestone=$milestone_fragment, labels=$labels]"
  else
    gh issue create \
      --repo "$REPO" \
      --title "$title" \
      --body "$body" \
      --label "$labels" \
      --milestone "$milestone_num"
    echo "  created: $title"
  fi
}

# ── Issues — Sprint 0: Foundation ─────────────────────────────────────────────
echo ""
echo "── Creating Sprint 0 issues ─────────────────────────────────────────────"

create_issue \
  "[pm] Write product backlog, MVP scope, and Given/When/Then acceptance criteria" \
  "**Role:** Product Manager

**Deliverables:**
- \`docs/product/backlog.md\` — User Stories in \"As a / I want / So that\" format
- \`docs/product/mvp-scope.md\` — MoSCoW prioritized MVP boundaries
- \`docs/product/acceptance-criteria.md\` — Given/When/Then per feature (used directly as BDD scenarios)
- \`docs/product/kpis.md\` — DAU, p99 latency, uptime, conversion rate

**Important:** Acceptance criteria must be in Gherkin Given/When/Then format.
QA will copy them verbatim into \`.feature\` files.

See \`requirements-subagents.md\` → PM section for full prompt." \
  "type:docs,status:accepted,size:m" \
  "Sprint 0"

create_issue \
  "[architect] Author OpenAPI 3.0 spec (docs/api/openapi.yaml) — primary deliverable" \
  "**Role:** Architect

**This is the single source of truth.** All Go stubs are generated from it.

**Endpoints to specify:** GET /{code}, POST /p/{code}, POST /api/v1/links, GET /api/v1/links,
GET /api/v1/links/{code}, PATCH /api/v1/links/{code}, DELETE /api/v1/links/{code},
GET /api/v1/links/{code}/stats, GET /api/v1/links/{code}/stats/timeline,
GET /api/v1/links/{code}/stats/geo, GET /api/v1/links/{code}/stats/referrers,
GET /api/v1/me, POST /api/v1/shorten.

Each endpoint: request schema, all response schemas, error codes, security scheme,
rate limit headers (X-RateLimit-Limit, X-RateLimit-Remaining, Retry-After).

**Gate:** \`make spec-validate\` must pass before Sprint 1 begins.

See \`requirements-subagents.md\` → Architect section for full prompt." \
  "type:docs,status:accepted,priority:critical,size:l" \
  "Sprint 0"

create_issue \
  "[architect] Write Architecture Decision Records — ADR-001 through ADR-010" \
  "**Role:** Architect

Minimum 10 ADRs in \`docs/adr/\` covering:
ADR-001 DynamoDB single-table, ADR-002 Redis cache-first, ADR-003 Async SQS click recording,
ADR-004 Lambda SnapStart gap (Go/ARM64), ADR-005 oapi-codegen spec-driven,
ADR-006 Redis Lua sliding window, ADR-007 IP hashing, ADR-008 DynamoDB TTL,
ADR-009 CloudFront+WAF, ADR-010 Cognito SSO.

Format per ADR: Status / Context / Decision / Consequences." \
  "type:docs,status:accepted,size:m" \
  "Sprint 0"

create_issue \
  "[dba] Validate DynamoDB access patterns and schema before implementation" \
  "**Role:** DBA

**Deliverables:**
- \`docs/db/dynamodb-access-patterns.md\` — full access pattern matrix (13 patterns)
- \`docs/db/dynamodb-schema.md\` — validated schema with hot partition analysis
- \`docs/db/atomic-patterns.md\` — ConditionalExpression patterns in Go pseudocode
- \`docs/db/redis-design.md\` — ZSET rate limiter, Lua script, allkeys-lru policy, pool sizing
- \`docs/db/capacity-planning.md\` — RCU/WCU at 1K/10K/50K RPS
- \`docs/db/migration-strategy.md\` — strategy per schema change type

**Critical findings to verify:**
- clicks table GSI projection must be INCLUDE not ALL (50% cost saving)
- SK design CLICK#{ISO-date}#{uuid} for efficient date-range queries
- daily quota reset via lazy daily_count_date pattern

See \`requirements-subagents.md\` → DBA section." \
  "type:docs,status:accepted,priority:high,size:m,component:db" \
  "Sprint 0"

# ── Issues — Sprint 1 ─────────────────────────────────────────────────────────
echo ""
echo "── Creating Sprint 1 issues ─────────────────────────────────────────────"

create_issue \
  "[aws-specialist] Lambda config, VPC design, WAF ordering, IAM policies, cost model" \
  "**Role:** AWS Specialist

**Deliverables:**
- \`docs/aws/lambda-config.md\` — ARM64, no SnapStart (Go), provisioned concurrency=2 for redirect, memory sizing, VPC design
- \`docs/aws/cloudfront-config.md\` — cache behaviors per path pattern, no-cache for redirects, custom error pages
- \`docs/aws/waf-config.md\` — rule priority table, Bot Control rollout plan (COUNT before BLOCK)
- \`docs/aws/apigw-config.md\` — throttling, CORS, custom domain
- \`docs/aws/cognito-config.md\` — token TTLs, PKCE, prevent user existence errors
- \`docs/aws/iam-policies.md\` — exact IAM JSON per Lambda function (no wildcard actions)
- \`docs/aws/cost-optimization.md\` — monthly cost at 1K/10K/50K RPS
- \`docs/aws/well-architected.md\` — 6-pillar gap analysis

**Key insight:** Lambda in VPC is required for ElastiCache access. Cold start +300ms mitigated by provisioned concurrency.

See \`requirements-subagents.md\` → AWS Specialist section." \
  "type:infra,status:accepted,priority:high,size:l,component:infra" \
  "Sprint 1"

create_issue \
  "[security] STRIDE threat model, URL validation spec, secrets policy, GDPR inventory" \
  "**Role:** Security Engineer

**Deliverables:**
- \`docs/security/threat-model.md\` — STRIDE per component
- \`docs/security/security-architecture.md\` — defense layers, URL validation spec (block SSRF, javascript:, private IPs including 169.254.169.254)
- \`docs/security/secrets-management.md\` — KMS hierarchy, rotation policy
- \`docs/security/privacy.md\` — GDPR data inventory, right to erasure
- \`docs/security/scanning.md\` — gosec config, govulncheck, OWASP ZAP config
- \`docs/security/security-tests.md\` — SEC-001..010 test specs for QA

**Feeds into:** Go Developer (URL validation), QA (security tests), DevOps (CI scanning).

See \`requirements-subagents.md\` → Security Engineer section." \
  "type:security,status:accepted,priority:high,size:l,component:security" \
  "Sprint 1"

create_issue \
  "[devops] Terraform IaC — all modules (lambda, dynamodb, apigw, cloudfront, waf, cognito, elasticache, sqs, monitoring)" \
  "**Role:** DevOps

**Terraform modules** under \`deploy/terraform/modules/\`:
lambda, dynamodb (links+clicks+users), api_gateway, cloudfront, waf, cognito, elasticache, sqs, monitoring.
Plus \`environments/dev\` and \`environments/prod\`.

**Depends on:** AWS Specialist docs for correct configuration.

See \`requirements-subagents.md\` → DevOps section." \
  "type:infra,status:accepted,priority:high,size:xl,component:infra" \
  "Sprint 1"

create_issue \
  "[devops] docker-compose.yml and docker-compose.infra.yml (LocalStack, Redis, Grafana, Jaeger, Loki)" \
  "**Role:** DevOps

- \`docker-compose.yml\` — LocalStack 3, Redis 7, app with Air hot reload
- \`docker-compose.infra.yml\` — Prometheus, Grafana (with provisioning), Jaeger all-in-one, Loki + Promtail
- LocalStack init script to create tables, queues, buckets on startup
- \`.env.example\` — complete variable template" \
  "type:infra,status:accepted,size:m,component:infra" \
  "Sprint 1"

create_issue \
  "[devops] GitHub Actions CI/CD workflows — ci.yml, deploy-dev.yml, deploy-prod.yml, destroy-dev.yml" \
  "**Role:** DevOps

- \`ci.yml\` — spec-validate, check, test, bdd, test-integration, build (on PR)
- \`deploy-dev.yml\` — deploy + test-e2e + load baseline (on merge to main)
- \`deploy-prod.yml\` — approval gate + canary deploy (on semver tag)
- \`destroy-dev.yml\` — manual workflow dispatch

All jobs must pass before PR can merge to main." \
  "type:infra,status:accepted,size:m,component:infra" \
  "Sprint 1"

create_issue \
  "[designer] Wireframes, design system tokens, user flows, WCAG accessibility checklist" \
  "**Role:** Designer

**Screens:** Landing page, password entry (/p/{code}), dashboard link list, link statistics, profile & quota settings.

**Deliverables:**
- \`docs/design/wireframes.md\` — ASCII wireframes with states (empty, loading, error)
- \`docs/design/design-system.md\` — CSS custom properties, component catalogue
- \`docs/design/user-flows.md\` — 5 user journeys
- \`docs/design/accessibility.md\` — WCAG 2.1 AA checklist

Design tokens: primary #6366F1, Inter font, monospace for links, dark mode, mobile-first." \
  "type:docs,status:accepted,size:m" \
  "Sprint 1"

# ── Issues — Sprint 2: BDD & E2E Red ─────────────────────────────────────────
echo ""
echo "── Creating Sprint 2 issues ─────────────────────────────────────────────"

create_issue \
  "[qa] Write BDD .feature files (redirect, create_link, rate_limit, password_link, ttl_expiry, stats)" \
  "**Role:** QA Automation

**MUST be done before Sprint 3 starts.** All scenarios must FAIL at runtime (red state).

Feature files in \`tests/bdd/features/\`:
- \`redirect.feature\` — active, time-expired, click-expired, password-protected
- \`create_link.feature\` — authenticated, anonymous, custom alias, duplicate
- \`rate_limit.feature\` — IP redirect limit, IP create limit, user quota
- \`password_link.feature\` — form render, wrong password, lockout, correct password
- \`ttl_expiry.feature\` — time TTL, click TTL, boundary conditions
- \`stats.feature\` — click counting, geo, timeline, referrers

Step definitions in \`tests/bdd/steps/\` must compile. Gate: \`make bdd\` exits non-zero." \
  "type:test,status:accepted,priority:critical,size:l,component:redirect,component:api" \
  "Sprint 2"

create_issue \
  "[qa] Write E2E test skeletons and security test stubs (SEC-001..010)" \
  "**Role:** QA Automation

- \`tests/e2e/\` — full AWS flow skeleton: create link → redirect → verify click in stats. Must compile, expected to fail (t.Skip or connection refused).
- Security tests from \`docs/security/security-tests.md\`: SSRF, javascript: scheme, password brute-force, JWT alg=none, XFF bypass, alias injection, XSS in title, expired JWT, CSRF, timing oracle.

Gate: \`make test-e2e\` compiles and exits non-zero." \
  "type:test,status:accepted,priority:high,size:m,component:security" \
  "Sprint 2"

# ── Issues — Sprint 3: Core Implementation ────────────────────────────────────
echo ""
echo "── Creating Sprint 3 issues ─────────────────────────────────────────────"

create_issue \
  "[dev] Project skeleton: go.mod, cmd/ layout, make spec-gen, internal/ packages" \
  "**Role:** Go Developer

First task before any Lambda code:
1. \`go mod init github.com/romanitalian/shorty\`
2. \`make spec-gen\` — generate \`internal/api/generated/\`
3. Create \`cmd/redirect/\`, \`cmd/api/\`, \`cmd/worker/\` with empty main.go
4. Create all \`internal/\` package directories with interface stubs
5. Verify \`make build\` produces linux/arm64 binaries" \
  "type:feature,status:accepted,priority:critical,size:s,component:api" \
  "Sprint 3"

create_issue \
  "[dev] Implement redirect Lambda — cache-first, async click recording, TTL enforcement" \
  "**Role:** Go Developer

Critical path target: p99 < 100ms.

Flow:
1. Redis GET link:{code} — HIT: 302 immediately
2. DynamoDB GetItem (ProjectionExpression: 6 fields only, eventually consistent)
3. Validate: is_active, expires_at, click_count < max_clicks, password_hash
4. goroutine: SQS SendMessage with context.WithTimeout(50ms) — non-blocking
5. goroutine: Redis SET link:{code} (populate cache)
6. UpdateItem atomic: click_count += 1 with ConditionalExpression
7. HTTP 302

Must not block on SQS under any circumstances." \
  "type:feature,status:accepted,priority:critical,size:l,component:redirect" \
  "Sprint 3"

create_issue \
  "[dev] Implement internal/ratelimit — Redis sliding window with Lua script" \
  "**Role:** Go Developer

Atomic sliding window using ZSET + Lua. Interface:
\`\`\`go
type Limiter interface {
    Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}
\`\`\`
Keys: rl:ip:{ip_hash}:redirect, rl:ip:{ip_hash}:create, rl:user:{user_id}:create.
Rate limit check must occur before any DynamoDB operation in all handlers." \
  "type:feature,status:accepted,priority:critical,size:m,component:ratelimit" \
  "Sprint 3"

create_issue \
  "[dev] Implement API Lambda — CRUD endpoints against generated ServerInterface" \
  "**Role:** Go Developer

Implement \`ServerInterface\` generated by oapi-codegen. All handlers must:
- Call ratelimit.Allow before business logic
- Validate JWT via internal/auth
- Propagate OTel context
- Return structured errors via pkg/apierr

Endpoints: POST /links, GET /links, GET /links/{code}, PATCH /links/{code},
DELETE /links/{code}, GET /me." \
  "type:feature,status:accepted,priority:high,size:l,component:api" \
  "Sprint 3"

create_issue \
  "[dev] Implement internal/shortener — Base62 generator with collision retry" \
  "**Role:** Go Developer

Base62 (0-9a-zA-Z), 7-char default. Collision handling via PutItem + attribute_not_exists(PK).
Retry with length +1 after 3 consecutive collisions. Exponential backoff.
Benchmark target: 0 heap allocations per Generate call." \
  "type:feature,status:accepted,size:s,component:shortener" \
  "Sprint 3"

# ── Issues — Sprint 4 ─────────────────────────────────────────────────────────
echo ""
echo "── Creating Sprint 4 issues ─────────────────────────────────────────────"

create_issue \
  "[code-review] Review Sprint 3 code — BLOCKER findings block Sprint 5" \
  "**Role:** Code Reviewer

Review all code from Sprint 3 issues. Produce \`docs/review/review-sprint-3.md\`.
BLOCKER findings must be resolved before any Sprint 5 work begins.

Checklist: correctness (data races, goroutine leaks, Lambda lifecycle), security
(NoSQL injection, JWT validation, bcrypt, rate limit before business logic),
performance (cache-first, async click, connection pooling, N+1),
observability (trace propagation, structured logging, panic recovery),
Go idioms (interfaces at point of use, error wrapping, no global state)." \
  "type:test,status:accepted,priority:high,size:m" \
  "Sprint 4"

create_issue \
  "[sre] Grafana dashboards, AlertManager rules, SLO/SLI definitions" \
  "**Role:** SRE

- \`docs/sre/slo.md\` — SLO definitions (availability 99.9%, redirect p99 < 100ms, error rate < 0.1%)
- \`config/grafana/dashboards/\` — 5 dashboard JSON files (Overview, Rate Limiting, Cache, Business, Lambda)
- \`config/prometheus/alerts.yml\` — AlertManager rules (critical, warning, info)
- Dashboards auto-provisioned — no manual Grafana import needed" \
  "type:infra,status:accepted,size:l,component:observability" \
  "Sprint 4"

create_issue \
  "[sre] Runbooks, incident response procedure, chaos experiment designs" \
  "**Role:** SRE

- \`docs/sre/runbooks/\` — one .md per alert (high-error-rate, high-latency, dynamodb-throttling, redis-unavailable, sqs-queue-depth, lambda-cold-starts, rate-limit-attack)
- \`docs/sre/incident-response.md\` — P1-P4 severity, on-call, post-mortem template
- \`docs/sre/chaos-experiments.md\` — 5 game day designs" \
  "type:docs,status:accepted,size:m,component:observability" \
  "Sprint 4"

create_issue \
  "[performance] Critical path analysis, benchmark specs, escape analysis, Lambda memory sizing" \
  "**Role:** Performance Engineer

- \`docs/performance/critical-path.md\` — latency budget per redirect step
- \`docs/performance/benchmarks.md\` — benchmark specs (0-alloc targets for hot path)
- \`docs/performance/allocations.md\` — escape analysis, sync.Pool patterns
- \`docs/performance/lambda-sizing.md\` — benchmark at 128/256/512/1024 MB
- \`docs/performance/dynamodb-performance.md\` — ProjectionExpression, eventually consistent reads
- \`docs/performance/redis-performance.md\` — pool sizing, pipeline opportunities" \
  "type:perf,status:accepted,priority:high,size:m,nfr" \
  "Sprint 4"

# ── Issues — Sprint 5 ─────────────────────────────────────────────────────────
echo ""
echo "── Creating Sprint 5 issues ─────────────────────────────────────────────"

create_issue \
  "[dev] Implement click-processor worker Lambda — SQS batch consumer" \
  "**Role:** Go Developer

SQS FIFO consumer. BatchWriteItem (25 items/batch) to clicks table.
On partial failure: return only failed message IDs to SQS for retry.
Add country lookup (GeoIP), device type detection, referer domain extraction." \
  "type:feature,status:accepted,size:m,component:worker" \
  "Sprint 5"

create_issue \
  "[dev] Implement stats API endpoints — timeline, geo, referrers" \
  "**Role:** Go Developer

Aggregate from clicks GSI code-date-index. Read with eventually consistent reads.
Cache results in Redis (TTL: 60s). Endpoints:
GET /api/v1/links/{code}/stats, /stats/timeline, /stats/geo, /stats/referrers." \
  "type:feature,status:accepted,size:m,component:api" \
  "Sprint 5"

create_issue \
  "[dev] Implement Cognito auth middleware, JWT validation, SSO flow" \
  "**Role:** Go Developer

internal/auth: JWT validation (RS256 only, alg whitelist, exp check, aud check).
Lambda authorizer or inline middleware.
Cognito token endpoint integration for token refresh." \
  "type:feature,status:accepted,priority:high,size:m,component:auth" \
  "Sprint 5"

create_issue \
  "[dev] Apply performance optimizations from Sprint 4 findings" \
  "**Role:** Go Developer

Apply recommendations from Performance Engineer:
- ProjectionExpression on redirect DynamoDB read
- sync.Pool for key builders (0 heap allocs on hot path)
- Verify SQS goroutine is truly non-blocking under load
- Lambda memory set to 512 MB based on benchmark results" \
  "type:perf,status:accepted,size:s,component:redirect,nfr" \
  "Sprint 5"

create_issue \
  "[qa] Integration tests (LocalStack) — all 7 scenarios including concurrent creation" \
  "**Role:** QA Automation

\`tests/integration/\` using LocalStack:
create→redirect→click_count, TTL time, TTL clicks, password (401/302), rate limit (429),
anonymous quota (429), concurrent code creation (only 1 winner via DynamoDB condition).

The concurrent test: 20 goroutines, same alias, assert exactly 1 returns 201." \
  "type:test,status:accepted,size:m,component:api,component:redirect" \
  "Sprint 5"

create_issue \
  "[code-review] Review Sprint 5 code — auth, stats, worker" \
  "**Role:** Code Reviewer

Review Sprint 5 PRs. Produce \`docs/review/review-sprint-5.md\`.
Special focus: JWT validation correctness, stats query N+1 risk, worker partial batch failure handling." \
  "type:test,status:accepted,size:m" \
  "Sprint 5"

# ── Issues — Sprint 6: Hardening ─────────────────────────────────────────────
echo ""
echo "── Creating Sprint 6 issues ─────────────────────────────────────────────"

create_issue \
  "[qa] Load tests — baseline, stress, spike, soak, abuse scenarios (k6)" \
  "**Role:** QA Automation

\`tests/load/\`:
- baseline.js: 1K RPS / 5 min — gate: p99 ≤ 100ms
- stress.js: ramp 0→10K RPS over 2 min, hold 5 min
- spike.js: instant 5K RPS surge
- soak.js: 500 RPS × 30 min (memory leak detection)
- abuse.js: 1K creates/sec single IP → assert 429 within 5 sec" \
  "type:test,status:accepted,priority:high,size:l,nfr" \
  "Sprint 6"

create_issue \
  "[qa] Security scan — OWASP ZAP, gosec full run, govulncheck, SEC-001..010 execution" \
  "**Role:** QA Automation

Run all security tests against dev environment:
- OWASP ZAP baseline scan → 0 HIGH findings
- gosec ./... → 0 BLOCKER
- govulncheck ./... → 0 HIGH/CRITICAL
- SEC-001..010 manual execution and verification" \
  "type:security,status:accepted,priority:high,size:m,component:security" \
  "Sprint 6"

create_issue \
  "[sre] Capacity planning report and chaos experiment execution" \
  "**Role:** SRE

- \`docs/sre/capacity-planning.md\` — AWS cost at 1K/10K/50K RPS with line items
- Execute at least 2 chaos experiments from \`docs/sre/chaos-experiments.md\` in dev
- Document results and remediation" \
  "type:docs,status:accepted,size:m,nfr" \
  "Sprint 6"

create_issue \
  "[devops] Production Terraform apply and canary deployment pipeline validation" \
  "**Role:** DevOps

- \`make tf-apply-prod\` — apply prod infrastructure
- Validate canary deploy: Lambda alias weighted routing (10% → 100%)
- Test automatic rollback: simulate error rate > 1%
- Document deploy runbook in \`deploy/scripts/deploy.sh\`" \
  "type:infra,status:accepted,priority:high,size:m,component:infra" \
  "Sprint 6"

create_issue \
  "[performance] Interpret Sprint 6 load test results and produce baseline report" \
  "**Role:** Performance Engineer

After QA runs k6 load tests:
- Analyze p99 latency at each RPS tier
- Identify any bottlenecks (cold starts, pool exhaustion, throttling)
- Produce \`docs/performance/baseline-report.md\`
- If p99 > 80ms at 1K RPS → identify root cause and open optimization issues" \
  "type:perf,status:accepted,size:s,nfr" \
  "Sprint 6"

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo "✓ GitHub setup complete for $REPO"
echo ""
echo "Next steps:"
echo "  1. Review created issues at https://github.com/$REPO/issues"
echo "  2. Enable GitHub Projects (kanban) and add issues to the board"
echo "  3. Set up branch protection on main:"
echo "     gh api repos/$REPO/branches/main/protection -X PUT \\"
echo "       -f required_status_checks='...'"
echo "  4. Enable Discussions for Q&A:"
echo "     gh api repos/$REPO -X PATCH -f has_discussions=true"
