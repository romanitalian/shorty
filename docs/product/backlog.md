# Shorty Product Backlog

## Prioritization Key

- **P0** -- Must Have (MVP blocker)
- **P1** -- Should Have (MVP, workaroundable)
- **P2** -- Could Have (nice for v1)
- **P3** -- Won't Have (post-MVP)

## Effort Key

- **XS** -- < 2 hours
- **S** -- 2-4 hours
- **M** -- 4-8 hours (1 day)
- **L** -- 2-3 days
- **XL** -- 1 week+

---

## Epic 1: Link Creation

### US-001: Create short link (anonymous)
**As an** anonymous visitor,
**I want to** submit a URL and get a short link back,
**so that** I can share a shorter URL without registering.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `cmd/api`, `internal/shortener`, `internal/store` |

**Acceptance Criteria:**
- Given a valid URL, when I POST to `/api/v1/shorten`, then I receive HTTP 201 with the short link.
- Given I have already created 5 links today from my IP, when I try to create another, then I receive HTTP 429.
- Given the URL exceeds 2,048 characters, when I submit it, then I receive HTTP 400.
- Anonymous links have a maximum TTL of 24 hours.

---

### US-002: Create short link (authenticated)
**As an** authenticated user,
**I want to** create short links with optional parameters (TTL, max clicks, password, custom alias, UTM, title),
**so that** I can manage my links with full control.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | L |
| Component | `cmd/api`, `internal/shortener`, `internal/store`, `internal/auth` |

**Acceptance Criteria:**
- Given I am authenticated with a valid JWT, when I POST to `/api/v1/links` with a valid URL, then I receive HTTP 201 with the short link.
- Given I provide a custom alias that is available, when I create the link, then the short code matches my alias.
- Given I provide a custom alias that is already taken, when I create the link, then I receive HTTP 409 Conflict.
- Given I set `expires_at` to a future timestamp, then the link auto-expires at that time.
- Given I set `max_clicks` to N, then the link deactivates after N clicks.
- Given I set a password, then the link requires password entry before redirect.
- Given I provide UTM parameters, then they are appended to the original URL on redirect.

---

### US-003: Create short link with custom alias
**As an** authenticated free or pro user,
**I want to** define a custom alias for my short link,
**so that** the short URL is memorable and branded.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | S |
| Component | `cmd/api`, `internal/shortener` |

**Acceptance Criteria:**
- Given a valid alias (3-30 alphanumeric characters), when I create the link, then the code equals my alias.
- Given an alias with invalid characters, then I receive HTTP 400.
- Given an alias already in use, then I receive HTTP 409.
- Anonymous users cannot use custom aliases.

---

### US-004: Auto-append UTM parameters
**As an** authenticated user,
**I want to** set UTM parameters on link creation,
**so that** campaign tracking is automatic for every click.

| Field | Value |
|---|---|
| Priority | P2 |
| Effort | S |
| Component | `cmd/api` |

**Acceptance Criteria:**
- Given I set `utm_source`, `utm_medium`, `utm_campaign` on creation, when a visitor is redirected, then the original URL has these parameters appended.
- Given the original URL already has query parameters, then UTM params are appended with `&`.

---

## Epic 2: Redirect

### US-010: Redirect to active link
**As a** visitor,
**I want to** visit a short URL and be redirected to the original destination,
**so that** I reach the intended page quickly.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `cmd/redirect`, `internal/cache`, `internal/store` |

**Acceptance Criteria:**
- Given a short link "abc123" pointing to "https://example.com" and is active, when I request GET /abc123, then I am redirected with HTTP 302.
- Redirect p99 latency must be under 100 ms.

---

### US-011: Redirect with time-based expiry
**As a** visitor,
**I want to** receive a clear error when a link has expired,
**so that** I know the link is no longer valid.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | S |
| Component | `cmd/redirect`, `internal/store` |

**Acceptance Criteria:**
- Given a short link with `expires_at` in the past, when I request GET /{code}, then I receive HTTP 410 Gone.

---

### US-012: Redirect with click-based expiry
**As a** visitor,
**I want to** be notified when a link has reached its click limit,
**so that** I understand why the redirect failed.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | S |
| Component | `cmd/redirect`, `internal/store` |

**Acceptance Criteria:**
- Given a short link with `max_clicks=1` and `click_count=1`, when I request GET /{code}, then I receive HTTP 410 Gone.
- Given a short link with `max_clicks=5` and `click_count=4`, when I request GET /{code}, then I am redirected (click_count becomes 5) and subsequent requests return HTTP 410.

---

### US-013: Redirect with password protection
**As a** visitor,
**I want to** enter a password before being redirected to a protected link,
**so that** only authorized people can access the destination.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | M |
| Component | `cmd/redirect` |

**Acceptance Criteria:**
- Given a short link with a password set, when I request GET /{code}, then I receive an HTML form asking for the password.
- Given I submit the correct password via POST /p/{code}, then I am redirected with HTTP 302.
- Given I submit an incorrect password, then I receive HTTP 401 with the form again.
- Given I submit 5 incorrect passwords within 15 minutes from the same IP, then I receive HTTP 429.
- The HTML form includes a CSRF token.

---

### US-014: Click event recording
**As a** system,
**I want to** record click events asynchronously for every redirect,
**so that** link owners can view statistics.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `cmd/redirect`, `cmd/worker`, SQS |

**Acceptance Criteria:**
- Given a successful redirect, then a click event is published to SQS FIFO containing: hashed IP, User-Agent, Referer, country, timestamp.
- Click event publishing never blocks the redirect response.
- The worker Lambda processes events in batch and writes to the `clicks` table.
- IP addresses are stored as SHA-256(IP + secret_salt), never in plain text.

---

### US-015: Nonexistent link
**As a** visitor,
**I want to** receive a clear error when a short code does not exist,
**so that** I know the URL is invalid.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | XS |
| Component | `cmd/redirect` |

**Acceptance Criteria:**
- Given no link exists with code "notfound", when I request GET /notfound, then I receive HTTP 404.

---

## Epic 3: User Dashboard

### US-020: List my links
**As an** authenticated user,
**I want to** see a paginated list of my short links,
**so that** I can manage them.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I am authenticated, when I GET /api/v1/links, then I receive a paginated list of my links sorted by creation date (newest first).
- Given I pass `?status=active`, then only active links are returned.
- Given I pass `?status=expired`, then only expired links are returned.
- Pagination uses cursor-based pagination with `?cursor=` and `?limit=` (default 20, max 100).

---

### US-021: View link details
**As an** authenticated user,
**I want to** view the full details of one of my links,
**so that** I can see its configuration and status.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | S |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I own the link with code "abc123", when I GET /api/v1/links/abc123, then I receive full link details including click_count, is_active, expires_at, max_clicks, created_at.
- Given I do not own the link, then I receive HTTP 403 Forbidden.

---

### US-022: Update a link
**As an** authenticated user,
**I want to** update my link's title, TTL, or deactivate it,
**so that** I can manage link lifecycle.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | S |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I own the link, when I PATCH /api/v1/links/{code} with `{"title": "New Title"}`, then the title is updated and I receive HTTP 200.
- Given I own the link, when I PATCH with `{"is_active": false}`, then the link is deactivated.
- Given I do not own the link, then I receive HTTP 403.
- The short code and original_url cannot be changed after creation.

---

### US-023: Delete a link
**As an** authenticated user,
**I want to** delete one of my links,
**so that** it is permanently removed.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | S |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I own the link, when I DELETE /api/v1/links/{code}, then I receive HTTP 204 and the link is permanently removed.
- Given I do not own the link, then I receive HTTP 403.
- After deletion, GET /{code} returns HTTP 404.

---

### US-024: View quota usage
**As an** authenticated user,
**I want to** see my quota usage (links today / limit, total active / limit),
**so that** I know how many links I can still create.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | S |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I am authenticated, when I GET /api/v1/me, then I receive my profile with daily usage, daily limit, total active links, and total limit.
- Given I am on the free plan, then daily limit is 50 and total limit is 500.

---

## Epic 4: Statistics

### US-030: View aggregate statistics
**As an** authenticated user,
**I want to** see aggregate stats for a link (total clicks, unique clicks),
**so that** I can measure link performance.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I own the link, when I GET /api/v1/links/{code}/stats, then I receive total clicks, unique clicks, and breakdowns by day/week/month.
- Given I am a free user, then stats are limited to the last 30 days.
- Given I do not own the link, then I receive HTTP 403.
- Anonymous users have no access to stats.

---

### US-031: View click timeline
**As an** authenticated user,
**I want to** see a time-series of clicks,
**so that** I can identify trends and peaks.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | M |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I own the link, when I GET /api/v1/links/{code}/stats/timeline?period=day, then I receive daily click counts.
- Supported periods: hour, day, week, month.

---

### US-032: View geographic breakdown
**As an** authenticated user,
**I want to** see which countries my clicks come from,
**so that** I understand my audience geography.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | M |
| Component | `cmd/api`, `internal/store`, `internal/geo` |

**Acceptance Criteria:**
- Given I own the link, when I GET /api/v1/links/{code}/stats/geo, then I receive a list of countries with click counts, sorted descending.

---

### US-033: View referrer sources
**As an** authenticated user,
**I want to** see which websites are sending traffic to my link,
**so that** I can measure channel effectiveness.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | S |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given I own the link, when I GET /api/v1/links/{code}/stats/referrers, then I receive a list of referrer domains with click counts.

---

## Epic 5: Authentication and Authorization

### US-040: Register / Login with email and password
**As a** visitor,
**I want to** create an account or log in with email and password,
**so that** I can access authenticated features.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | L |
| Component | `internal/auth`, Cognito |

**Acceptance Criteria:**
- Given I provide a valid email and password, when I register, then my account is created and I receive JWT tokens.
- Given I provide valid credentials, when I log in, then I receive Access and Refresh JWT tokens in httpOnly cookies.
- Given I provide invalid credentials, then I receive HTTP 401.

---

### US-041: Login with Google SSO
**As a** visitor,
**I want to** log in with my Google account,
**so that** I can get started without creating a new password.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `internal/auth`, Cognito |

**Acceptance Criteria:**
- Given I initiate Google OAuth flow, when Google authenticates me, then I am redirected back with valid JWT tokens.
- Given I am a new Google user, then an account is auto-created on first login.

---

### US-042: Login with GitHub SSO
**As a** developer,
**I want to** log in with my GitHub account,
**so that** I can use my existing developer identity.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | M |
| Component | `internal/auth`, Cognito |

**Acceptance Criteria:**
- Given I initiate GitHub OAuth flow, when GitHub authenticates me, then I am redirected back with valid JWT tokens.

---

### US-043: JWT token refresh
**As an** authenticated user,
**I want to** have my session refreshed automatically,
**so that** I am not logged out unexpectedly.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | S |
| Component | `internal/auth` |

**Acceptance Criteria:**
- Given my access token has expired, when the client sends the refresh token, then a new access token is issued.
- Given the refresh token is invalid or expired, then I receive HTTP 401.

---

## Epic 6: Rate Limiting

### US-050: Rate limit anonymous redirects
**As the** system,
**I want to** limit anonymous redirect requests to 200/min per IP,
**so that** the service is protected from abuse.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `internal/ratelimit`, `cmd/redirect` |

**Acceptance Criteria:**
- Given an anonymous IP has made 200 redirect requests in the current minute, when they make another request, then they receive HTTP 429 with Retry-After header.
- Rate limit uses a sliding window algorithm in Redis.

---

### US-051: Rate limit anonymous link creation
**As the** system,
**I want to** limit anonymous link creation to 5/hour per IP,
**so that** storage abuse is prevented.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | S |
| Component | `internal/ratelimit`, `cmd/api` |

**Acceptance Criteria:**
- Given an anonymous IP has created 5 links in the current hour, when they try to create another, then they receive HTTP 429.

---

### US-052: Enforce user quotas
**As the** system,
**I want to** enforce daily and total link quotas per user plan,
**so that** resource usage is fair.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `cmd/api`, `internal/store` |

**Acceptance Criteria:**
- Given a free user has created 50 links today, when they try to create another, then they receive HTTP 429 with a message indicating their daily limit.
- Given a free user has 500 total active links, when they try to create another, then they receive HTTP 429 with a message indicating their total limit.

---

### US-053: Rate limit password attempts
**As the** system,
**I want to** limit password attempts to 5 per 15 minutes per IP per link,
**so that** password brute-force is prevented.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | S |
| Component | `cmd/redirect`, `internal/ratelimit` |

**Acceptance Criteria:**
- Given an IP has submitted 5 incorrect passwords for a link in 15 minutes, when they submit again, then they receive HTTP 429.

---

## Epic 7: Observability

### US-060: Structured logging
**As an** operator,
**I want** all services to produce structured JSON logs with trace IDs,
**so that** I can search and correlate logs efficiently.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | M |
| Component | `internal/telemetry` |

**Acceptance Criteria:**
- Every log line is valid JSON containing: level, timestamp, trace_id, span_id, service, event, duration_ms.

---

### US-061: Distributed tracing
**As an** operator,
**I want** end-to-end distributed traces for every request,
**so that** I can diagnose latency issues.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | M |
| Component | `internal/telemetry` |

**Acceptance Criteria:**
- Every Lambda handler creates a root span with link.code, user.id, cache.hit attributes.
- Traces are exportable to Jaeger (local) and X-Ray (AWS).

---

### US-062: Grafana dashboards
**As an** operator,
**I want** pre-built dashboards for overview, rate limiting, cache, business, and infrastructure metrics,
**so that** I can monitor the system at a glance.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | L |
| Component | `config/grafana/dashboards` |

**Acceptance Criteria:**
- Five dashboards are provisioned automatically: Overview, Rate Limiting, Cache, Business, Infrastructure.

---

## Epic 8: Security

### US-070: URL validation and Safe Browsing
**As the** system,
**I want to** validate URLs and check against Google Safe Browsing API,
**so that** malicious links are blocked.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | M |
| Component | `internal/validator` |

**Acceptance Criteria:**
- Given a URL flagged by Google Safe Browsing, when a user tries to create a short link, then they receive HTTP 400 with a message that the URL is blocked.
- Given a URL that is not a valid HTTP/HTTPS URL, then I receive HTTP 400.

---

### US-071: CSRF protection
**As the** system,
**I want** all form submissions to require a valid CSRF token,
**so that** cross-site request forgery is prevented.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | S |
| Component | `cmd/redirect` |

**Acceptance Criteria:**
- Given a password form POST without a valid CSRF token, then I receive HTTP 403.

---

### US-072: Security headers
**As the** system,
**I want** all responses to include security headers (CSP, X-Frame-Options, etc.),
**so that** common web attacks are mitigated.

| Field | Value |
|---|---|
| Priority | P1 |
| Effort | XS |
| Component | all Lambda handlers |

---

## Epic 9: Infrastructure & DevOps

### US-080: Terraform IaC for all AWS resources
**As a** DevOps engineer,
**I want** all AWS infrastructure managed by Terraform,
**so that** environments are reproducible and auditable.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | XL |
| Component | `deploy/terraform/` |

**Acceptance Criteria:**
- All AWS resources (Lambda, DynamoDB, SQS, ElastiCache, Cognito, CloudFront, WAF, API Gateway) are defined in Terraform modules.
- `make tf-plan-dev` and `make tf-apply-dev` work end-to-end.

---

### US-081: CI/CD pipeline
**As a** developer,
**I want** automated CI/CD with GitHub Actions,
**so that** every PR is validated and merges auto-deploy to dev.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | L |
| Component | `.github/workflows/` |

**Acceptance Criteria:**
- PR pipeline runs: spec-validate, check, test, bdd, test-integration, build.
- Merge to main triggers deploy to dev + test-e2e + test-load baseline.
- Tag triggers deploy to prod with canary + automatic rollback.

---

### US-082: Local development environment
**As a** developer,
**I want to** run the full stack locally with `make dev-up`,
**so that** I can develop and test without an AWS account.

| Field | Value |
|---|---|
| Priority | P0 |
| Effort | L |
| Component | `docker-compose.yml`, `docker-compose.infra.yml` |

**Acceptance Criteria:**
- `make dev-up` starts LocalStack, Redis, Grafana, Jaeger, Prometheus, Loki.
- API is accessible at localhost:8080, redirect at localhost:8081.
- Hot reload works for both services.

---

## Epic 10: Post-MVP Features

### US-090: Pro/Enterprise plans with billing
**As a** user,
**I want to** upgrade to Pro or Enterprise for higher limits,
**so that** I can scale my usage.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | XL |
| Component | billing, Stripe integration |

---

### US-091: Bulk import (CSV)
**As a** pro user,
**I want to** import multiple URLs from a CSV file,
**so that** I can create links in bulk.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | L |
| Component | `cmd/api` |

---

### US-092: QR code generation
**As an** authenticated user,
**I want to** generate a QR code for any short link,
**so that** I can share links in physical media.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | M |
| Component | `cmd/api` |

---

### US-093: Developer API with API key management
**As a** pro user,
**I want to** manage API keys for programmatic access,
**so that** I can integrate Shorty into my applications.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | L |
| Component | `cmd/api`, `internal/auth` |

---

### US-094: Custom domain (white-label)
**As an** enterprise user,
**I want to** use my own domain for short links,
**so that** links are branded to my company.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | XL |
| Component | CloudFront, Route53, `deploy/terraform/` |

---

### US-095: A/B redirect testing
**As a** pro user,
**I want to** split redirect traffic between multiple destinations,
**so that** I can test which page performs better.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | L |
| Component | `cmd/redirect` |

---

### US-096: Webhooks on events
**As a** pro user,
**I want to** receive webhook notifications on click and expiration events,
**so that** I can trigger workflows in external systems.

| Field | Value |
|---|---|
| Priority | P3 |
| Effort | L |
| Component | `cmd/worker`, SNS |

---

## Definition of Done

A User Story is **Done** when all of the following are true:

1. All corresponding BDD scenarios pass (`make bdd`)
2. Unit tests pass with race detector (`make test`)
3. Integration tests pass against LocalStack (`make test-integration`)
4. Code review has zero BLOCKER findings
5. Feature is deployed to dev environment and E2E tests pass (`make test-e2e`)
6. Acceptance Criteria are verified by QA
7. No regressions in existing BDD scenarios
8. Performance benchmarks pass (redirect p99 < 100 ms) if the change touches the redirect path
