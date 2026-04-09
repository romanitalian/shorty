# ADR-007: IP Anonymization via SHA-256 Hashing

## Status

Accepted

## Context

Shorty records click events for analytics: unique visitors, geographic distribution,
and traffic patterns. Client IP addresses are essential for:

1. **Unique visitor counting** -- distinguishing repeat visits from the same user.
2. **Rate limiting** -- enforcing per-IP request limits (see ADR-006).
3. **Abuse detection** -- identifying patterns like single-IP bot traffic.

However, IP addresses are **personally identifiable information (PII)** under GDPR
(Article 4(1), confirmed by ECJ in C-582/14 Breyer). Storing raw IPs creates:

- Legal compliance risk (GDPR, CCPA, LGPD).
- Data breach liability (leaked IPs can be correlated with user activity).
- Trust concerns for enterprise customers.

We need a solution that preserves analytical utility while making IP recovery
computationally infeasible.

## Decision

All IP addresses are immediately hashed using **SHA-256 with a secret salt** before
storage or logging. Raw IPs exist only in memory during request processing and are
never written to DynamoDB, logs, or any persistent store.

### Implementation

```go
func HashIP(ip string, salt string) string {
    h := sha256.New()
    h.Write([]byte(salt))
    h.Write([]byte(ip))
    return hex.EncodeToString(h.Sum(nil))
}
```

### Salt management

- The salt is stored in **AWS Secrets Manager** (`shorty/ip-hash-salt`).
- The salt is loaded once during Lambda init (outside the handler) and cached in
  memory for the lifetime of the execution environment.
- Salt rotation: planned quarterly. When the salt rotates, existing hashes become
  incomparable to new hashes. This is acceptable -- unique visitor counts reset,
  which aligns with the 90-day click data retention policy (ADR-008).

### Where hashing occurs

| Component | What is hashed | Storage field |
|---|---|---|
| Redirect Lambda | Client IP for click events | `clicks.ip_hash` |
| Redirect Lambda | Client IP for rate limiting | Redis key `rl:redirect:{ip_hash}` |
| API Lambda | Client IP for anonymous rate limiting | Redis key `rl:create:{ip_hash}` |
| API Lambda | Owner ID for anonymous links | `links.owner_id = ANON#{ip_hash}` |

### What is NOT stored

- Raw IP addresses -- never persisted anywhere.
- User-Agent strings -- hashed separately (`user_agent_hash` in clicks table).
- Referer URLs -- stored as domain only (`referer_domain`), not full URL.

## Consequences

**Positive:**
- GDPR compliance: hashed IPs are pseudonymized data. Combined with the secret salt,
  reversal is computationally infeasible.
- Unique visitor counting still works: same IP + same salt = same hash.
- Rate limiting works: hash is deterministic within a salt rotation period.
- Reduced data breach impact: leaked hashes cannot be reversed to IPs.

**Negative:**
- Cannot perform IP-based geolocation from stored data. GeoIP lookup must happen
  at request time, before hashing. The `country` field in the clicks table stores
  the resolved country code, not the IP.
- Salt rotation breaks hash continuity. Unique visitor counts are not comparable
  across salt rotation periods.
- SHA-256 is not a slow hash (unlike bcrypt). An attacker with the salt could
  brute-force the IPv4 space (~4.3 billion addresses) in hours on modern hardware.
  Mitigation: the salt is stored in Secrets Manager with restricted IAM access,
  and the 90-day click data TTL limits the exposure window.

## Alternatives Considered

**IP truncation (zeroing last octet):**
Rejected. Truncation preserves partial geographic information but reduces uniqueness
for visitor counting. A /24 subnet can contain thousands of users behind a NAT.
This approach is common in analytics tools (e.g., Google Analytics IP anonymization)
but insufficient for rate limiting.

**GeoIP lookup only (no IP storage at all):**
Rejected. Losing IP hashes eliminates unique visitor counting and makes rate limiting
stateless (must use WAF-only approach). GeoIP country codes are too coarse for
abuse detection patterns.

**HMAC-SHA256 instead of SHA-256:**
Considered. HMAC provides better cryptographic guarantees against length-extension
attacks, but since we control the input format (salt + IP, no user-controlled
prefix), plain SHA-256 with prepended salt is sufficient and simpler.

**bcrypt/argon2 (slow hashes):**
Rejected. Slow hashes would add 50-200 ms latency per request -- incompatible with
the p99 < 100 ms redirect target. SHA-256 completes in < 1 microsecond. The
brute-force risk is mitigated by salt secrecy and data TTL.

**Storing raw IPs with encryption at rest:**
Rejected. Encryption at rest (DynamoDB SSE) protects against physical media theft
but not application-level access. Any Lambda with DynamoDB read access could
retrieve raw IPs. Hashing is a defense-in-depth measure beyond encryption.
