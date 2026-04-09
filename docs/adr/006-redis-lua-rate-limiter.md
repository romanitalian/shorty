# ADR-006: Redis Lua Sliding Window Rate Limiter

## Status

Accepted

## Context

Shorty enforces rate limits at multiple tiers (RFC-0001, requirements-init.md Section 7):

| Scope | Limit | Use case |
|---|---|---|
| Per-IP redirect | 200 req/min | Prevent scraping |
| Per-IP anonymous creation | 5 links/hour | Storage abuse prevention |
| Per-user (free) creation | 50 links/day | Quota enforcement |
| Per-user (pro) creation | 500 links/day | Quota enforcement |

Rate limiting must execute **before** any business logic or database access in every
Lambda handler. A single DynamoDB read costs ~0.5 RCU; blocking abusive traffic before
that read saves both latency and money.

The rate limiter must be:
- **Atomic**: concurrent requests from the same IP must not race past the limit.
- **Low-latency**: < 1 ms overhead on the redirect hot path.
- **Distributed**: shared state across all Lambda instances.

## Decision

We implement a **sliding window counter** using a **Redis Lua script** for atomicity.

### Algorithm: sliding window log

Each request is recorded as a member of a Redis sorted set, scored by its Unix
timestamp in milliseconds. The Lua script atomically:

1. Removes expired entries outside the window.
2. Counts remaining entries.
3. If under the limit, adds the current request.
4. Returns the count and remaining quota.

```lua
-- rate_limit.lua
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])

-- Remove entries outside the window
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

-- Count current entries
local count = redis.call('ZCARD', key)

if count < limit then
    -- Add this request
    redis.call('ZADD', key, now, now .. ':' .. math.random(1, 1000000))
    redis.call('PEXPIRE', key, window)
    return {count + 1, limit - count - 1, 0}  -- {current, remaining, retry_after}
else
    -- Rate limited
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local retry_after = (oldest[2] + window) - now
    return {count, 0, retry_after}  -- {current, remaining, retry_after_ms}
end
```

### Key format

- Redirect rate limit: `rl:redirect:{ip_hash}` (window: 60s, limit: 200)
- Creation rate limit: `rl:create:{ip_hash}` (window: 3600s, limit: 5)
- User quota: `rl:quota:{user_id}:{date}` (window: 86400s, limit: per-plan)

### Response headers

Every response includes rate limit headers per the OpenAPI spec:

```
X-RateLimit-Limit: 200
X-RateLimit-Remaining: 142
X-RateLimit-Reset: 1714003260
Retry-After: 23  (only on 429 responses)
```

## Consequences

**Positive:**
- Lua script executes atomically on the Redis server -- no race conditions between
  concurrent Lambda instances.
- Sliding window provides smooth rate limiting (no burst at window boundaries, unlike
  fixed-window counters).
- Sub-millisecond execution: the entire Lua script runs in a single Redis roundtrip.
- Per-key TTL ensures automatic cleanup of stale rate limit entries.

**Negative:**
- Sorted set memory usage: each entry in the set is ~50 bytes. At 200 req/min per IP,
  a single key uses ~10 KB. With 10,000 concurrent IPs, total memory is ~100 MB.
  Acceptable for a small Redis instance.
- If Redis is unavailable, rate limiting fails open (allows all traffic). This is a
  deliberate choice: availability > strict rate enforcement. The WAF provides a
  secondary rate limiting layer (see ADR-009).
- Lua scripts block the Redis event loop during execution. Our script is O(log N)
  for ZREMRANGEBYSCORE + O(1) for ZCARD, completing in < 0.1 ms.

## Alternatives Considered

**Fixed-window counter (Redis INCR + EXPIRE):**
Rejected. Fixed windows allow burst traffic at window boundaries (up to 2x the limit).
A user could send 200 requests in the last second of window 1 and 200 more in the
first second of window 2, achieving 400 req/min in a 2-second span.

**Token bucket (Redis):**
Considered. Token bucket provides smoother rate limiting and supports burst allowances.
However, it requires tracking two values (tokens remaining + last refill time),
and the sliding window log is simpler to implement correctly in Lua. Token bucket
may be added later for specific use cases (e.g., anonymous link creation burst).

**Application-level rate limiting (in-memory):**
Rejected. Lambda functions are stateless across invocations. In-memory counters would
reset on every cold start and wouldn't be shared across concurrent instances.

**AWS WAF rate-based rules only:**
Rejected as the sole mechanism. WAF rate-based rules have a minimum threshold of
100 requests per 5-minute window and limited granularity. They serve as a secondary
defense layer (ADR-009) but cannot enforce the fine-grained, per-tier limits Shorty
requires.

**DynamoDB atomic counter for rate limiting:**
Rejected. DynamoDB writes are 5-10x slower than Redis operations and consume WCUs.
Using DynamoDB for rate limiting on the redirect path would add 5-10 ms latency and
significant write costs at 10K RPS.
