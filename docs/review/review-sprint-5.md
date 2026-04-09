# Sprint 5 Code Review

**Reviewer:** Senior Code Reviewer (S5-T05)
**Date:** 2026-04-05
**Scope:** All Sprint 5 Go implementation files -- auth, worker, stats, integration tests

## Summary

**Total findings: 3 BLOCKER, 5 MAJOR, 4 MINOR, 3 NITPICK**

Sprint 5 delivers three major capabilities: Cognito JWT authentication, SQS click worker with partial batch failure handling, and DynamoDB-based stats aggregation. The code is well-structured, auth middleware design is clean (optional by default, RequireAuth for protected routes), and the worker correctly uses SQSEventResponse for partial batch failures. However, there are security gaps in token validation, a scalability concern in stats queries, and a data integrity bug in the worker.

**Verdict: APPROVE WITH CONDITIONS** -- all BLOCKER and MAJOR findings must be resolved before Sprint 6.

---

## Sprint 3 Fix Verification

| S3 Finding | Status | Notes |
|---|---|---|
| B1 (bcrypt) | FIXED | `cmd/api/server.go:363` uses `bcrypt.GenerateFromPassword`; integration test `SEC-006` verifies bcrypt prefix |
| B2 (CSRF) | DEFERRED | Password form lives in redirect Lambda; stub in API. Acceptable for Sprint 5. |
| B3 (security headers) | FIXED | `mw.SecurityHeaders` middleware added in `cmd/api/main.go:90` |
| B4 (body size limit) | FIXED | `mw.MaxBodySize(10 * 1024)` middleware added in `cmd/api/main.go:91` |
| B5 (TOCTOU) | UNCHANGED | `GenerateCustom` still check-only; mitigated by `CreateLink` conditional write. Acceptable. |
| M1 (DNS check) | FIXED | `validator.New(validator.WithDNSCheck())` at `cmd/api/main.go:69` |
| M2 (errors.Is) | NOT VERIFIED | Not in Sprint 5 scope; should be confirmed separately |
| M3 (501 handler) | FIXED | `chiadapter.ChiLambdaV2` integrated at `cmd/api/main.go:95` |
| M4 (clientIP) | FIXED | `clientIP()` now parses leftmost IP from X-Forwarded-For and strips port. `middleware.RealIP` in router. |
| M5 (stale cache) | UNCHANGED | Documented trade-off; cache invalidation on update/delete present. |
| M6 (field validation) | FIXED | Full field validation in `cmd/api/server.go:262-304` |

---

## Findings

### BLOCKER

#### B1. `token_use` claim not validated in Cognito JWT
**File:** `internal/auth/cognito.go:99-118`
**Severity:** BLOCKER (security vulnerability)

Cognito issues both **access tokens** and **ID tokens**. They have different `token_use` values (`"access"` vs `"id"`). The `cognitoClaims` struct declares `TokenUse string` (line 69) but `ValidateToken` never checks its value. An attacker who obtains an ID token (which is passed to the frontend and may be stored in localStorage) could use it as an access token to call API endpoints. Per AWS Cognito best practices, the `token_use` claim must be validated to match the expected type.

**Fix:** After extracting `cognitoClaims`, add:
```go
if cc.TokenUse != "access" {
    return nil, fmt.Errorf("token_use must be 'access', got %q", cc.TokenUse)
}
```

#### B2. Worker sets `UserAgentHash` to `IPHash` instead of hashing the User-Agent
**File:** `cmd/worker/main.go:120`
```go
UserAgentHash: msg.IPHash, // IP is already hashed; user_agent stored as-is in UA hash field
```
**Severity:** BLOCKER (data corruption)

The `UserAgentHash` field is assigned the value of `msg.IPHash`, not a hash of the user agent. The comment acknowledges this but the behavior is wrong -- it stores the IP hash in the wrong field, making the `user_agent_hash` column in DynamoDB contain IP hashes. This corrupts the data model and will make any future user-agent analytics produce incorrect results. Additionally, if the raw user agent should be hashed for privacy (per the field name `user_agent_hash`), it is not being hashed at all.

**Fix:** Either hash the user agent properly:
```go
UserAgentHash: hashString(msg.UserAgent),
```
Or if storing the raw UA is intentional, rename the model field to `UserAgent` and update the DynamoDB attribute name accordingly.

#### B3. JWKS refresh has no rate limiting -- cache stampede and DoS vector
**File:** `internal/auth/cognito.go:137-158`
**Severity:** BLOCKER (availability)

When a token arrives with an unknown `kid` (key rotation or attacker-crafted token), `getKey` calls `refreshKeys`. The double-check in `refreshKeys` (line 179) only prevents re-fetches within 5 seconds. An attacker can send tokens with random `kid` values every 5 seconds, each triggering a full JWKS fetch to the Cognito endpoint. At scale, this can:
1. Cause Cognito JWKS endpoint throttling, blocking legitimate key refreshes
2. Add 100-500ms latency per request during refresh (HTTP round-trip to Cognito)
3. Enable a denial-of-service via repeated cache invalidation

The `stale` check (line 140) uses `cacheTTL` (1 hour), but the double-check uses a hardcoded 5 seconds -- these are inconsistent.

**Fix:** Add a negative cache for unknown `kid` values, or limit total refresh attempts (e.g., max 1 refresh per 60 seconds regardless of `kid`). Align the double-check threshold with a configurable minimum refresh interval.

---

### MAJOR

#### M1. Stats queries fetch ALL click events into memory -- unbounded O(n) scan
**File:** `internal/store/store.go:429-466` (`queryAllClicks`)
**Severity:** MAJOR (performance/scalability)

All four stats methods (`GetLinkStats`, `GetLinkTimeline`, `GetLinkGeo`, `GetLinkReferrers`) call `queryAllClicks`, which paginates through the entire clicks table for a given link. For a popular link with millions of clicks, this will:
1. Consume massive RCU (each page reads up to 1 MB)
2. Allocate unbounded memory (all events loaded into `[]*ClickEvent`)
3. Take multiple seconds, violating the p99 < 100ms target for any path that calls stats

The in-memory aggregation with bubble sort (lines 533-539, 564-570, 596-602) makes this O(n^2) in the worst case for sorting.

**Fix (Sprint 6):** Replace `queryAllClicks` with:
- Pre-computed aggregates (materialized by the worker or a DynamoDB Stream consumer)
- Or DynamoDB `Select: COUNT` for total clicks
- Or limit the scan to the requested time range using SK range conditions
- Replace bubble sort with `sort.Slice`

#### M2. `queryAllClicks` is called 4 times for a dashboard page load
**File:** `cmd/api/server.go:582-719`
**Severity:** MAJOR (performance)

A user visiting their link dashboard will likely call `/stats`, `/stats/timeline`, `/stats/geo`, and `/stats/referrers` in sequence. Each endpoint independently calls `queryAllClicks`, meaning the same click data is scanned from DynamoDB 4 times. Combined with M1, a link with 10,000 clicks means 40,000 items read and aggregated.

**Fix:** Consider a combined stats endpoint that fetches clicks once and computes all aggregations, or implement caching of the aggregate results with a short TTL.

#### M3. Worker LOCAL_MODE HTTP handler has no body size limit
**File:** `cmd/worker/main.go:199`
```go
body, err := io.ReadAll(r.Body)
```
**Severity:** MAJOR (denial-of-service in local dev)

The local HTTP handler uses `io.ReadAll` without any size limit. While this is only active in LOCAL_MODE, it mirrors the pattern that was fixed as B4 in Sprint 3 for the API server. A large POST body could exhaust memory.

**Fix:** Use `io.LimitReader(r.Body, maxBodySize)` or `http.MaxBytesReader`.

#### M4. `localAuthenticator` accepts any token value as user ID
**File:** `cmd/api/main.go:124-129`
**Severity:** MAJOR (security in local dev)

The `localAuthenticator` uses the raw token string as the `Subject`. If LOCAL_MODE is accidentally enabled in a non-local environment, any request with `Authorization: Bearer admin` would be authenticated as user "admin". This is documented as local-only but there is no guard beyond the `LOCAL_MODE` env var.

**Fix:** Add a startup warning log when `LOCAL_MODE` is active. Consider also binding to localhost-only when in local mode (currently binds to `:8080` which is all interfaces).

#### M5. Integration test `security_test.go` SEC-009 body size test is ineffective
**File:** `tests/integration/security_test.go:276-296`
**Severity:** MAJOR (test coverage gap)

The test claims to verify body size limits but only sends a 300-character title (well under 10 KB). The test accepts multiple status codes (422, 400, and even logs 201 as acceptable). The integration test server does NOT include the `MaxBodySize` middleware, so the actual body size limit is never tested in integration.

**Fix:** Either add `MaxBodySize` middleware to the integration test server, or send an actually oversized body (>10 KB) and assert the correct rejection status code.

---

### MINOR

#### m1. Bubble sort used for stats results sorting
**File:** `internal/store/store.go:533-539, 564-570, 596-602`
**Severity:** MINOR (code quality)

Three separate bubble sort implementations exist for sorting timeline buckets, geo stats, and referrer stats. Go's standard library provides `sort.Slice` which is O(n log n) and more idiomatic.

**Fix:** Replace with `sort.Slice(result, func(i, j int) bool { return result[i].Timestamp < result[j].Timestamp })` etc.

#### m2. `GetProfile` still returns hardcoded stub data
**File:** `cmd/api/server.go:520-543`
**Severity:** MINOR (completeness)

`GetProfile` returns hardcoded quota values and does not query the `users` table. The `GetUser` and `UpdateUserQuota` store methods exist but are unused. This was noted as m3 in Sprint 3 review and is still unresolved.

**Fix:** Wire `GetProfile` to call `s.store.GetUser(ctx, userID)` and return real quota data. Add a TODO comment with target sprint if deferring.

#### m3. Integration test server duplicates all handler logic from `cmd/api/server.go`
**File:** `tests/integration/security_test.go:1-612`
**Severity:** MINOR (maintainability)

The `integrationAPIServer` in `security_test.go` is a near-complete copy of `cmd/api/server.go` (700+ lines). Any bug fix or feature change to the real server must be manually replicated in the test server. The two will inevitably drift (they already differ -- the integration server omits M6 field validation).

**Fix:** Extract the core handler logic into an `internal/api` package that both `cmd/api/main.go` and integration tests can import. The `cmd/api` package would only contain Lambda init and wiring.

#### m4. Worker skips expired events silently without metrics
**File:** `cmd/worker/main.go:97-104`
**Severity:** MINOR (observability)

Expired events are skipped with a `WARN` log but no metric is emitted. In production, a spike in expired events could indicate SQS delivery delays or clock skew, but operators would have no way to detect this without parsing logs.

**Fix:** Add a counter metric (e.g., `worker.events.expired`) via OpenTelemetry.

---

### NITPICK

#### n1. Unused `_ = shutdownTracer` in worker init
**File:** `cmd/worker/main.go:169`
```go
_ = shutdownTracer // shutdown is called by Lambda runtime on process exit
```
The comment is misleading -- Lambda runtime does not call this function. For Lambda, OTel shutdown should ideally be deferred in the handler or use the Lambda extension. The blank assignment suppresses the unused variable warning but the tracer is never properly shut down.

#### n2. Variable shadowing: `r` reused in referrer stats loop
**File:** `cmd/api/server.go:659`
```go
for _, r := range refStats {
```
The loop variable `r` shadows the `*http.Request` parameter `r` from the enclosing function signature. This compiles fine but is confusing for readers.

**Fix:** Rename the loop variable to `ref` or `rs`.

#### n3. `var _ = context.Background` unused import guard in two files
**File:** `cmd/api/server.go:735`, `tests/integration/security_test.go:611`
These lines are unnecessary -- `context` is used directly in the files. Remove them.

---

## File-by-File Notes

### `internal/auth/auth.go`
Clean interface definition. `contextKey` is an unexported type preventing key collisions -- correct Go pattern. No issues.

### `internal/auth/cognito.go`
JWKS caching and key rotation handling is well-designed overall. Algorithm pinning to RS256 is correct. The `parseRSAPublicKey` helper properly uses `base64.RawURLEncoding`. Double-check in `refreshKeys` prevents thundering herd on cache miss. Missing: `token_use` validation (B1), refresh rate limiting (B3).

### `internal/auth/middleware.go`
Clean separation between `Middleware` (optional auth) and `RequireAuth` (mandatory auth). Token extraction from both `Authorization` header and `session` cookie is correct. One design note: `Bearer ` with empty value after the prefix passes through as anonymous (line 80-82: `strings.TrimPrefix` returns empty string). This is tested and intentional per the integration tests.

### `internal/auth/auth_test.go`
Good coverage: valid JWT, expired JWT, wrong audience, middleware with/without token, session cookie. Test helpers (`testKeyPair`, `jwksJSON`, `signToken`) are well-factored. Missing test: wrong `token_use` claim (related to B1).

### `cmd/worker/main.go`
SQS partial batch failure handling via `SQSEventResponse.BatchItemFailures` is correctly implemented. Invalid JSON and missing required fields are reported as individual failures. Expired events are silently dropped (not failures) -- correct behavior. The `init()` pattern with warm-start-safe initialization is good. Issues: `UserAgentHash` bug (B2), `io.ReadAll` without limit in LOCAL_MODE (M3).

### `cmd/worker/main_test.go`
Comprehensive unit tests: valid message, invalid JSON, batch write error, expired TTL, mixed batch. The `newTestWorker` helper with fixed time is a good pattern. Mock store correctly implements the full `Store` interface.

### `cmd/api/main.go`
Auth wiring is clean: Cognito in production, `localAuthenticator` in LOCAL_MODE. The deprecated `EndpointResolverWithOptions` (M7 from Sprint 3) is still present but acceptable. The `chiadapter.ChiLambdaV2` integration fixes Sprint 3 M3.

### `cmd/api/server.go`
Stats handlers follow the ownership verification pattern consistently. `verifyLinkOwnership` is a good extraction. The `statsPeriodFromParams` helper correctly handles default 30-day period. Variable shadowing in `GetLinkStatsReferrers` (n2). Unused import guard (n3).

### `internal/store/store.go`
`queryAllClicks` is the main scalability concern (M1). The DynamoDB query pattern (PK + begins_with SK) is correct. Pagination via `LastEvaluatedKey` is properly implemented. `BatchWriteClicks` retry logic with exponential backoff + jitter is solid (unchanged from Sprint 3). Stats aggregation logic is correct but needs optimization for production scale.

### `internal/store/models.go`
New stats model types (`LinkStats`, `TimelineBucket`, `GeoStat`, `ReferrerStat`) are clean and minimal. No issues.

### `tests/integration/auth_test.go`
Good coverage of auth middleware integration: anonymous access, valid token, invalid token, expired token, RequireAuth, session cookie. Tests correctly verify both the auth middleware and the context propagation to handlers.

### `tests/integration/stats_test.go`
Good coverage: valid owner, wrong owner (404 not 403 -- correct for preventing enumeration), unauthenticated, timeline with hour/day granularity, geo, referrers, edge cases (nonexistent code, zero clicks).

### `tests/integration/helpers_test.go`
Mock implementations are comprehensive and correctly implement compile-time interface checks. The `authHeaders` helper simulates authentication via `X-User-Id` header, which matches the fallback path in `getUserID`. Note: this bypasses the actual auth middleware in most tests, relying on the header fallback.

### `tests/integration/security_test.go`
Excellent security test coverage: SSRF, scheme validation, rate limiting, XSS in title, SQL injection, bcrypt, CSRF stub, security headers, body size, IP anonymization, cross-user access. The body size test (SEC-009) is ineffective (M5). The integration server duplication is a maintenance risk (m3).

---

## Recommendation

**APPROVE WITH CONDITIONS**

The Sprint 5 implementation is solid architecturally. Auth, worker, and stats are well-structured. The following must be resolved before Sprint 6:

1. **B1 (token_use validation)** -- prevents ID token misuse; quick fix
2. **B2 (UserAgentHash data corruption)** -- data integrity; fix before any production clicks are written
3. **B3 (JWKS refresh rate limiting)** -- availability; add negative cache or refresh throttle
4. **M1+M2 (stats query scalability)** -- acceptable for MVP but must be redesigned before beta; add TODO with Sprint 6 tracking
5. **M3 (worker body size limit)** -- quick fix for LOCAL_MODE handler
6. **M5 (ineffective body size test)** -- fix to ensure CI gate actually catches regressions
