# Redis Performance Guide

> ElastiCache Redis serves as both the redirect cache layer and rate limiter for Shorty.
> This document covers connection management, memory optimization, Lua script performance,
> failure modes, and monitoring -- all tuned for the Lambda execution environment.

---

## 1. Connection Pooling in Lambda Context

### The Lambda Connection Model

Each Lambda instance maintains its own Redis connection pool, initialized in `init()` outside the handler. Connections survive across warm invocations but are destroyed when the instance is recycled.

**Current configuration** (from `cmd/redirect/main.go` and `docs/db/redis-design.md`):

| Parameter | Value | Rationale |
|---|---|---|
| `PoolSize` | 10 | Max active connections per Lambda instance |
| `MinIdleConns` | 2 | Keep 2 connections warm between invocations |
| `MaxIdleConns` | 5 | Cap idle connection memory |
| `DialTimeout` | 3s | Fail fast on connection issues |
| `ReadTimeout` | 1s | Single GET/SET should complete in < 1 ms |
| `WriteTimeout` | 1s | Lua EVALSHA should complete in < 2 ms |
| `PoolTimeout` | 2s | Wait for a free connection from pool |
| `ConnMaxIdleTime` | 240s | Recycle idle connections before ElastiCache timeout (300s) |

### Recommended Adjustment: Reduce PoolSize to 2

Lambda processes **one request at a time** (single concurrent invocation per instance). A pool of 10 connections is wasteful:

- 10 idle connections cost ~100 KB memory per Lambda instance.
- Cold start establishes up to `MinIdleConns` (2) connections, adding ~20 ms. With `PoolSize: 10`, all 10 could be created under connection pressure.
- At 1,000 Lambda instances (10K RPS), 10,000 total Redis connections approach the ElastiCache 65,000 limit.

**Recommendation:** `PoolSize: 2` (one active + one spare for pipelining). This gives:

| Lambda Instances | Connections per Instance | Total Redis Connections |
|---|---|---|
| 50 (low traffic) | 2 | 100 |
| 1,000 (10K RPS) | 2 | 2,000 |
| 5,000 (50K RPS) | 2 | 10,000 |

With `PoolSize: 2`, even at 50K RPS burst the connection count stays at 15% of ElastiCache limits.

### Connection Lifecycle

```
Lambda Cold Start
  |
  [init()] -- redis.NewClient(opts)
  |           PoolSize: 2, MinIdleConns: 1
  |           Establishes 1 TCP connection (~20 ms with TLS + AUTH)
  |
  [Handler invocation 1]
  |  GET link:{code}     -- uses connection 1
  |  EVALSHA rate_limit  -- uses connection 1 (sequential)
  |
  [Handler invocation 2...N]  (warm path, reuses connection)
  |  Same connection, no dial overhead
  |
  [4 min idle]
  |  ConnMaxIdleTime triggers close
  |  MinIdleConns: 1 triggers reconnect on next invocation
  |
  [Lambda recycled]
  |  All connections closed with instance
```

### ConnMaxIdleTime vs ElastiCache Timeout

ElastiCache `timeout` parameter is set to 300s (see `docs/db/redis-design.md`). Setting `ConnMaxIdleTime: 240s` in the Go client ensures connections are recycled before ElastiCache drops them server-side. A server-side close causes a "connection reset" error on the next command, requiring a retry.

---

## 2. Pipeline vs Single Commands Analysis

### Current Pattern: Sequential Single Commands

The redirect hot path makes 2-3 sequential Redis roundtrips:

```
[1] EVALSHA sliding_window.lua  -- rate limit check (~1-2 ms)
[2] GET link:{code}             -- cache lookup (~0.5-1 ms)
[3] GET neg:{code}              -- negative cache check (~0.5-1 ms, only on miss)
```

Total Redis time on cache miss: 2-4 ms across 3 roundtrips.

### Optimization: Pipeline Cache + Negative Lookups

Steps [2] and [3] are independent reads executed sequentially. Pipelining them into a single roundtrip saves one network hop.

```go
// Proposed: GetLinkOrNegative pipelines both GETs
pipe := client.Pipeline()
linkCmd := pipe.Get(ctx, "link:" + code)
negCmd := pipe.Get(ctx, "neg:" + code)
_, err := pipe.Exec(ctx)
// Parse linkCmd and negCmd results
```

| Pattern | Roundtrips | Latency (cache miss) |
|---|---|---|
| Sequential GETs | 3 | 2-4 ms |
| Pipelined GET + negative | 2 | 1.5-3 ms |

**Savings:** ~0.5 ms per cache-miss request. Referenced in `docs/performance/critical-path.md` as finding F2.

### When NOT to Pipeline

- **Rate limit + cache lookup:** The rate limit EVALSHA must complete before the cache lookup, because a rate-limited request should not touch the cache at all. These remain sequential.
- **Cache SET on miss:** `SetLink` happens after DynamoDB returns. It could be fire-and-forget (goroutine), which is better than pipelining since it removes it from the critical path entirely (finding F1).

### Pipeline Overhead

Redis pipeline batches commands into a single TCP write and reads all responses in one TCP read. The overhead is:
- One `[]byte` buffer allocation for the pipeline (amortized across commands).
- No additional Redis server overhead -- commands are processed sequentially server-side.

For 2 commands, pipeline overhead is negligible. For batch operations (e.g., warming multiple cache entries), pipelines become essential.

---

## 3. Memory Optimization

### 3.1 Key Naming Efficiency

Current key patterns and their memory cost:

| Key Pattern | Example | Key Size | Overhead |
|---|---|---|---|
| `link:{code}` | `link:abc1234` | 12 bytes | ~100 bytes (SDS header + dict entry + TTL) |
| `neg:{code}` | `neg:abc1234` | 11 bytes | ~80 bytes |
| `rl:redir:{ip_hash}` | `rl:redir:a1b2c3...` (64-char hex) | 74 bytes | ~200 bytes (sorted set overhead) |

Key naming is already compact. The largest keys are rate limiter keys due to the 64-character SHA-256 IP hash. Truncating to 16 characters would save 48 bytes per key with acceptable collision risk (16 hex chars = 64 bits), but the savings are negligible at scale (~50 KB across 1,000 rate limit keys).

### 3.2 TTL Strategy

| Key Type | TTL | Rationale |
|---|---|---|
| `link:{code}` | `min(expires_at - now, 300s)` | Bounds staleness to 5 minutes. Prevents serving expired links. |
| `neg:{code}` | 60s | Short enough that newly created links become reachable within 1 minute. |
| `rl:redir:{ip_hash}` | Window size (60s) | Set by Lua script via `PEXPIRE`. Auto-cleanup when no requests arrive. |
| `stats:{code}:aggregate` | 60s | Dashboard stats are at most 1 minute stale. |

The 300s link TTL is the main tuning parameter:
- **Shorter (60s):** Fresher data, more DynamoDB reads, lower cache hit rate.
- **Longer (600s):** Higher cache hit rate, more stale data, deactivated links serve up to 10 minutes.
- **300s is the balance point** for 80-90% cache hit rate at power-law traffic distribution.

### 3.3 Eviction Policy: `allkeys-lfu`

Selected over `allkeys-lru` because URL shortener traffic follows a power-law distribution. LFU keeps viral links cached even during brief idle periods, while LRU would evict them in favor of rarely-accessed long-tail links.

Configuration in ElastiCache parameter group:
```
maxmemory-policy: allkeys-lfu
```

**Eviction pressure indicators:**
- `Evictions` metric > 100/min: consider upgrading node type.
- `DatabaseMemoryUsagePercentage` > 80%: approaching eviction territory.

### 3.4 Value Serialization: JSON (Current)

The `cachedLink` struct serializes to ~300-500 bytes JSON. At 50,000 cached links, total cache payload is ~15-25 MB.

| Format | Avg Size | Marshal Time | Unmarshal Time | Debuggability |
|---|---|---|---|---|
| JSON | ~400 B | < 500 ns | < 1,000 ns | High (Redis CLI readable) |
| MessagePack | ~280 B | < 300 ns | < 500 ns | Low (binary) |
| Hand-rolled binary | ~200 B | < 100 ns | < 100 ns | None |

**Current choice (JSON) is correct.** The total cache memory footprint is under 50 MB even at high traffic. Switch to MessagePack only when cache size exceeds 500 MB, which requires > 1 million actively cached links.

---

## 4. Lua Script Performance

### Sliding Window Rate Limiter

The rate limiter uses a single Lua script (`docs/db/redis-design.md` section 2.3) with these Redis operations per call:

```
ZREMRANGEBYSCORE key 0 (now - window)   -- O(log(N) + M) where M = expired members
ZCARD key                                 -- O(1)
ZADD key now request_id                   -- O(log(N))
PEXPIRE key window                        -- O(1)
```

Total time complexity: `O(log(N) + M)` per call, where N = current window size (max 200 for redirect, 5 for creation) and M = expired entries removed.

### Sorted Set vs Simple Counter Trade-offs

| Approach | Accuracy | Memory per Key | Ops per Check | Complexity |
|---|---|---|---|---|
| **Sorted set (current)** | Exact sliding window | ~5 KB (200 members) | 4 Redis ops (atomic Lua) | Medium |
| Simple counter + EXPIRE | Fixed window only | ~80 B | 2 Redis ops (INCR + EXPIRE) | Low |
| Two counters (sliding) | Approximate | ~160 B | 3 Redis ops | Low |

**Why sorted set is correct for Shorty:**

1. **Accuracy at boundaries:** A simple counter resets at fixed intervals, allowing 2x burst at window boundaries (e.g., 200 requests at 0:59 + 200 at 1:00). The sorted set prevents this.
2. **Memory is acceptable:** At 10,000 unique IPs, sorted set memory = 10,000 * 5 KB = 50 MB. This fits in the `cache.t4g.medium` (3 GB) node.
3. **Latency is acceptable:** The EVALSHA roundtrip is 1-2 ms, which is within the 100 ms redirect budget.

**When to switch to simple counters:**
- If unique IP count exceeds 100,000 concurrently (sorted set memory > 500 MB).
- If rate limit check latency exceeds 5 ms (window size > 10,000 members).

### EVALSHA vs EVAL

The Lua script is loaded once via `SCRIPT LOAD` during Lambda `init()`, and subsequent calls use `EVALSHA` with the SHA1 hash. This avoids sending the script body on every rate limit check.

If Redis restarts or the script is flushed, `EVALSHA` returns `NOSCRIPT`. The go-redis client handles this automatically by falling back to `EVAL` (which reloads the script).

---

## 5. Negative Cache Effectiveness

### Cache Miss Storm Prevention

Without negative caching, a bot scanning random short codes generates a DynamoDB `GetItem` for every unknown code. At 10K RPS with 5% unknown codes:

| Scenario | DynamoDB Reads/s | Cost Impact |
|---|---|---|
| No negative cache | 500 reads/s (all misses hit DDB) | +250 RCU/s |
| 60s negative cache | ~8 reads/s (first hit per code per minute) | +4 RCU/s |
| **Reduction** | **98.4%** | **~246 RCU/s saved** |

### Negative Cache Configuration

```
Key:   neg:{code}
Value: "1" (1 byte)
TTL:   60 seconds
```

The 60-second TTL is chosen to balance:
- **Protection:** bots retrying the same code within 60s get cached 404s.
- **Freshness:** a link created after a negative cache entry becomes reachable within 60s.

### Edge Case: Code Created After Negative Cache

1. Request for `/{code}` arrives. Code does not exist. `neg:{code}` set with 60s TTL.
2. Owner creates a link with the same `{code}` (custom alias) via API.
3. API Lambda calls `cache.DeleteLink(code)` -- but this only deletes `link:{code}`, not `neg:{code}`.

**Gap:** The negative cache entry is not invalidated on link creation. The link is unreachable for up to 60 seconds.

**Mitigation:** The API Lambda should also call `cache.client.Del(ctx, "neg:" + code)` when creating a link with a custom alias. This is a minor code change in the create handler. For auto-generated codes, the probability of a negative cache entry existing for a random Base62 code is negligible.

---

## 6. Monitoring

### Redis INFO Stats to Watch

```bash
# Connect to ElastiCache (via bastion or VPC-connected instance)
redis-cli -h shorty-redis.xxxxx.cache.amazonaws.com INFO
```

Key sections:

| INFO Section | Metric | Warning | Critical |
|---|---|---|---|
| `stats` | `keyspace_hits` / `keyspace_misses` | Hit rate < 70% | Hit rate < 50% |
| `stats` | `evicted_keys` | > 100/min | > 1,000/min |
| `clients` | `connected_clients` | > 50,000 | > 60,000 |
| `memory` | `used_memory` / `maxmemory` | > 60% | > 80% |
| `memory` | `mem_fragmentation_ratio` | > 1.5 | > 2.0 |
| `stats` | `total_commands_processed` | Baseline + 50% | Baseline + 100% |
| `replication` | `master_link_status` | `down` | `down > 30s` |

### Slow Log Analysis

```bash
# View last 10 slow commands (default threshold: 10ms)
redis-cli SLOWLOG GET 10

# Lower threshold for performance tuning (1ms)
redis-cli CONFIG SET slowlog-log-slower-than 1000
```

Expected slow log entries:
- `EVALSHA` (rate limiter) should never appear (< 2 ms).
- `ZREMRANGEBYSCORE` within Lua: if a sorted set grows very large (> 10,000 members), this could be slow.
- `GET`/`SET` should never appear (< 1 ms).

If `EVALSHA` appears in slow log:
1. Check sorted set sizes: `ZCARD rl:redir:{sample_key}`.
2. If > 10,000 members, the window is too long or the limit too high.
3. The `ZREMRANGEBYSCORE` within the Lua script is the likely culprit.

### CloudWatch Metrics (ElastiCache)

| Metric | Monitoring Purpose | Alarm Threshold |
|---|---|---|
| `CacheHitRate` | Cache effectiveness | < 70% |
| `DatabaseMemoryUsagePercentage` | Memory pressure | > 80% |
| `CurrConnections` | Connection pool sizing | > 50,000 |
| `Evictions` | Eviction pressure | > 1,000/min |
| `ReplicationLag` | Replica health | > 1s |
| `EngineCPUUtilization` | CPU bottleneck | > 90% |
| `NetworkBytesIn` / `NetworkBytesOut` | Network saturation | > 80% of node limit |

### Custom Application Metrics

Emit from the Go cache adapter:

```
shorty.cache.hit_rate        -- per-Lambda cache hit ratio (counter)
shorty.cache.latency_ms      -- per-operation latency histogram
shorty.cache.error_rate      -- connection failures (counter)
shorty.ratelimit.latency_ms  -- EVALSHA roundtrip time
shorty.ratelimit.denied      -- rate-limited requests (counter)
```

---

## 7. Failure Modes

### 7.1 Connection Timeout

**Symptom:** `DialTimeout` (3s) exceeded. Lambda logs show `i/o timeout` on Redis operations.

**Causes:**
- ElastiCache node is in a different AZ than the Lambda subnet.
- Security group does not allow port 6379 from Lambda's security group.
- ElastiCache failover in progress (multi-AZ).

**Impact:**
- Rate limiter: fails open (allows requests). DDoS handled by WAF.
- Cache: falls through to DynamoDB. Redirect latency increases by 3-8 ms per request.

**Recovery:** Automatic once connectivity is restored. go-redis reconnects on next operation.

### 7.2 Pool Exhaustion

**Symptom:** `PoolTimeout` (2s) exceeded. Lambda logs show `redis: connection pool timeout`.

**Causes:**
- `PoolSize` too small for concurrent pipeline operations.
- Slow Redis commands blocking all connections.
- Network latency spike causing connections to be held longer.

**Impact:** Same as connection timeout (fail-open for rate limiting, DynamoDB fallback for cache).

**Mitigation:** With `PoolSize: 2` and single-request Lambda, pool exhaustion should be impossible. If it occurs, the likely cause is a blocked connection (slow Lua script or network issue), not pool sizing.

### 7.3 Eviction Pressure

**Symptom:** `evicted_keys` metric increasing. Cache hit rate dropping.

**Causes:**
- Memory usage exceeds `maxmemory`. LFU evicts least-frequently-used keys.
- Traffic spike brings many new unique codes into cache simultaneously.
- Rate limiter sorted sets consuming excessive memory (many unique IPs).

**Impact:**
- Evicted `link:{code}` keys cause cache misses, increasing DynamoDB load.
- Evicted `rl:redir:{ip_hash}` keys reset rate limit counters (IPs get a fresh window).
- Evicted `neg:{code}` keys cause negative cache misses (minor DynamoDB impact).

**Mitigation:**
1. Upgrade node type (e.g., `cache.t4g.medium` to `cache.r7g.large`).
2. Reduce link cache TTL from 300s to 120s (reduces working set).
3. Reduce rate limit window from 60s to 30s (smaller sorted sets).

### 7.4 Failover (Multi-AZ)

**Symptom:** 10-30 seconds of connection errors during automatic failover.

**Impact:** All Redis operations fail. Rate limiter fails open. Cache falls through to DynamoDB.

**Mitigation:**
- go-redis detects connection loss and reconnects to the new primary endpoint.
- ElastiCache DNS endpoint (`shorty-redis.xxxxx.cache.amazonaws.com`) updates to point to the new primary.
- DNS TTL is 5 seconds; go-redis reconnect typically resolves within 10-15 seconds.

---

## 8. ElastiCache Recommendations

### Cluster Mode: Disabled

Cluster mode is disabled because:
1. **Dataset size:** Total Redis memory is < 130 MB at high traffic. A single shard is sufficient.
2. **Lua script compatibility:** The rate limiter Lua script accesses a single key per invocation. Cluster mode is compatible, but adds operational complexity with no benefit.
3. **Failover simplicity:** Single-shard replication group with one replica provides multi-AZ HA without the complexity of cluster-mode slot management.

**When to enable cluster mode:**
- If memory usage exceeds the largest single-node capacity (e.g., > 100 GB on `cache.r7g.4xlarge`).
- If write throughput exceeds single-node capacity (> 100,000 ops/s on `cache.r7g.large`).
- Neither is likely for Shorty at any projected scale.

### Node Type Selection

| Environment | Node Type | Memory | vCPUs | Monthly Cost |
|---|---|---|---|---|
| Dev/Staging | `cache.t4g.micro` | 0.5 GB | 2 | ~$12 |
| Prod (launch) | `cache.t4g.medium` | 3 GB | 2 | ~$47 |
| Prod (scale) | `cache.r7g.large` | 13 GB | 2 | ~$206 |

**Launch recommendation:** `cache.t4g.medium` with 1 replica (multi-AZ). Estimated memory usage is ~40-130 MB, giving 23-75x headroom. The `t4g` burstable instances are cost-effective for URL shortener workloads where Redis CPU usage is minimal (simple GET/SET + small Lua scripts).

**Upgrade trigger:** Upgrade to `cache.r7g.large` when:
- `DatabaseMemoryUsagePercentage` exceeds 60% sustained.
- `CurrConnections` exceeds 10,000 sustained.
- `EngineCPUUtilization` exceeds 70% sustained (burstable credits depleted).

### Encryption Configuration

Production ElastiCache requires:
- **At-rest encryption:** enabled (AES-256, managed by AWS).
- **In-transit encryption:** enabled (TLS 1.2). The go-redis client must set `TLSConfig` in connection options.
- **AUTH token:** stored in Secrets Manager, rotated quarterly. Set via `redis.Options.Password`.

### Backup Strategy

- **Snapshot retention:** 7 days.
- **Snapshot window:** 03:00-04:00 UTC (low traffic period).
- **Note:** Snapshots are for disaster recovery, not data protection. All Redis data is ephemeral (cache + rate limit counters). The authoritative data is in DynamoDB. A full Redis loss is recoverable: cache rebuilds from DynamoDB organically, rate limit counters reset (brief window of no rate limiting until WAF catches up).
