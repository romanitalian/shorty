# Runbook: CloudFront Cache Drop

**Alert:** Manual detection / cache hit ratio monitoring
**Severity:** SEV3 (cache miss rate elevated) / SEV2 (origin overloaded due to cache miss)
**SLO Impact:** CloudFront cache misses increase origin (API Gateway + Lambda) load, which can push redirect p99 latency above the 100ms target and increase Lambda concurrency costs. A sustained cache drop may cause Lambda throttling.

---

## Symptoms

- CloudFront cache hit ratio drops significantly (below 50% or a sudden drop from baseline).
- Origin request count spikes (CloudWatch `OriginRequests` metric).
- Redirect Lambda invocation count increases unexpectedly.
- Redirect p99 latency elevated (more requests hitting origin).
- Lambda throttling due to increased origin load.
- CloudFront `CacheMissRate` metric elevated.

---

## Diagnosis

### 1. Check CloudFront cache statistics

```bash
# Get distribution ID
DIST_ID=$(aws cloudfront list-distributions \
  --query "DistributionList.Items[?Comment=='shorty'].Id" --output text)

# Cache hit ratio (last 1 hour, requires real-time metrics enabled)
aws cloudwatch get-metric-statistics \
  --namespace AWS/CloudFront \
  --metric-name CacheHitRate \
  --dimensions Name=DistributionId,Value=$DIST_ID Name=Region,Value=Global \
  --start-time "$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 300 --statistics Average

# Origin requests vs total requests
aws cloudwatch get-metric-statistics \
  --namespace AWS/CloudFront \
  --metric-name Requests \
  --dimensions Name=DistributionId,Value=$DIST_ID Name=Region,Value=Global \
  --start-time "$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 300 --statistics Sum
```

### 2. Check cache behavior configuration

```bash
# Get current distribution config
aws cloudfront get-distribution-config --id $DIST_ID \
  --query 'DistributionConfig.CacheBehaviors'

# Check default cache behavior (redirect path)
aws cloudfront get-distribution-config --id $DIST_ID \
  --query 'DistributionConfig.DefaultCacheBehavior.{TTL: DefaultTTL, MaxTTL: MaxTTL, MinTTL: MinTTL, CachePolicyId: CachePolicyId, Compress: Compress, ViewerProtocolPolicy: ViewerProtocolPolicy}'
```

### 3. Check for recent invalidations

```bash
# List recent invalidation requests
aws cloudfront list-invalidations --distribution-id $DIST_ID \
  --query 'InvalidationList.Items[:10].[Id, Status, CreateTime]'

# Get details of a specific invalidation
aws cloudfront get-invalidation --distribution-id $DIST_ID --id {invalidation_id}
```

A recent `/*` wildcard invalidation would explain a sudden cache drop.

### 4. Check origin health

```bash
# API Gateway 5xx errors (origin returning errors = CloudFront won't cache)
aws cloudwatch get-metric-statistics \
  --namespace AWS/ApiGateway \
  --metric-name 5XXError \
  --dimensions Name=ApiId,Value={api_id} \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum

# Origin latency
aws cloudwatch get-metric-statistics \
  --namespace AWS/CloudFront \
  --metric-name OriginLatency \
  --dimensions Name=DistributionId,Value=$DIST_ID Name=Region,Value=Global \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics p99
```

### 5. Check response headers for cacheability

```bash
# Test a known short code to verify Cache-Control headers
curl -sI "https://{cloudfront_domain}/{known_code}" | grep -i 'cache-control\|x-cache\|age\|vary'
```

Expected response:
- `X-Cache: Hit from cloudfront` (cached) or `Miss from cloudfront` (not cached)
- `Cache-Control: public, max-age=300` (or similar)
- `Vary` header should be minimal (excessive Vary values reduce cache hit ratio)

### 6. Check if origin is returning non-cacheable responses

```bash
# CloudWatch Insights: check redirect response headers
filter @message like /shorty-redirect/
| fields @timestamp, @status, @message
| filter @status >= 300 and @status < 400
| limit 20
```

If the redirect Lambda is returning `Cache-Control: no-cache` or `private`, CloudFront will not cache the response.

---

## Mitigation

### Step 1: Verify cache behavior configuration

Ensure the redirect path (`GET /{code}`) has appropriate caching:

```bash
# Check the cache policy attached to the default cache behavior
aws cloudfront get-cache-policy --id {cache_policy_id}
```

The redirect should use a cache policy with:
- **TTL:** MinTTL >= 60s, DefaultTTL = 300s, MaxTTL = 86400s
- **Cache key:** Only the URL path (no query strings, no cookies, minimal headers)
- **Compression:** Enabled (gzip, br)

### Step 2: Fix origin response headers

If the Lambda is returning non-cacheable headers, update the Lambda to include proper `Cache-Control`:

```
Cache-Control: public, max-age=300
```

For redirects (301/302), CloudFront respects the origin's `Cache-Control` header. Ensure the redirect Lambda sets this.

### Step 3: Remove excessive Vary headers

If the `Vary` header includes values like `Accept-Encoding, User-Agent, Cookie`, each unique combination creates a separate cache entry, fragmenting the cache.

The redirect response should have `Vary: Accept-Encoding` at most.

### Step 4: Wait for cache to warm (if invalidation was the cause)

If a wildcard invalidation caused the drop, the cache will rebuild naturally as requests come in. Monitor the cache hit ratio over the next 30-60 minutes.

### Step 5: Pre-warm cache for high-traffic codes (optional)

```bash
# If known high-traffic codes exist, pre-warm them
for code in abc123 xyz789 top10; do
  curl -s "https://{cloudfront_domain}/$code" > /dev/null
done
```

---

## Prevention

1. **Avoid wildcard invalidations:** Never use `/*` invalidation unless absolutely necessary. Invalidate specific paths (`/abc123`) instead.
2. **Cache-Control headers:** Ensure all redirect responses include `Cache-Control: public, max-age=300`. This should be set in the redirect Lambda handler.
3. **Cache policy review:** Audit the CloudFront cache policy quarterly. The cache key should be as simple as possible (URL path only for redirects).
4. **Origin response monitoring:** Alert on CloudFront `OriginRequests` spike relative to total `Requests`. A ratio change indicates cache effectiveness change.
5. **Vary header discipline:** The redirect Lambda should not set `Vary` headers beyond `Accept-Encoding`. Review any middleware that adds headers.
6. **Real-time metrics:** Enable CloudFront real-time metrics for cache hit ratio monitoring at 1-minute granularity.

---

## Escalation

| Condition | Action |
|-----------|--------|
| Cache hit ratio < 20% for > 30 min | Engineering Lead -- verify cache configuration has not been accidentally changed |
| Origin overloaded (Lambda throttling) due to cache miss | Follow [lambda-throttling.md](lambda-throttling.md) to increase Lambda concurrency while cache rebuilds |
| CloudFront distribution misconfigured | SRE team -- review Terraform config in `deploy/terraform/` |
| CloudFront service issue suspected | Open AWS Support case |
| Unauthorized invalidation request | Security team -- review CloudFront API access logs in CloudTrail |
