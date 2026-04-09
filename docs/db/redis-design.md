# Redis Design -- Shorty URL Shortener

## Overview

ElastiCache Redis serves two critical roles in Shorty:

1. **Cache layer** -- reduces DynamoDB read load on the redirect hot path (target: 80-90% cache hit rate).
2. **Rate limiter** -- sliding window counters enforced before business logic in every Lambda handler.

Redis is deployed as a single-node ElastiCache instance (dev) or a single-shard replication group with one replica (prod). Cluster mode is **disabled** -- the dataset fits in a single shard and Lua scripts require all keys to hash to the same slot.

---

## 1. Cache Layer

### 1.1 Key Naming Convention

All keys use a colon-delimited namespace pattern: `{namespace}:{identifier}`.

| Key Pattern | Redis Type | Value | TTL | Description |
|-------------|-----------|-------|-----|-------------|
| `link:{code}` | STRING | JSON-serialized Link struct | `min(link.expires_at - now, 300s)` | Cached link record for redirect |
| `neg:{code}` | STRING | `"1"` | 60s | Negative cache for non-existent codes |
| `stats:{code}:aggregate` | STRING | JSON-serialized stats summary | 60s | Pre-computed click stats |

### 1.2 Cached Link Struct

The `link:{code}` value is a JSON object containing only the fields needed for redirect:

```json
{
  "original_url": "https://example.com/long-path",
  "password_hash": "$2b$10$...",
  "expires_at": 1712300000,
  "max_clicks": 100,
  "click_count": 42,
  "is_active": true,
  "utm_source": "twitter",
  "utm_medium": "social",
  "utm_campaign": "launch"
}
```

Fields deliberately excluded from the cache: `owner_id`, `title`, `created_at`, `updated_at` -- these are not needed on the redirect hot path.

### 1.3 Serialization Format: JSON

**Choice: JSON** over msgpack or protobuf.

Rationale:
- **Debuggability** -- Redis CLI `GET link:abc1234` returns human-readable output. Critical during incident response.
- **No schema registry** -- protobuf requires `.proto` compilation and version management. Overhead not justified for a single cached struct.
- **Size is acceptable** -- average cached link is ~300-500 bytes in JSON. At 50,000 cached links this is 15-25 MB. Redis memory is not the bottleneck.
- **msgpack trade-off** -- saves ~30% space but loses debuggability. Not worth it until cache exceeds 500 MB.
- **Revisit trigger** -- if cache memory exceeds 500 MB, evaluate msgpack migration (transparent to callers via adapter interface).

### 1.4 TTL Strategy

```
cache_ttl = min(link.expires_at - time.Now().Unix(), 300)
```

- **Active links without expiry:** cached for 300 seconds (5 minutes). This bounds staleness: a deactivated link may serve redirects for up to 5 minutes.
- **Active links with expiry:** cached until the earlier of expiry or 5 minutes. Prevents serving a redirect after the link has expired.
- **Click count staleness:** the cached `click_count` may be up to 5 minutes behind. For `max_clicks` enforcement, the DynamoDB conditional update is the authoritative gate (the cache is optimistic).

### 1.5 Negative Cache

When a `GET link:{code}` misses both Redis and DynamoDB, write `neg:{code}` = `"1"` with 60-second TTL.

Purpose: prevents repeated DynamoDB reads for bots scanning random short codes. At 10,000 RPS with 5% unknown codes, this avoids 500 unnecessary DynamoDB reads/second.

The 60-second TTL ensures that a newly created link becomes reachable within 1 minute even if a request for that code arrived before the link existed.

### 1.6 Cache Invalidation

Invalidation events and their actions:

| Event | Action | Triggered By |
|-------|--------|-------------|
| Link updated (title, URL, UTM) | `DEL link:{code}` | API Lambda (UpdateItem handler) |
| Link deactivated | `DEL link:{code}` | API Lambda (deactivate handler) |
| Link deleted | `DEL link:{code}` | API Lambda (delete handler) |
| Link created | No action (cache populates on first redirect) | -- |
| Click count crosses max_clicks | No action (DynamoDB conditional update is authoritative) | -- |

Invalidation is **best-effort**. If the `DEL` fails (Redis unavailable), the stale entry expires within 5 minutes. This is acceptable for our consistency model.

### 1.7 Memory Policy

```
maxmemory-policy: allkeys-lfu
```

**Why `allkeys-lfu`:**
- LFU (Least Frequently Used) keeps the most popular links in cache. URL shortener traffic follows a power-law distribution: a small number of links receive the majority of clicks.
- `allkeys-*` (not `volatile-*`) ensures eviction can target any key. With `volatile-lfu`, only keys with TTL are candidates -- but all our keys have TTL, so the practical difference is minimal. `allkeys-lfu` is the safer default.

**Why not `allkeys-lru`:**
- LRU evicts keys that haven't been accessed recently. A viral link accessed 10,000 times/minute but not in the last 2 seconds could be evicted in favor of a long-tail link accessed once. LFU prevents this.

**Why not `noeviction`:**
- On memory pressure, Redis would return OOM errors. The rate limiter would stop functioning, which is worse than evicting cold cache entries.

### 1.8 Cache Read/Write Flow (Redirect Lambda)

```
GET /{code}
  |
  v
[1] Redis GET link:{code}
  |
  +-- HIT --> deserialize, check is_active + expires_at
  |             |
  |             +-- valid --> 302 redirect (async: publish click to SQS)
  |             +-- expired/inactive --> 410 Gone, DEL link:{code}
  |
  +-- MISS --> [2] Redis GET neg:{code}
                |
                +-- HIT --> 404 Not Found (no DynamoDB call)
                |
                +-- MISS --> [3] DynamoDB GetItem(LINK#{code}, META)
                              |
                              +-- found --> SET link:{code} with TTL, 302 redirect
                              +-- not found --> SET neg:{code} "1" EX 60, 404
```

---

## 2. Rate Limiter

### 2.1 Algorithm: Sliding Window (Sorted Set)

Each rate limit check uses a Redis sorted set where:
- Members are unique request identifiers (timestamp + random suffix)
- Scores are the request timestamp in milliseconds

This gives an accurate sliding window count without the boundary issues of fixed-window counters.

### 2.2 Key Patterns

| Key Pattern | Window | Limit | Use Case |
|-------------|--------|-------|----------|
| `rl:redir:{ip_hash}` | 60s | 200 (anon), 500 (free), 2000 (pro) | Redirect rate limit |
| `rl:create:{ip_hash}` | 3600s | 5 (anon) | Anonymous link creation |
| `rl:create:user:{user_id}` | 86400s | 50 (free), 500 (pro) | Authenticated link creation |
| `rl:pwd:{code}:{ip_hash}` | 900s | 5 | Password attempt brute-force protection |

The `{ip_hash}` is `SHA-256(IP + secret_salt)` -- the same hash stored in click events. Raw IPs never reach Redis.

### 2.3 Lua Script: Sliding Window Rate Limiter

This script is executed atomically by Redis. It will be used directly in `internal/ratelimit/limiter.go`.

```lua
-- sliding_window.lua
-- Atomic sliding window rate limiter using sorted sets.
--
-- KEYS[1] = rate limit key (e.g., "rl:redir:a1b2c3...")
-- ARGV[1] = current timestamp in milliseconds (e.g., 1712300000000)
-- ARGV[2] = window size in milliseconds (e.g., 60000 for 1 minute)
-- ARGV[3] = maximum allowed requests in the window
-- ARGV[4] = unique request ID (e.g., timestamp concatenated with random suffix)
--
-- Returns: { allowed (0 or 1), current_count, ttl_ms }
--   allowed = 1 means the request is permitted
--   allowed = 0 means the request is rate-limited
--   current_count = number of requests in the current window (after cleanup)
--   ttl_ms = milliseconds until the oldest entry in the window expires

local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local request_id = ARGV[4]

-- Step 1: Remove all entries outside the current window.
-- Entries with score < (now - window) are expired.
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

-- Step 2: Count remaining entries in the window.
local count = redis.call('ZCARD', key)

-- Step 3: Check if the request is within the limit.
if count >= limit then
    -- Rate limited. Return the TTL of the oldest entry so the caller
    -- can set a Retry-After header.
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local reset_ms = 0
    if #oldest > 0 then
        reset_ms = tonumber(oldest[2]) + window - now
        if reset_ms < 0 then reset_ms = 0 end
    end
    return {0, count, reset_ms}
end

-- Step 4: Add the current request to the window.
-- Using the request_id as member ensures uniqueness even if two requests
-- arrive in the same millisecond.
redis.call('ZADD', key, now, request_id)

-- Step 5: Set key expiry to window size (cleanup if no further requests).
-- PEXPIRE uses milliseconds.
redis.call('PEXPIRE', key, window)

-- Step 6: Return success with updated count.
return {1, count + 1, 0}
```

**Atomicity guarantee:** Redis executes the entire Lua script as a single operation. No other client command can interleave between ZREMRANGEBYSCORE and ZADD. This eliminates race conditions that would occur with a WATCH/MULTI/EXEC pipeline.

### 2.4 Go Integration Pattern

```go
// internal/ratelimit/limiter.go (usage pattern)

type Result struct {
    Allowed   bool
    Count     int64
    RetryAfterMs int64
}

func (l *Limiter) Check(ctx context.Context, key string, window time.Duration, limit int64) (Result, error) {
    now := time.Now().UnixMilli()
    requestID := fmt.Sprintf("%d:%s", now, uuid.New().String()[:8])

    res, err := l.client.EvalSha(ctx, l.scriptSHA, []string{key},
        now,
        window.Milliseconds(),
        limit,
        requestID,
    ).Int64Slice()
    if err != nil {
        // Redis down: fail open (allow) or fail closed (deny) based on config.
        // Default: fail open for redirects, fail closed for creation.
        return l.fallbackPolicy(key, err)
    }

    return Result{
        Allowed:      res[0] == 1,
        Count:        res[1],
        RetryAfterMs: res[2],
    }, nil
}
```

### 2.5 Failure Mode: Redis Unavailable

| Operation | Failure Policy | Rationale |
|-----------|---------------|-----------|
| Redirect rate limit | **Fail open** (allow) | Availability > protection. DDoS is handled by WAF. A brief Redis outage should not block all redirects. |
| Creation rate limit | **Fail closed** (deny) | Abuse prevention is critical. Return 503 with Retry-After header. DynamoDB quota check is the secondary gate. |
| Cache read | **Skip cache** (fall through to DynamoDB) | Redirect still works, just slower. |

### 2.6 Per-Tier Rate Limit Configuration

Rate limits are loaded from environment variables (or SSM Parameter Store in production), not hardcoded:

```
# Redirect (per IP, sliding window)
RATE_LIMIT_REDIRECT_ANON=200/60s
RATE_LIMIT_REDIRECT_FREE=500/60s
RATE_LIMIT_REDIRECT_PRO=2000/60s

# Creation (per IP for anon, per user_id for authenticated)
RATE_LIMIT_CREATE_ANON=5/3600s
RATE_LIMIT_CREATE_FREE=50/86400s
RATE_LIMIT_CREATE_PRO=500/86400s

# Password attempts (per code+IP)
RATE_LIMIT_PASSWORD=5/900s
```

### 2.7 Token Bucket Alternative (Not Used)

A token bucket was considered for creation quotas but rejected:

- **Pro:** smoother rate limiting, allows short bursts.
- **Con:** requires persistent state (bucket level + last refill time). With Lambda's ephemeral execution model, this state must live in Redis, adding complexity.
- **Decision:** sliding window is simpler, well-understood, and sufficient for our use case. The daily quota in DynamoDB (`links_created_today`) handles the per-day cap. Redis sliding window handles the per-hour burst cap for anonymous users.

---

## 3. Stats Cache

### 3.1 Aggregate Stats

```
Key:    stats:{code}:aggregate
Type:   STRING (JSON)
TTL:    60 seconds
```

Value:
```json
{
  "total_clicks": 12345,
  "unique_visitors": 8901,
  "top_countries": [{"code": "US", "count": 5000}, {"code": "DE", "count": 2000}],
  "top_referrers": [{"domain": "twitter.com", "count": 3000}],
  "device_breakdown": {"desktop": 7000, "mobile": 5000, "bot": 345},
  "computed_at": 1712300000
}
```

This cache is populated by the stats API endpoint after querying DynamoDB. The 60-second TTL means stats are at most 1 minute stale -- acceptable for a dashboard.

### 3.2 Timeline Cache

```
Key:    stats:{code}:timeline:{period}
Type:   STRING (JSON)
TTL:    60 seconds
```

Where `{period}` is `7d`, `30d`, or `90d`. Contains an array of `{date, count}` pairs.

---

## 4. Distributed Locking (Code Generation)

Short code generation uses a collision-retry approach (generate code, PutItem with `attribute_not_exists(PK)`, retry on collision). A distributed lock is **not needed** because:

1. The DynamoDB conditional write is the authoritative uniqueness gate.
2. Collision probability is low: 62^7 = 3.5 trillion possible codes. At 100K links, collision chance per attempt is ~0.000003%.
3. Adding a Redis lock would increase redirect latency and add a failure mode.

If collision rates increase significantly (>1% of creates), the mitigation is extending code length from 7 to 8 characters, not adding locks.

---

## 5. Connection Pooling

### 5.1 Lambda Connection Pool Settings

```go
// Initialized outside the handler (survives warm invocations)
pool := &redis.Options{
    Addr:         os.Getenv("REDIS_ENDPOINT"),
    PoolSize:     10,       // max active connections per Lambda instance
    MinIdleConns: 2,        // keep 2 connections warm
    MaxIdleConns: 5,        // max idle connections
    DialTimeout:  3 * time.Second,
    ReadTimeout:  1 * time.Second,
    WriteTimeout: 1 * time.Second,
    PoolTimeout:  2 * time.Second,  // wait for connection from pool
    ConnMaxIdleTime: 240 * time.Second,
}
```

### 5.2 Connection Scaling

| Scenario | Lambda Instances | Connections per Instance | Total Redis Connections |
|----------|-----------------|------------------------|------------------------|
| Dev (LocalStack) | 1 (Go process) | 10 | 10 |
| Prod low traffic | 50 | 10 | 500 |
| Prod peak (10K RPS) | 1,000 | 10 | 10,000 |
| Prod burst (50K RPS) | 5,000 | 10 | 50,000 |

ElastiCache connection limits:
- `cache.t4g.micro`: 65,000 max connections (dev/staging)
- `cache.r7g.large`: 65,000 max connections (prod)

At 50,000 connections we are at 77% of the limit. If Lambda concurrency grows beyond 6,000 instances, consider reducing `PoolSize` to 5 or enabling connection multiplexing.

---

## 6. ElastiCache Configuration

### 6.1 Parameter Group Settings

```hcl
# deploy/terraform/modules/elasticache/main.tf

resource "aws_elasticache_parameter_group" "shorty" {
  name   = "shorty-redis"
  family = "redis7"

  parameter {
    name  = "maxmemory-policy"
    value = "allkeys-lfu"
  }

  parameter {
    name  = "tcp-keepalive"
    value = "60"
  }

  parameter {
    name  = "timeout"
    value = "300"
  }

  # Enable keyspace notifications for monitoring evictions
  parameter {
    name  = "notify-keyspace-events"
    value = "Kx"
  }
}
```

### 6.2 Replication Group (Production)

```hcl
resource "aws_elasticache_replication_group" "shorty" {
  replication_group_id = "shorty-redis"
  description          = "Shorty URL shortener - cache and rate limiter"
  engine               = "redis"
  engine_version       = "7.1"
  node_type            = "cache.r7g.large"  # 13.07 GB RAM, 2 vCPUs
  num_cache_clusters   = 2                   # 1 primary + 1 replica
  port                 = 6379

  automatic_failover_enabled = true
  multi_az_enabled           = true

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auth_token                 = var.redis_auth_token

  subnet_group_name    = aws_elasticache_subnet_group.shorty.name
  security_group_ids   = [aws_security_group.redis.id]
  parameter_group_name = aws_elasticache_parameter_group.shorty.name

  snapshot_retention_limit = 7
  snapshot_window          = "03:00-04:00"
  maintenance_window       = "sun:05:00-sun:06:00"

  tags = {
    Environment = var.environment
    Project     = "shorty"
  }
}
```

### 6.3 Dev/Local Configuration

Local development uses a standalone Redis container (no replication, no TLS, no auth):

```yaml
# docker-compose.yml (excerpt)
redis:
  image: redis:7-alpine
  ports:
    - "6379:6379"
  command: >
    redis-server
    --maxmemory 128mb
    --maxmemory-policy allkeys-lfu
    --save ""
    --appendonly no
```

No persistence (`save ""`, `appendonly no`) -- cache is ephemeral. Rate limiter state is lost on restart, which is acceptable for local development.
