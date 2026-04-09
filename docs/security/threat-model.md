# STRIDE Threat Model -- Shorty URL Shortener

**Version:** 1.0
**Date:** 2026-04-05
**Author:** Security Engineer (S1-T04)
**Status:** Active
**Review cadence:** Quarterly or after significant architecture changes

---

## 1. Attack Surface Inventory

### External (Internet-Facing)

| Surface | Protocol | Auth | Rate Limited |
|---|---|---|---|
| CloudFront distribution | HTTPS (TLS 1.2+) | None | WAF rules |
| `GET /{code}` (redirect) | HTTPS | None | 200 req/min/IP |
| `POST /p/{code}` (password form) | HTTPS | None | 5 attempts/15min/IP |
| `POST /api/v1/shorten` (guest create) | HTTPS | None | 5 links/hour/IP |
| `POST /api/v1/links` (auth create) | HTTPS | JWT (Cognito) | 50/day (free), 500/day (pro) |
| `GET /api/v1/links/*` (auth CRUD) | HTTPS | JWT (Cognito) | Per-plan quota |
| `GET /api/v1/me` (profile) | HTTPS | JWT (Cognito) | Standard |
| Cognito Hosted UI | HTTPS | OAuth 2.0 | Cognito-managed |

### Internal (AWS VPC / Service-to-Service)

| Surface | Protocol | Auth |
|---|---|---|
| API Gateway -> Lambda | AWS IAM invoke | IAM role |
| Lambda -> DynamoDB | HTTPS (AWS SDK) | IAM role |
| Lambda -> ElastiCache Redis | TLS + AUTH token | Redis AUTH |
| Lambda -> SQS FIFO | HTTPS (AWS SDK) | IAM role |
| Lambda -> Secrets Manager | HTTPS (AWS SDK) | IAM role |
| Worker Lambda -> DynamoDB | HTTPS (AWS SDK) | IAM role (clicks table only) |

---

## 2. STRIDE Analysis by Component

### Risk Rating Matrix

| | Low Impact | Medium Impact | High Impact | Critical Impact |
|---|---|---|---|---|
| **High Likelihood** | Medium | High | Critical | Critical |
| **Medium Likelihood** | Low | Medium | High | Critical |
| **Low Likelihood** | Low | Low | Medium | High |

---

### 2.1 CloudFront / WAF Edge

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| CF-S1 | **Spoofing** | Attacker spoofs `X-Forwarded-For` header to bypass IP-based rate limiting | High | High | **Critical** | CloudFront overwrites `X-Forwarded-For` with the true client IP before forwarding to API Gateway. WAF rate rules use CloudFront's `sourceIp`, not the header. Lambda reads IP from API Gateway's `requestContext.identity.sourceIp`. | Low -- CloudFront is the only entry point (origin access restricted). |
| CF-T1 | **Tampering** | TLS downgrade attack to intercept/modify traffic | Low | Critical | **High** | CloudFront enforces TLS 1.2 minimum (TLS 1.3 preferred). `security_policy = TLSv1.2_2021`. HSTS header with `max-age=31536000; includeSubDomains; preload`. | Low -- requires compromising the CloudFront TLS termination. |
| CF-R1 | **Repudiation** | Attacker denies originating abusive requests | Medium | Medium | **Medium** | CloudFront access logs enabled (S3). WAF logs to CloudWatch. IP addresses hashed in application layer per ADR-007, but CloudFront logs retain raw IPs for 14 days for incident response. | Low -- dual logging (CloudFront + application). |
| CF-I1 | **Info Disclosure** | CloudFront access logs expose raw IP addresses | Medium | Medium | **Medium** | Log retention set to 14 days. S3 bucket has restricted IAM policy (security team only). Log field redaction for non-incident use cases. Application layer never stores raw IPs (ADR-007). | Medium -- raw IPs exist in logs for 14 days. |
| CF-D1 | **DoS** | Volumetric DDoS (L3/L4) flooding the edge | Medium | High | **High** | AWS Shield Standard (included). CloudFront absorbs edge traffic globally across 400+ PoPs. API Gateway throttling at 10,000 req/s burst. Lambda concurrency limits prevent runaway cost. | Low -- Shield Standard handles most volumetric attacks. Shield Advanced recommended for production at scale. |
| CF-D2 | **DoS** | Application-layer (L7) HTTP flood targeting `GET /{code}` | High | High | **Critical** | WAF rate-based rules: 1,000 req/min/IP global. WAF Bot Control managed rule group. Application-level sliding window: 200 req/min/IP via Redis. CloudFront caching of 301 redirects absorbs repeat traffic. | Medium -- sophisticated distributed attacks from many IPs may partially succeed. |
| CF-E1 | **Elevation** | Attacker bypasses WAF rules via HTTP request smuggling | Low | Critical | **High** | API Gateway v2 (HTTP API) is not vulnerable to classic HTTP request smuggling. CloudFront normalizes requests before forwarding. WAF rules inspect normalized requests. | Low -- AWS-managed infrastructure handles normalization. |

### 2.2 API Gateway

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| AG-S1 | **Spoofing** | Attacker sends requests with forged JWT to bypass authentication | Medium | Critical | **Critical** | JWT validation at Lambda level: RS256 only, validate `exp`, `aud`, `iss` against Cognito JWKS endpoint. Reject `alg=none` and `alg=HS256`. Token expiry: 1 hour access, 7 day refresh with rotation. | Low -- RS256 with JWKS validation is cryptographically strong. |
| AG-T1 | **Tampering** | Attacker modifies request body between CloudFront and API Gateway | Low | High | **Medium** | TLS enforced end-to-end (CloudFront -> API Gateway -> Lambda). API Gateway validates request against OpenAPI schema. Input validation repeated in Lambda handler. | Low -- TLS prevents in-transit modification. |
| AG-D1 | **DoS** | API Gateway throttling exhaustion causing 429 for legitimate users | Medium | Medium | **Medium** | API Gateway configured with 10,000 req/s burst, 5,000 req/s steady. Per-route throttling configured. WAF blocks abusers before they reach API Gateway. | Medium -- during large-scale attacks, legitimate users may see occasional 429s. |
| AG-I1 | **Info Disclosure** | Verbose error messages from API Gateway leak internal details | Medium | Medium | **Medium** | Custom error responses configured via Lambda. All errors use RFC 7807 `ProblemDetail` format. Stack traces never returned in production. API Gateway `DEFAULT_5XX` customized. | Low -- standardized error responses. |

### 2.3 Redirect Lambda (`GET /{code}`)

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| RL-S1 | **Spoofing** | Bot creates automated requests to enumerate short codes and discover private links | Medium | Medium | **Medium** | 7-char Base62 = 3.5 trillion combinations. Rate limit: 200 req/min/IP. 404 responses are uniform (no timing oracle). Negative cache (`link:{code}:miss`, 60s TTL) prevents DynamoDB probing. | Low -- keyspace is too large for practical enumeration. |
| RL-T1 | **Tampering** | Open redirect: attacker stores `javascript:alert(1)` as original_url, redirect executes script | High | High | **Critical** | URL validation on creation (not redirect): only `http://` and `https://` schemes allowed. `javascript:`, `data:`, `file:`, `vbscript:`, `ftp:` blocked. Redirect uses HTTP 302 with `Location` header (browser does not execute JS in Location). | Low -- validation at creation prevents malicious URLs from being stored. |
| RL-T2 | **Tampering** | SSRF via stored URL: attacker stores `http://169.254.169.254/latest/meta-data/` and the redirect Lambda follows it | High | Critical | **Critical** | URL validation blocks private IP ranges (127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, ::1). DNS resolution check at creation time to catch DNS rebinding. Lambda does not follow the redirect -- it only issues a `Location` header. The *browser* follows the redirect, so SSRF is only a risk if the Lambda itself fetches the URL (e.g., for preview/unfurl features). | Low -- the redirect Lambda never fetches the target URL. |
| RL-R1 | **Repudiation** | User denies clicking a link (relevant for click-count billing disputes) | Low | Low | **Low** | Click events recorded asynchronously via SQS with `ip_hash`, timestamp, user-agent hash. Immutable write-only pattern (SQS -> Worker -> DynamoDB). 90-day TTL provides bounded retention. | Low -- sufficient audit trail for business purposes. |
| RL-I1 | **Info Disclosure** | Cache timing attack: response time difference reveals whether a code exists | Medium | Low | **Low** | Negative cache entries (`link:{code}:miss`) ensure consistent response times for non-existent codes. Cache-hit and cache-miss paths both return within p99 < 100ms target. Uniform 404 response body regardless of cache status. | Low -- timing differences are within noise at network level. |
| RL-D1 | **DoS** | Redis unavailable -> cache misses flood DynamoDB -> throttling -> service degradation | Medium | High | **High** | Circuit breaker on Redis: if Redis is down, redirect goes directly to DynamoDB with backoff. DynamoDB on-demand mode absorbs bursts. Rate limiter fails closed: if Redis is unavailable, deny anonymous creates (not redirects -- redirects degrade to DynamoDB-only). Alarm on cache hit ratio drop. | Medium -- extended Redis outage causes DynamoDB cost spike and latency increase. |
| RL-E1 | **Elevation** | Attacker accesses password-protected link without entering password | Medium | High | **High** | Password check is server-side (bcrypt compare in Lambda). Cache stores `password_hash` presence flag (not the hash itself). If `has_password` is true, Lambda returns 403 with HTML form. Password hash is only loaded from DynamoDB for comparison. | Low -- password enforcement is in the Lambda handler, not in the cache layer. |

### 2.4 API Lambda (CRUD + Stats)

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| AL-S1 | **Spoofing** | Stolen JWT used to impersonate another user and access/modify their links | Medium | Critical | **Critical** | Short access token TTL (1 hour). Refresh token rotation (Cognito revokes old refresh token on use). httpOnly, Secure, SameSite=Strict cookie. Token revocation on password change. | Medium -- stolen token is valid for up to 1 hour. |
| AL-S2 | **Spoofing** | Bot creates unlimited links via `POST /api/v1/shorten` (guest endpoint) to abuse the service as free hosting/redirect infrastructure | High | High | **Critical** | Anonymous quota: 5 links/hour/IP via Redis sliding window. Guest links max TTL 24 hours. WAF CAPTCHA triggered on suspicious creation patterns. Honeypot field in form. Google Safe Browsing check (async). | Medium -- distributed botnets can rotate IPs. |
| AL-T1 | **Tampering** | NoSQL injection via malformed DynamoDB expressions | Medium | High | **High** | All DynamoDB operations use the AWS SDK `expression` package -- never string concatenation. Input validation: `code` matches `^[a-zA-Z0-9]{3,32}$`, `url` is validated URI, `title` max 255 chars. No user input is ever interpolated into expressions. | Low -- SDK parameterization prevents injection. |
| AL-T2 | **Tampering** | XSS via link `title` field: attacker stores `<script>alert(1)</script>` | Medium | Medium | **Medium** | Output encoding: all JSON responses are serialized by `encoding/json` which escapes `<`, `>`, `&`. HTML responses (password form) use `html/template` which auto-escapes. CSP header: `script-src 'self'` blocks inline scripts. `X-Content-Type-Options: nosniff`. | Low -- multiple layers of XSS protection. |
| AL-T3 | **Tampering** | Alias injection: custom code set to `../../../etc/passwd` or path traversal | Medium | Medium | **Medium** | Custom code validated against `^[a-zA-Z0-9]{3,32}$` (alphanumeric only, no special characters). Regex enforced at OpenAPI schema level and in Lambda handler. | Low -- strict regex prevents any traversal characters. |
| AL-R1 | **Repudiation** | User creates a phishing link and denies ownership | Medium | High | **High** | Immutable audit trail: `owner_id` (Cognito sub) + `created_at` stored on every link. Anonymous links store `ANON#{ip_hash}` as owner. IP hash salt rotates quarterly (old hashes become unlinkable by design). CloudTrail logs all AWS API calls. | Medium -- anonymous links have weaker attribution after salt rotation. |
| AL-I1 | **Info Disclosure** | Unauthorized user accesses another user's link details or statistics | Medium | High | **High** | Authorization check on every endpoint: `owner_id` must match JWT `sub` claim. DynamoDB ConditionExpression: `owner_id = :caller_id` on update/delete. Stats endpoints verify link ownership before returning data. | Low -- per-request ownership validation. |
| AL-I2 | **Info Disclosure** | Password hash leaked in API response | Medium | High | **High** | `password_hash` is never included in API responses. The `Link` schema returns `has_password: boolean` flag only. `password_hash` is excluded from all DynamoDB projection expressions except the internal redirect flow. | Low -- field is never projected in read operations. |
| AL-D1 | **DoS** | Large payload attack: POST body with extremely long URL or many fields | Medium | Medium | **Medium** | API Gateway payload limit: 10 MB (default). Application-level validation: `url` max 2,048 chars, `title` max 255 chars, `password` max 128 chars, `custom_code` max 32 chars. Request body limit enforced at 10 KB in Lambda middleware. | Low -- multiple size limits. |
| AL-E1 | **Elevation** | JWT algorithm confusion: attacker signs token with `alg=none` or `alg=HS256` using the public key | Medium | Critical | **Critical** | JWT validation explicitly requires `alg=RS256`. Reject any token with `alg=none`, `alg=HS256`, or any symmetric algorithm. Validate against Cognito JWKS endpoint (cached, refreshed on key rotation). Check `iss` matches Cognito User Pool URL. Check `aud` matches app client ID. | Low -- explicit algorithm pinning prevents confusion attacks. |

### 2.5 Worker Lambda (SQS Click Processor)

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| WL-S1 | **Spoofing** | Unauthorized messages injected into SQS queue | Low | High | **Medium** | SQS queue policy restricts `SendMessage` to the Redirect Lambda IAM role only. SQS FIFO deduplication prevents replay. Dead-letter queue for failed messages. | Low -- IAM policy restricts access. |
| WL-T1 | **Tampering** | Malformed click event causes Worker to write corrupt data | Medium | Medium | **Medium** | Worker validates all message fields before writing to DynamoDB: `code` format, `ip_hash` is 64-char hex, `country` is 2-char alpha, `device_type` is enum, `timestamp` is valid Unix. Invalid messages sent to DLQ with alert. | Low -- input validation on consumer side. |
| WL-R1 | **Repudiation** | Click events lost between SQS and DynamoDB | Low | Medium | **Low** | SQS FIFO guarantees exactly-once delivery. DLQ captures failed processing. CloudWatch alarm on DLQ depth > 0. Worker uses BatchWriteItem with error handling for partial failures. | Low -- SQS FIFO + DLQ provides delivery guarantees. |
| WL-D1 | **DoS** | Click event backlog causes SQS queue depth to grow, increasing Lambda concurrency and cost | Medium | Medium | **Medium** | Worker Lambda reserved concurrency limit (e.g., 10). SQS batch size = 10, max batching window = 5s. CloudWatch alarm on queue depth > 10,000. Auto-scaling is bounded by reserved concurrency. | Low -- concurrency limits cap cost exposure. |
| WL-E1 | **Elevation** | Worker Lambda IAM role has excessive permissions beyond clicks table | Low | High | **Medium** | Least-privilege IAM: Worker role has `dynamodb:BatchWriteItem` on `clicks` table only. No access to `links` or `users` tables. No `PutItem` on `links`. Reviewed in Terraform code review. | Low -- IAM least privilege enforced in Terraform. |

### 2.6 DynamoDB

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| DB-S1 | **Spoofing** | Unauthorized AWS principal accesses DynamoDB tables | Low | Critical | **High** | IAM policies restrict table access to specific Lambda execution roles. No public access. VPC endpoint for DynamoDB (no internet transit). CloudTrail logs all DynamoDB API calls. | Low -- IAM + VPC endpoint isolation. |
| DB-T1 | **Tampering** | Direct table manipulation bypassing application logic (e.g., setting `click_count` to bypass max_clicks) | Low | High | **Medium** | No human access to production DynamoDB (IAM restriction). All modifications go through Lambda handlers with business logic enforcement. ConditionExpressions enforce invariants (e.g., `click_count < max_clicks`). | Low -- application-level invariants + IAM. |
| DB-I1 | **Info Disclosure** | Data at rest exposed via stolen EBS volume or backup | Low | High | **Medium** | DynamoDB SSE with AWS KMS Customer Managed Key (CMK). Point-in-time recovery (PITR) enabled. Backups encrypted with same CMK. CMK key policy restricts usage to DynamoDB service principal + admin role. | Low -- encryption at rest with CMK. |
| DB-I2 | **Info Disclosure** | Password hashes or email addresses exposed in DynamoDB Streams or exports | Low | High | **Medium** | DynamoDB Streams not enabled (not required). Data export uses encrypted S3 bucket with restricted access. `password_hash` never included in GSI projections. | Low -- no streams, restricted export. |
| DB-D1 | **DoS** | Hot partition on popular short code causes throttling | Medium | Medium | **Medium** | `links` table uses on-demand mode (auto-scales). Partition key `LINK#{code}` distributes evenly (Base62 random codes). Popular links cached in Redis (5 min TTL). `clicks` table isolated from `links` (separate tables). | Low -- on-demand + caching. |

### 2.7 ElastiCache Redis

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| RC-S1 | **Spoofing** | Unauthorized client connects to Redis | Low | High | **Medium** | Redis AUTH token (stored in Secrets Manager). TLS in-transit encryption enabled. Redis deployed in private subnet (no public IP). Security group restricts access to Lambda ENIs only. | Low -- AUTH + TLS + network isolation. |
| RC-T1 | **Tampering** | Attacker manipulates rate limiter keys to bypass rate limits | Low | High | **Medium** | Rate limiter uses atomic Lua script (ZREMRANGEBYSCORE + ZCARD + ZADD in single EVAL). Redis AUTH prevents unauthorized access. Keys use `ip_hash` (not raw IP), so attacker cannot predict key names without the salt. | Low -- Lua atomicity + AUTH + hashed keys. |
| RC-I1 | **Info Disclosure** | Cached link data (including password_hash presence) exposed | Low | Medium | **Low** | Cache stores `has_password` flag, not the actual bcrypt hash. Redis AUTH + TLS + private subnet. Cache entries expire after 5 minutes. Redis does not persist to disk (no RDB/AOF in ElastiCache). | Low -- minimal sensitive data in cache. |
| RC-D1 | **DoS** | Redis out of memory causes eviction of rate limiter keys, bypassing rate limits | Medium | High | **High** | `maxmemory-policy = volatile-lru` (evict keys with TTL first). Rate limiter keys have short TTL (60s-900s). Separate Redis cluster for cache vs. rate limiting (if budget allows). CloudWatch alarm on memory usage > 80%. **Fail-closed policy**: if Redis is unreachable, deny anonymous creates. | Medium -- under extreme memory pressure, rate limiter keys may be evicted before expiry. |
| RC-D2 | **DoS** | Redis cluster failure causes redirect latency spike (cache miss -> DynamoDB) | Medium | Medium | **Medium** | ElastiCache Multi-AZ with automatic failover. Redirect Lambda has circuit breaker: if Redis timeout > 50ms, skip cache and go to DynamoDB. DynamoDB on-demand mode handles the burst. | Medium -- failover takes seconds; brief latency spike during switchover. |

### 2.8 SQS FIFO

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| SQ-S1 | **Spoofing** | Unauthorized producer sends fake click events | Low | Medium | **Low** | SQS queue policy: only Redirect Lambda role can `SendMessage`. Messages include `code`, `ip_hash`, `timestamp` -- all validated by Worker before writing. | Low -- IAM restricts producers. |
| SQ-T1 | **Tampering** | Message modified in transit | Low | Medium | **Low** | SQS uses HTTPS (AWS SDK enforces TLS). SQS server-side encryption with KMS CMK. FIFO deduplication detects replayed messages. | Low -- TLS + SSE + deduplication. |
| SQ-D1 | **DoS** | Queue backlog grows unbounded during traffic spike, causing delayed analytics | Medium | Low | **Low** | SQS FIFO throughput: 3,000 msg/s with batching. Worker Lambda reserved concurrency: 10. DLQ with alarm on depth > 0. Click analytics are non-critical (do not affect redirect latency). | Low -- analytics delay is acceptable; redirect is unaffected. |
| SQ-I1 | **Info Disclosure** | Click events in queue contain potentially sensitive data (ip_hash, referer) | Low | Low | **Low** | SSE with KMS CMK. ip_hash is irreversible without salt. Referer stored as domain only. Queue retention: 4 days max. | Low -- pseudonymized data + encryption. |

### 2.9 Cognito

| # | STRIDE | Threat Description | Likelihood | Impact | Risk | Mitigation | Residual Risk |
|---|---|---|---|---|---|---|---|
| CG-S1 | **Spoofing** | Credential stuffing attack against Cognito User Pool (email/password) | High | High | **Critical** | Cognito Advanced Security Features: adaptive authentication, compromised credential detection. WAF rate limit on Cognito hosted UI. MFA option available (TOTP). Account lockout after 5 failed attempts (Cognito-managed). | Medium -- credential stuffing is persistent; MFA adoption depends on users. |
| CG-S2 | **Spoofing** | OAuth token theft via authorization code interception | Medium | High | **High** | PKCE (Proof Key for Code Exchange) required for all OAuth flows. `state` parameter validated. Redirect URI strictly matched (no wildcards). Tokens in httpOnly, Secure, SameSite=Strict cookies. | Low -- PKCE prevents authorization code interception. |
| CG-T1 | **Tampering** | Attacker modifies Cognito user attributes (email, plan) via Cognito API | Low | High | **Medium** | User attributes marked as immutable or admin-writable-only in Cognito schema. `plan` attribute managed exclusively by server-side admin flow (not user self-service). Lambda pre-token-generation trigger adds custom claims. | Low -- attribute write permissions restricted. |
| CG-I1 | **Info Disclosure** | User email addresses enumerated via Cognito sign-up/forgot-password flow | Medium | Medium | **Medium** | Cognito configured to prevent user existence errors (returns generic messages). Custom email templates do not reveal whether account exists. | Low -- generic error messages. |
| CG-D1 | **DoS** | Cognito service throttled by high login/signup volume | Low | Medium | **Low** | Cognito has built-in throttling with automatic scaling. WAF rate limits on auth endpoints. Service quotas can be increased via AWS support. | Low -- AWS-managed scaling. |
| CG-E1 | **Elevation** | Attacker registers with email similar to admin and gains elevated access | Low | Critical | **High** | No role-based access in application (only `plan` tiers). Admin operations require separate IAM credentials (not Cognito). Email verification required before account activation. | Low -- no admin role in Cognito; admin access is IAM-based. |

---

## 3. Top Risks Summary (Sorted by Risk Rating)

| Rank | ID | Threat | Risk | Status |
|---|---|---|---|---|
| 1 | CF-S1 | X-Forwarded-For spoofing to bypass rate limits | Critical | Mitigated (CloudFront rewrites XFF) |
| 2 | CF-D2 | L7 HTTP flood on redirect endpoint | Critical | Mitigated (WAF + Redis rate limit); residual risk from distributed attacks |
| 3 | RL-T1 | Open redirect via malicious stored URL | Critical | Mitigated (URL validation on creation) |
| 4 | RL-T2 | SSRF via stored URL targeting AWS metadata | Critical | Mitigated (IP range blocking + Lambda does not fetch URL) |
| 5 | AL-S1 | Stolen JWT for user impersonation | Critical | Mitigated (short TTL, rotation); 1-hour exposure window |
| 6 | AL-S2 | Bot abuse of guest link creation | Critical | Mitigated (quota + CAPTCHA); distributed botnets remain a risk |
| 7 | AL-E1 | JWT algorithm confusion (alg=none) | Critical | Mitigated (RS256 pinning) |
| 8 | CG-S1 | Credential stuffing against Cognito | Critical | Partially mitigated (Cognito security + WAF); depends on MFA adoption |
| 9 | RC-D1 | Redis OOM causing rate limiter bypass | High | Partially mitigated (fail-closed + monitoring) |
| 10 | RL-E1 | Bypass password protection on links | High | Mitigated (server-side bcrypt check) |

---

## 4. OWASP Top 10 2021 Mapping

| OWASP Category | Relevant Threats | Status |
|---|---|---|
| A01:2021 Broken Access Control | AL-I1, AL-E1, RL-E1, WL-E1 | Mitigated: per-request ownership check, RS256 JWT, IAM least privilege |
| A02:2021 Cryptographic Failures | CF-T1, DB-I1, RC-S1 | Mitigated: TLS 1.2+, DynamoDB SSE with CMK, Redis AUTH+TLS |
| A03:2021 Injection | AL-T1, AL-T2, AL-T3 | Mitigated: SDK parameterization, input validation, output encoding |
| A04:2021 Insecure Design | RL-T1, RL-T2, AL-S2 | Mitigated: URL validation, SSRF prevention, abuse quotas |
| A05:2021 Security Misconfiguration | AG-I1, CG-T1 | Mitigated: custom error responses, Cognito attribute restrictions |
| A06:2021 Vulnerable Components | -- | Mitigated: govulncheck + gosec in CI, Trivy container scanning |
| A07:2021 Identification and Authentication Failures | AG-S1, CG-S1, CG-S2 | Mitigated: RS256 JWT, PKCE OAuth, Cognito adaptive auth |
| A08:2021 Software and Data Integrity Failures | SQ-T1, WL-T1 | Mitigated: SQS SSE + TLS, Worker input validation |
| A09:2021 Security Logging and Monitoring Failures | CF-R1, RL-R1, AL-R1 | Mitigated: CloudTrail, CloudFront logs, immutable click log |
| A10:2021 Server-Side Request Forgery | RL-T2 | Mitigated: URL validation blocks private IPs; Lambda never fetches target URL |

---

## 5. Threat Model Maintenance

- **Trigger for review:** New endpoint, new data store, new external integration, or security incident.
- **Owner:** Security Engineer role.
- **Process:** Update this document, create ADR for new mitigation decisions, update security test specifications (`docs/security/security-tests.md`).
- **Tooling:** Quarterly automated scan (govulncheck, gosec, Trivy) results reviewed against this model.
