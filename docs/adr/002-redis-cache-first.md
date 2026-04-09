# ADR-002: Redis Cache-First Redirect Strategy

## Status

Accepted

## Context

The redirect Lambda (`GET /{code}`) is the hot path -- it must serve 10,000 RPS with
p99 < 100 ms (RFC-0001). DynamoDB GetItem alone is typically 3-10 ms within the same
region, but under load with burst traffic, tail latencies can reach 20-50 ms.
Adding network round-trip variance, we need a faster primary lookup path.

Popular short links follow a power-law distribution: a small percentage of codes
account for the majority of redirects. This makes caching highly effective.

## Decision

We use a **cache-aside (lazy-loading)** pattern with ElastiCache Redis as the primary
lookup for the redirect Lambda:

1. **Read path:** Redis GET first. On hit, return immediately (sub-1ms). On miss,
   fetch from DynamoDB, populate Redis, then return.

2. **Cache key format:** `link:{code}` -- stores a JSON-serialized link record.

3. **TTL policy:**
   - Default cache TTL: 5 minutes for active links.
   - Links with `expires_at`: cache TTL = min(5 min, time until expiry).
   - Deactivated/deleted links: cache a tombstone (`{"gone":true}`) for 60 seconds
     to prevent cache stampede on recently expired links.

4. **Cache invalidation:** Explicit `DEL link:{code}` on link update or delete via
   the API Lambda. No pub/sub or cache bus -- the API Lambda calls Redis directly
   after a successful DynamoDB write.

5. **Cache warm-up:** None at launch. The cache populates organically from traffic.
   Provisioned concurrency on the redirect Lambda ensures warm DynamoDB connections
   even on cold cache.

6. **Serialization:** JSON (not Protocol Buffers). The link record is small (~200
   bytes). JSON keeps debugging simple (`redis-cli GET link:abc123` returns
   human-readable data).

```go
func (s *RedirectService) Resolve(ctx context.Context, code string) (*Link, error) {
    // 1. Try cache
    link, err := s.cache.Get(ctx, "link:"+code)
    if err == nil {
        return link, nil // cache hit
    }
    // 2. Fallback to DynamoDB
    link, err = s.store.GetLink(ctx, code)
    if err != nil {
        return nil, err
    }
    // 3. Populate cache (async-safe, fire-and-forget)
    _ = s.cache.Set(ctx, "link:"+code, link, s.ttlFor(link))
    return link, nil
}
```

## Consequences

**Positive:**
- Cache hits serve in < 1 ms, well within the 100 ms p99 budget.
- DynamoDB read capacity is reduced by 80-95% for the redirect path (based on
  typical power-law traffic distributions).
- Simple operational model: Redis is a pure cache, no durability requirements.
  If Redis fails, the redirect Lambda falls back to DynamoDB gracefully.

**Negative:**
- Cache staleness window of up to 5 minutes for link updates (acceptable for URL
  shortener -- link targets rarely change).
- Redis is a single point of partial degradation. If Redis is unreachable,
  all traffic hits DynamoDB, which may throttle under 10K RPS without sufficient
  provisioned capacity.
- ElastiCache in VPC means the redirect Lambda must be VPC-attached, adding
  ~300 ms to cold starts (mitigated by provisioned concurrency = 2).

## Alternatives Considered

**Write-through cache:**
Rejected. Write-through adds latency to link creation (must write Redis + DynamoDB).
Since creation is not latency-sensitive (p99 < 300 ms target) this isn't a performance
concern, but it adds complexity: the API Lambda must know about the cache, and cache
population happens for links that may never receive traffic.

**DynamoDB DAX (in-memory cache):**
Rejected. DAX is a managed DynamoDB cache, but it operates at the item level with
limited TTL control. We need per-link TTL logic (tombstones for expired links,
shorter TTL near expiry). DAX also requires VPC and adds ~$0.10/hr per node minimum,
comparable to a small Redis instance but less flexible.

**CloudFront caching of 301 redirects:**
Rejected for MVP. Caching at the CDN layer would bypass the Lambda entirely, preventing
click counting. A future feature flag could enable 301 + CloudFront caching for links
where analytics is not needed. See RFC-0001.

**No cache (DynamoDB only):**
Rejected. While DynamoDB can handle the throughput, tail latencies under burst traffic
would violate the p99 < 100 ms target without significant over-provisioning of read
capacity units, which is more expensive than a small Redis cluster.
