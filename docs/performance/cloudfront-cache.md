# CloudFront Cache Performance Guide

> Cache behavior analysis for Shorty's redirect responses, TTL strategy,
> cache key design, and geographic performance impact.
> Companion to `docs/aws/cloudfront-config.md` (the infrastructure specification).

---

## 1. Cache Behavior for Redirect 302 Responses

### Current Configuration: No Caching (TTL=0)

The default CloudFront cache behavior for `/*` (redirect endpoint) is configured with `min_ttl=0, default_ttl=0, max_ttl=0`. This means **every redirect request reaches the origin** (API Gateway -> Lambda).

```
Client --> CloudFront (MISS) --> API Gateway --> Redirect Lambda --> 302 Location: {url}
```

### Why Redirects Are Not Cached

Every redirect must be counted as a click. The redirect Lambda:
1. Checks the rate limiter (Redis).
2. Looks up the short code (Redis cache -> DynamoDB fallback).
3. Validates the link is active and under click limit.
4. Increments `click_count` (DynamoDB conditional update via SQS).
5. Publishes a click event to SQS with metadata (IP hash, user agent, referrer, geo).

If CloudFront cached the 302 response, steps 1-5 would be skipped. Click counts would be inaccurate, rate limiting would not apply, and analytics data would be lost.

### Cache Miss Headers

The redirect Lambda sets these headers to prevent caching at every layer:

```
Cache-Control: private, no-cache, no-store, max-age=0, must-revalidate
Pragma: no-cache
Expires: 0
```

CloudFront respects `Cache-Control: no-cache` regardless of TTL settings, but the explicit `max_ttl=0` on the cache policy provides defense-in-depth.

---

## 2. Cache Key Policy

### Redirect Path: Minimal Cache Key

Even though redirects are not cached (TTL=0), the cache key policy affects what CloudFront forwards to the origin.

**Current cache key configuration:**

| Component | Included in cache key | Forwarded to origin |
|---|---|---|
| Path | Yes (always) | Yes |
| Query strings | All (forwarded) | Yes |
| Headers | None (except `Accept-Encoding`) | `Host` via origin request policy |
| Cookies | None | None |

### Why Path-Only Cache Key Is Correct

1. **Short codes are path-based:** `GET /{code}` -- the path alone identifies the resource.
2. **No headers affect response:** The 302 redirect is the same regardless of `User-Agent`, `Accept-Language`, or any other header. Including headers in the cache key would fragment the cache (if caching were enabled in the future).
3. **No cookies needed:** The redirect endpoint is unauthenticated. Cookies are not forwarded, which prevents cache fragmentation and avoids leaking session data.
4. **Query strings forwarded:** Future features may use query parameters (e.g., `?preview=true` for link preview). Forwarding all query strings ensures compatibility without cache key bloat (since TTL=0).

### API Path: No Cache

The `/api/*` cache behavior uses `CachingDisabled` policy. All headers except `Host` are forwarded. This is correct because API responses are user-specific (authenticated) and dynamic.

---

## 3. TTL Strategy for 302 Caching

### Current: TTL=0 (No Caching)

All redirect responses bypass CloudFront cache. Every request is an origin fetch.

### Post-MVP: Selective Caching for Analytics-Disabled Links

Links created with `analytics: false` (opt-out of click tracking) could be cached at CloudFront. The Lambda would set:

```
Cache-Control: public, max-age=86400
```

CloudFront would cache the 302 for 24 hours. Benefits:
- Zero origin load for popular analytics-disabled links.
- Redirect latency drops from ~30-50 ms (origin fetch) to ~1-5 ms (edge cache hit).

**Implementation requirements:**
- Lambda sets `Cache-Control` dynamically based on link configuration.
- CloudFront `max_ttl` must be raised from 0 to 86400 on the default behavior.
- A separate cache behavior `/na/*` (no-analytics prefix) is cleaner than dynamic TTL on `/*`.

### Permanent Redirect (301) Caching

Links configured as permanent redirects (301) could also be cached:

```
Cache-Control: public, max-age=604800
```

Browsers cache 301 responses indefinitely by default. Adding CloudFront caching aligns with this behavior and reduces origin load.

**Risk:** A cached 301 cannot be "uncached" at the browser level. Only use for links that are truly permanent (e.g., domain migrations).

### TTL Summary Table

| Link Type | HTTP Status | Cache-Control | CloudFront TTL | Cached |
|---|---|---|---|---|
| Standard (analytics on) | 302 | `private, no-cache, no-store` | 0 | No |
| Analytics disabled (post-MVP) | 302 | `public, max-age=86400` | 86400 (24h) | Yes |
| Permanent redirect (post-MVP) | 301 | `public, max-age=604800` | 604800 (7d) | Yes |
| Expired / Inactive | 410 | `public, max-age=300` | 300 (5m) | Yes |
| Not found | 404 | `public, max-age=60` | 60 (1m) | Yes |

**410 and 404 responses are cached** even today (via CloudFront custom error response configuration: 410 for 300s, 404 for 60s). This prevents repeated origin fetches for dead links and scanning bots.

---

## 4. Origin Shield

### Current: Disabled

Origin Shield is a centralized caching layer between CloudFront edge locations and the origin. It reduces origin load by consolidating cache misses from multiple edge locations into a single request to the origin.

### Why Disabled for MVP

1. **No redirect caching:** With TTL=0 on redirects, Origin Shield provides no benefit -- every request reaches the origin regardless.
2. **Single region:** MVP deploys to a single AWS region. Origin Shield helps most when the origin is distant from many edge locations.
3. **Cost:** Origin Shield adds $0.0090 per 10,000 requests (us-east-1). At 10K RPS, this is ~$23/day = ~$700/month for zero caching benefit.

### When to Enable Origin Shield

Enable Origin Shield when:
1. Redirect caching is implemented (analytics-disabled links or 301 redirects).
2. The distribution uses `PriceClass_All` (global edge locations).
3. Static asset cache hit ratio is below target (though static assets are served from S3, not Lambda).

**Recommended Origin Shield region:** Same as the API Gateway region (e.g., `us-east-1`).

---

## 5. Cache Hit Ratio Targets

### Current State (TTL=0 Redirects)

With redirects uncached, the overall CloudFront cache hit ratio for the redirect path is **0%**. This is by design.

The only cached responses are:
- Static assets (`/static/*`): target **> 95%** cache hit ratio (assets are versioned, long TTL).
- Error pages (404, 410): target **> 80%** for repeated requests to the same dead link.

### Target State (Post-MVP with Selective Caching)

If analytics-disabled links represent 10% of traffic:

| Traffic segment | % of total | Cache hit ratio | Origin requests saved |
|---|---|---|---|
| Analytics-enabled redirects | 90% | 0% | 0% |
| Analytics-disabled redirects | 10% | ~85% | 8.5% of total |
| Static assets | < 1% | > 95% | < 1% |
| Error responses | ~5% | ~80% | ~4% |
| **Blended** | 100% | **~12-13%** | **~12-13%** |

### Monitoring Cache Hit Ratio

CloudWatch metrics for the CloudFront distribution:

| Metric | Description | Target |
|---|---|---|
| `CacheHitRate` | Percentage of viewer requests served from cache | See above per segment |
| `OriginLatency` | Time from CloudFront to origin and back | < 50 ms p99 |
| `4xxErrorRate` | Client error percentage | < 5% |
| `5xxErrorRate` | Server error percentage | < 0.1% |
| `TotalErrorRate` | Combined error percentage | < 5% |

---

## 6. Cache Invalidation

### Redirect Path: No Invalidation Needed

With TTL=0, there is no cached redirect content to invalidate. Link updates and deletions take effect immediately because every request reaches the origin.

### Post-MVP: Invalidation for Cached Redirects

When selective caching is enabled, link updates and deletions require cache invalidation:

**Option A: Path-based invalidation**
```
aws cloudfront create-invalidation \
  --distribution-id E1234567890 \
  --paths "/{code}"
```
- Cost: first 1,000 paths/month free, then $0.005 per path.
- Propagation time: 1-5 minutes globally.
- Suitable for individual link updates/deletions.

**Option B: Cache-Control header on update**

When a link is updated, the API Lambda publishes an invalidation event. A separate Lambda subscriber calls the CloudFront invalidation API. This decouples invalidation from the API request path.

**Option C: Short TTL instead of invalidation**

Use `max-age=300` (5 minutes) instead of `max-age=86400` for cacheable redirects. Accept 5 minutes of staleness in exchange for zero invalidation complexity. This is the recommended approach for Shorty because:
- Link destinations rarely change.
- 5 minutes of staleness is acceptable for analytics-disabled links.
- Zero operational overhead.

### Error Page Invalidation

Error pages (404, 410) are cached with short TTLs (60s, 300s). When a new link is created with a code that previously returned 404, the cached 404 response serves for up to 60 seconds. This is the same window as the Redis negative cache TTL (60s) and is acceptable.

No explicit invalidation is needed for error pages.

---

## 7. Geographic Distribution Impact

### Edge Location Latency

CloudFront's 400+ edge locations terminate TLS close to the user, reducing TLS handshake latency by 50-80% compared to a direct origin connection.

| User location | Edge -> Origin (us-east-1) | Direct to Origin | Savings |
|---|---|---|---|
| New York | ~5 ms | ~10 ms (TLS) | ~5 ms |
| London | ~75 ms | ~150 ms (TLS) | ~75 ms |
| Tokyo | ~150 ms | ~250 ms (TLS) | ~100 ms |
| Sydney | ~200 ms | ~350 ms (TLS) | ~150 ms |

**Note:** These are network round-trip savings only. The origin fetch (Lambda execution) is not affected by CloudFront geography. Total redirect latency = edge TLS termination + edge-to-origin network + Lambda execution.

### Price Class Impact

| Price Class | Regions | Monthly cost at 10M requests | Coverage |
|---|---|---|---|
| `PriceClass_100` (current) | NA + EU | ~$85 | 60% of global users |
| `PriceClass_200` | NA + EU + Asia | ~$120 | 85% of global users |
| `PriceClass_All` | All regions | ~$150 | 100% |

**MVP recommendation:** `PriceClass_100` covers North America and Europe. Users in Asia, South America, and Africa are routed to the nearest included edge location, adding 50-150 ms network latency.

**Upgrade trigger:** When > 20% of traffic originates from excluded regions (visible in CloudFront access logs geographic breakdown).

### HTTP/2 and HTTP/3

The CloudFront distribution is configured with `http_version = "http2and3"`:

- **HTTP/2:** Multiplexed connections reduce latency for users making multiple requests (e.g., redirect + static assets).
- **HTTP/3 (QUIC):** Reduces connection establishment time by 1 RTT compared to TCP+TLS. Particularly beneficial for mobile users and high-latency connections. CloudFront automatically negotiates HTTP/3 with supporting clients via the `Alt-Svc` header.

### Connection Reuse

CloudFront maintains persistent connections to the origin (API Gateway). The `origin_keepalive_timeout: 5s` setting keeps connections warm between requests. At 10K RPS, CloudFront maintains a pool of ~100-200 persistent connections to the origin, avoiding TCP+TLS handshake overhead (~30 ms) for each request.

---

## 8. Performance Optimization Roadmap

### Phase 1 (MVP): Current Configuration

- Redirect TTL=0, no caching.
- Static assets cached with 24h TTL.
- Error pages cached with short TTLs.
- `PriceClass_100` (NA + EU).
- Origin Shield disabled.

**Expected CloudFront overhead:** 1-5 ms per request (TLS termination + edge routing). Offset by faster TLS handshake for end users.

### Phase 2 (Post-MVP): Selective Caching

- Analytics-disabled links cached at CloudFront (5-minute TTL).
- 301 permanent redirects cached (7-day TTL).
- Origin Shield enabled if global distribution is active.
- Estimated origin load reduction: 10-15%.

### Phase 3 (Scale): Geographic Optimization

- Upgrade to `PriceClass_All`.
- Enable Origin Shield in us-east-1.
- Consider multi-region origin failover (API Gateway in us-east-1 + eu-west-1).
- Target: < 50 ms p99 redirect latency for NA/EU, < 200 ms for Asia-Pacific.
