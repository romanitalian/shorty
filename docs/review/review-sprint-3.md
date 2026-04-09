# Sprint 3 Code Review

**Reviewer:** Senior Code Reviewer (S4-T01)
**Date:** 2026-04-05
**Scope:** All Sprint 3 Go implementation files

## Summary

**Total findings: 5 BLOCKER, 7 MAJOR, 6 MINOR, 5 NITPICK**

Overall the codebase is well-structured. Interfaces are defined at point of use, error wrapping is consistent, DynamoDB conditional expressions are used correctly, and the Lambda init pattern is sound. However, there are several security issues that must be resolved before Sprint 5, most notably around password verification and CSRF.

**Verdict: APPROVE WITH CONDITIONS** -- all BLOCKER and MAJOR findings must be resolved.

---

## Findings

### BLOCKER

#### B1. Password verification uses SHA-256 instead of bcrypt
**File:** `cmd/redirect/main.go` [line 186]
```go
passwordHash := HashIP(submittedPassword, h.ipSalt) // reuse hash function
if passwordHash != link.PasswordHash {
```
The `HashIP` function computes `SHA-256(salt + password)` and compares it with plain string equality against `link.PasswordHash`. Per `security-architecture.md` Section 8, passwords must be hashed with **bcrypt (cost 12)** and compared with `bcrypt.CompareHashAndPassword` (timing-safe). Using SHA-256 for password hashing is cryptographically weak (no key stretching, vulnerable to GPU attacks), and the plain `!=` comparison is not constant-time (timing side-channel).

**Fix:** Import `golang.org/x/crypto/bcrypt`, store passwords as bcrypt hashes at creation time, and verify with `bcrypt.CompareHashAndPassword([]byte(link.PasswordHash), []byte(submittedPassword))`.

#### B2. No CSRF token on password form
**File:** `cmd/redirect/main.go` [lines 45-56, 176-193]
The password form HTML does not include a CSRF token hidden field. The POST handler does not validate a CSRF token. Per `security-architecture.md` Section 6 and the checklist, `POST /p/{code}` (or the equivalent POST to `/{code}`) must include CSRF protection with an HMAC-signed token.

**Fix:** Add CSRF token generation on GET (embedded in hidden field), validate on POST before password comparison.

#### B3. No security headers on any response
**File:** `cmd/redirect/main.go`, `cmd/api/main.go`
Per `security-architecture.md` Section 5, all responses must include `X-Frame-Options`, `X-Content-Type-Options`, `Strict-Transport-Security`, `Referrer-Policy`, `Permissions-Policy`, and `Content-Security-Policy`. Neither the redirect Lambda nor the API Lambda sets any of these headers. The password form HTML response is particularly at risk (clickjacking, XSS).

**Fix:** Add a `SecurityHeadersMiddleware` to the Chi router in `cmd/api/main.go`. For the redirect Lambda, add security headers to the `passwordFormResponse` and `jsonResponse` helpers.

#### B4. No request body size limit on API endpoints
**File:** `cmd/api/main.go`, `cmd/api/server.go`
Per `security-architecture.md` Section 3.2, request bodies must be limited to 10 KB using `http.MaxBytesReader`. Currently `json.NewDecoder(r.Body).Decode(&req)` reads unbounded input, allowing request smuggling or OOM.

**Fix:** Add `LimitBodyMiddleware` to the Chi router, or wrap `r.Body` with `http.MaxBytesReader` in each handler.

#### B5. Race condition in GenerateCustom -- TOCTOU
**File:** `internal/shortener/shortener.go` [lines 99-115]
`GenerateCustom` calls `GetLink` to check availability, then returns without reserving the code. Between the check and the subsequent `CreateLink` call in `server.go`, another request can claim the same code. The `CreateLink` conditional write will catch this, but the current flow in `server.go` line 233 calls `GenerateCustom` (check only) and then `store.CreateLink` separately. If a collision happens, the error path in `server.go` line 287-289 does handle `ErrCodeCollision`, so the impact is a confusing error to the user rather than data corruption.

**Fix:** Either make `GenerateCustom` perform the `CreateLink` atomically (preferred), or document that the TOCTOU is handled by the caller's `CreateLink` error handling. Upgrading to atomic is recommended.

---

### MAJOR

#### M1. DNS check disabled by default in URL validator
**File:** `internal/validator/validator.go` [line 93], `cmd/api/server.go` [line 67]
```go
urlValidator := validator.New() // no WithDNSCheck()
```
The validator is created without `WithDNSCheck()`, so hostnames that resolve to private IPs are not blocked. Only direct IP addresses in the URL are caught. An attacker can register a domain pointing to `169.254.169.254` (AWS metadata) and store it. Per `security-architecture.md` Section 2.2, DNS resolution must be checked at creation time.

**Fix:** Use `validator.New(validator.WithDNSCheck())` in `cmd/api/server.go`. Consider making DNS check the default.

#### M2. Shortener uses `==` instead of `errors.Is` for sentinel errors
**File:** `internal/shortener/shortener.go` [lines 77, 108]
```go
if err == store.ErrLinkNotFound {
```
This uses direct equality instead of `errors.Is(err, store.ErrLinkNotFound)`. If the store ever wraps this error (e.g., via `fmt.Errorf("...: %w", ErrLinkNotFound)`), the comparison will silently fail and the code will treat "not found" as an unexpected error.

**Fix:** Replace with `errors.Is(err, store.ErrLinkNotFound)` in both locations.

#### M3. API Lambda handler returns 501 Not Implemented
**File:** `cmd/api/main.go` [lines 76-83]
The Lambda handler always returns `501 Not Implemented` with a comment "use local mode". This means the API Lambda is non-functional when deployed to AWS. The aws-lambda-go-api-proxy/chi adapter must be integrated before deployment.

**Fix:** Integrate `github.com/awslabs/aws-lambda-go-api-proxy/chi` in the handler function to proxy API Gateway events through the Chi router.

#### M4. `clientIP` in API server trusts X-Forwarded-For without sanitization
**File:** `cmd/api/server.go` [lines 86-91]
```go
func clientIP(r *http.Request) string {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        return xff
    }
    return r.RemoteAddr
}
```
The entire `X-Forwarded-For` header value (which can contain multiple comma-separated IPs and be attacker-controlled) is used as the rate limit key. An attacker can bypass rate limiting by sending different `X-Forwarded-For` values. Also, `r.RemoteAddr` includes the port (`ip:port`), which would produce different rate limit keys for the same IP.

**Fix:** Use `middleware.RealIP` (already in the router, which sets `r.RemoteAddr`), then strip the port from `r.RemoteAddr`. Or parse only the leftmost trusted IP from `X-Forwarded-For` after the last trusted proxy.

#### M5. Cached click_count becomes stale, leading to over-counting
**File:** `internal/cache/cache.go`, `cmd/redirect/main.go`
The cached link includes `click_count` and `max_clicks`. On a cache hit, the redirect handler skips DynamoDB and uses the cached link. However, `IncrementClickCount` is called with `link.MaxClicks` from the (potentially stale) cached link. The increment itself is atomic in DynamoDB (the condition expression reads the real `click_count`), so there is no data corruption. But the cached `IsActive` or `ExpiresAt` could be stale for up to 5 minutes, meaning a deactivated link continues to redirect until cache expires.

**Fix:** This is acceptable for a 5-minute TTL, but document the trade-off. Consider using a shorter TTL or adding cache invalidation when a link is deactivated. The API server's `DeleteLink` and `UpdateLink` do invalidate the cache, which helps.

#### M6. No input validation on UTM parameters, title, or max_clicks in API server
**File:** `cmd/api/server.go` [lines 265-284]
Per `security-architecture.md` Section 3.1, `title` must be max 255 chars, `utm_source/medium/campaign` must match `^[a-zA-Z0-9_-]+$` and max 128 chars, and `max_clicks` must be 1-1,000,000. None of these validations are performed in `CreateLink` or `UpdateLink`.

**Fix:** Add field-level validation before storing the link.

#### M7. Deprecated AWS SDK API used in cmd/api/main.go
**File:** `cmd/api/main.go` [lines 33-38]
```go
config.WithEndpointResolverWithOptions(...)
```
`EndpointResolverWithOptions` is deprecated in aws-sdk-go-v2 since mid-2023. It still works but will be removed in a future major version.

**Fix:** Migrate to `config.WithBaseEndpoint()` or per-service `EndpointResolverV2`.

---

### MINOR

#### m1. `fmt.Println` used in API main
**File:** `cmd/api/main.go` [line 87]
```go
fmt.Println("Starting API server on :8080")
```
Per checklist, all logging should use `zerolog`. No structured logging is set up anywhere in the codebase.

**Fix:** Add zerolog dependency, use structured logger.

#### m2. Unused import guard is a code smell
**File:** `cmd/api/server.go` [line 509]
```go
var _ = context.Background
```
This line exists to prevent an "unused import" compiler error, suggesting the `context` import is not actually used directly in this file (it is used via `r.Context()`). The line is unnecessary.

**Fix:** Remove the line. The `context` import is used transitively and the compiler should not flag it.

#### m3. `GetProfile` returns hardcoded stub data
**File:** `cmd/api/server.go` [lines 435-449]
Returns `"user@example.com"` and hardcoded quota values. This is clearly a stub but should have a `// TODO: Sprint 5` comment to track.

**Fix:** Add a tracking comment.

#### m4. `GetLinkStats` swallows store error
**File:** `cmd/api/server.go` [line 458]
```go
link, err := s.store.GetLink(ctx, code)
if err != nil || link.OwnerID != userID {
```
If `err != nil` due to an internal error (not NotFound), the response is still 404 instead of 500. The user gets a misleading error.

**Fix:** Check `errors.Is(err, store.ErrLinkNotFound)` separately from authorization check.

#### m5. `UpdateUserQuota` wraps wrong sentinel error
**File:** `internal/store/store.go` [line 475]
```go
return fmt.Errorf("store.UpdateUserQuota: quota exceeded or date mismatch: %w", ErrLinkInactiveOrLimitReached)
```
`ErrLinkInactiveOrLimitReached` is semantically about links/clicks, not user quotas. There should be a dedicated `ErrQuotaExceeded` sentinel.

**Fix:** Add `ErrQuotaExceeded` to `errors.go` and use it here.

#### m6. `passwordFormHTML` template action path may not match API Gateway routing
**File:** `cmd/redirect/main.go` [line 50]
```html
<form method="POST" action="/{{.Code}}">
```
Per `security-architecture.md`, the password form should POST to `/p/{code}`, not `/{code}`. If the redirect Lambda handles both GET and POST on `/{code}`, this works but deviates from the documented design.

**Fix:** Confirm the intended routing and align the template with the API spec.

---

### NITPICK

#### n1. `Store` interface defined in implementing package
**File:** `internal/store/store.go` [line 21]
Go idiom prefers interfaces at point of use. The `Store` interface is defined in the same package that implements it. This makes it harder for consumers to define their own subset interfaces.

That said, `CodeStore` in `internal/shortener/shortener.go` correctly follows the "interface at point of use" pattern. The main `Store` interface is used across multiple packages, so a central definition is pragmatic here.

#### n2. `DynamoDBClient` interface could use `dynamodb.Client` method set
**File:** `internal/store/store.go` [line 40]
The custom interface is correct for testability but consider using the AWS SDK's own interface if one is provided.

#### n3. `RedirectHandler` struct field ordering
**File:** `cmd/redirect/main.go` [lines 66-74]
For hot-path structs, fields should be ordered by alignment (largest to smallest). The `nowFunc` (function value, 8 bytes) and string fields are interleaved.

#### n4. Import grouping
**File:** `cmd/redirect/main.go`
Imports are grouped as stdlib / external / internal which is correct. No issue.

#### n5. `paginationToken` cursor could be tampered with
**File:** `internal/store/store.go` [lines 213-218, 243-258]
The pagination cursor is just base64-encoded JSON. A malicious client could craft a cursor to access other users' data. However, since the query is filtered by `owner_id`, this is not exploitable -- the GSI key condition ensures only the caller's links are returned.

---

## File-by-File Notes

### internal/store/store.go
Well-written DynamoDB operations. Conditional expressions are correctly used for collision prevention and authorization. `BatchWriteClicks` retry with exponential backoff is solid. The `math/rand` usage for jitter is acceptable (not security-sensitive). One concern: `UpdateLink` builds expressions dynamically from the `updates` map, but uses expression attribute names (`#attr0`, etc.) which prevents injection.

### internal/store/models.go
Clean model definitions. Field tags are consistent. `User.LinksCreatedToday` has correct alignment. No issues.

### internal/store/errors.go
Sentinel errors are properly defined. Missing `ErrQuotaExceeded` (see m5).

### internal/cache/cache.go
Good graceful degradation on Redis failure. `cachedLink` correctly stores only hot-path fields. `computeTTL` handles edge cases. One design note: the cache returns `(nil, nil)` on miss, which is idiomatic for this pattern but requires callers to check for nil rather than an error.

### internal/ratelimit/ratelimit.go
Lua script is well-designed with atomic sliding window. `FailOpen`/`FailClosed` policy is a good pattern. UUID-based request IDs prevent sorted set member collisions. The `Clock` injection enables testability.

### internal/ratelimit/config.go
Tier definitions match requirements. No issues.

### internal/ratelimit/middleware.go
Clean header helper. No issues.

### internal/shortener/shortener.go
Uses `crypto/rand` (not `math/rand`) for code generation -- correct. Custom alias regex allows underscore and hyphen (`[a-zA-Z0-9_-]`), but `security-architecture.md` Section 3.1 says `^[a-zA-Z0-9]{3,32}$` (alphanumeric only, no underscore/hyphen). The max length is 31 (regex `{2,31}` means 3-32 total with the first char) vs spec's 32. Both are close but should be aligned.

### internal/validator/validator.go
Comprehensive URL validation. Scheme stripping, private IP blocking, IDN homograph detection all present. The `init()` function for CIDR parsing is acceptable in a non-`cmd/` package since it is deterministic and only runs once. Missing: DNS check is opt-in and not enabled by default (see M1).

### internal/telemetry/telemetry.go
Clean OTel setup. No-op path for tests is good. `WithInsecure()` is fine for local/sidecar use. In production, the ADOT sidecar handles TLS. No issues.

### internal/geo/geo.go
Stub implementation is appropriate for MVP. Bot detection order (before mobile) is correct. No issues.

### cmd/redirect/main.go
Core redirect logic is correct: rate limit -> cache -> negative cache -> DynamoDB -> TTL/active check -> password -> click increment -> async SQS. The async SQS goroutine uses `context.WithTimeout` and is fire-and-forget -- correct. Main issues: password verification (B1), CSRF (B2), security headers (B3).

### cmd/api/main.go
Lambda init pattern is correct. `EndpointResolverWithOptions` is deprecated (M7). Handler returns 501 (M3).

### cmd/api/server.go
Good use of generated `ServerInterface`. Rate limiting is applied before business logic in `GuestShorten` and `CreateLink` -- correct. Missing rate limiting on `UpdateLink`, `DeleteLink`, `GetLink`, `ListLinks` (less critical but noted). Missing input validation on individual fields (M6).

### pkg/apierr/errors.go
Clean RFC 7807 implementation. `WriteProblem` correctly sets `application/problem+json`. `WriteJSON` ignores encode errors (acceptable for known-good types).

---

## Recommendation

**APPROVE WITH CONDITIONS**

The architecture and core patterns are sound. The following must be resolved before Sprint 5:

1. **B1 (password bcrypt)** -- security vulnerability, immediate fix required
2. **B2 (CSRF)** -- security vulnerability, required per OWASP
3. **B3 (security headers)** -- defense-in-depth requirement
4. **B4 (body size limit)** -- denial-of-service risk
5. **B5 (TOCTOU)** -- low-impact but should be made atomic
6. **M1 (DNS check)** -- SSRF prevention gap
7. **M2 (errors.Is)** -- correctness risk
8. **M3 (501 handler)** -- deployment blocker
9. **M4 (clientIP bypass)** -- rate limiting bypass
10. **M6 (field validation)** -- injection/abuse risk

Items M5 and M7 can be deferred to Sprint 6 if needed.
