# Redirect Critical Path Analysis

> Target: p99 < 100 ms (NFR 3.1 in requirements-init.md)
> Handler: `cmd/redirect/main.go` -> `RedirectHandler.Handle()`

---

## Request Flow Diagram

```
GET /{code}
  |
  [1] extractCode(rawPath)             ~0.001 ms  (string ops, no alloc needed)
  |
  [2] Extract + hash client IP         ~0.005 ms  (SHA-256, CPU-bound)
  |                                                 cmd/redirect/main.go:113-121
  |
  [3] Rate limit check (Redis Lua)     ~1-2 ms    (single EVALSHA roundtrip)
  |                                                 internal/ratelimit/ratelimit.go:155
  |
  [4] Cache lookup (Redis GET)         ~0.5-1 ms  (single GET roundtrip)
  |                                                 internal/cache/cache.go:71
  |--- CACHE HIT ---> skip to [8]
  |
  [5] Negative cache check (Redis GET) ~0.5-1 ms  (only on cache miss)
  |                                                 internal/cache/cache.go:167
  |--- NEG HIT -----> return 404
  |
  [6] DynamoDB GetItem                 ~3-8 ms    (eventually consistent read)
  |                                                 internal/store/store.go:100
  |
  [7] Cache warm (Redis SET)           ~0.5-1 ms  (synchronous, not fire-and-forget!)
  |                                                 internal/cache/cache.go:106
  |
  [8] Validation: TTL + IsActive       ~0.001 ms  (pure Go, no I/O)
  |                                                 cmd/redirect/main.go:165-171
  |
  [9] Password check (if applicable)   ~0.005 ms  (SHA-256 hash compare)
  |                                                 cmd/redirect/main.go:176-193
  |
  [10] Click increment (DynamoDB)      ~3-8 ms    (conditional UpdateItem)
  |                                                 internal/store/store.go:320
  |
  [11] SQS publish (async goroutine)   ~0 ms      (non-blocking, goroutine)
  |                                                 cmd/redirect/main.go:206
  |
  [12] Return 302 + Location header    ~0.01 ms
```

---

## Latency Budget

| Step | Cache HIT | Cache MISS | Notes |
|---|---|---|---|
| Code extraction + IP hash | 0.01 ms | 0.01 ms | Pure CPU, negligible |
| Rate limit (Redis EVALSHA) | 1-2 ms | 1-2 ms | Single Lua script roundtrip |
| Cache lookup (Redis GET) | 0.5-1 ms | 0.5-1 ms | Returns nil on miss |
| Negative cache check | -- | 0.5-1 ms | Only on cache miss |
| DynamoDB GetItem | -- | 3-8 ms | Eventually consistent, no projection |
| Cache warm (Redis SET) | -- | 0.5-1 ms | Currently synchronous (see finding F1) |
| Validation (TTL, active) | 0.001 ms | 0.001 ms | Pure Go |
| Click increment (DynamoDB) | 3-8 ms | 3-8 ms | Conditional UpdateItem |
| SQS publish | 0 ms | 0 ms | Async goroutine, never blocks |
| Response write | 0.01 ms | 0.01 ms | Headers only (302) |
| **Total** | **5-11 ms** | **8-20 ms** | Well within 100 ms budget |

---

## Network Latency Assumptions

| Path | Expected Latency | Condition |
|---|---|---|
| Lambda -> ElastiCache Redis | < 1 ms | Same VPC, same AZ, VPC endpoint |
| Lambda -> DynamoDB | 2-5 ms | Same region, VPC endpoint configured |
| Lambda -> SQS | 3-8 ms | Same region (irrelevant: async) |
| API Gateway -> Lambda | < 1 ms | Managed integration, warm invoke |

These assume:
- ElastiCache node is in the same AZ as the Lambda subnet
- DynamoDB VPC endpoint (gateway type) is configured in the VPC route table
- Redis connection pool is initialized in `init()` (confirmed: `cmd/redirect/main.go:386-390`)

---

## Cold Start Impact Analysis

### Current Cold Start Budget

| Phase | Duration | Source |
|---|---|---|
| Lambda runtime init | ~50 ms | Go ARM64 binary, minimal |
| AWS SDK config load | ~100 ms | `awsconfig.LoadDefaultConfig` (main.go:371) |
| DynamoDB client init | ~5 ms | `dynamodb.NewFromConfig` (main.go:378) |
| Redis connection | ~20 ms | TCP + AUTH to ElastiCache (main.go:385-390) |
| SQS client init | ~5 ms | `sqs.NewFromConfig` (main.go:395) |
| Secrets Manager fetch | ~100-200 ms | `GetSecretValue` API call (main.go:402-410) |
| **Total cold start** | **~300-400 ms** | First request only |

### Mitigation: Provisioned Concurrency

The redirect Lambda uses `provisioned_concurrency = 2`. This means:
- 2 warm instances always ready (no cold start for the first 2 concurrent requests)
- Additional concurrent requests beyond 2 will hit cold start
- At 10,000 RPS steady state, Lambda concurrency ~= RPS * avg_duration = 10000 * 0.015s = 150 instances
- Autoscaling provisioned concurrency should track the 150 target

### Cold Start Optimization Opportunities

1. **Secrets Manager fetch in init()** (`main.go:400-410`): This is the biggest cold start contributor at 100-200 ms. Consider caching the secret in a Lambda extension or using the `IP_HASH_SALT` environment variable (encrypted via KMS) to avoid the API call entirely.

2. **Redis pool size** (`main.go:389`): `PoolSize: 10` is oversized for a Lambda that processes one request at a time. Reduce to `PoolSize: 1` to save memory and connection setup time.

---

## Findings and Optimization Opportunities

### F1: Cache warm is synchronous on the critical path (MEDIUM)

**Location:** `cmd/redirect/main.go:159`
```go
_ = h.cache.SetLink(ctx, code, link, 0)
```

This Redis SET happens synchronously before validation and click increment. While it only costs ~0.5 ms, it is not strictly necessary on the response path. The link data is already retrieved from DynamoDB and will be used for the current request regardless.

**Recommendation:** Move to a fire-and-forget goroutine, matching the pattern used for SQS publish:
```go
go func() { _ = h.cache.SetLink(ctx, code, link, 0) }()
```

Savings: ~0.5-1 ms on cache MISS path.

### F2: Negative cache check adds a Redis roundtrip on miss (LOW)

**Location:** `cmd/redirect/main.go:144`

On cache miss, the handler makes two sequential Redis calls: `GetLink` (miss) then `IsNegative`. These could be pipelined into a single roundtrip using `redis.Pipeline()`.

**Recommendation:** Create a `GetLinkOrNegative(ctx, code)` method on the cache that pipelines both GETs.

Savings: ~0.5 ms on cache MISS path.

### F3: fmt.Sprintf in rate limit key construction (LOW)

**Location:** `cmd/redirect/main.go:123`
```go
rlKey := fmt.Sprintf("rl:redirect:%s", ipHash)
```

`fmt.Sprintf` causes a heap allocation. For a fixed-prefix key, string concatenation is zero-alloc:
```go
rlKey := "rl:redirect:" + ipHash
```

Savings: 1 allocation per request (~64 bytes).

### F4: uuid.New() in rate limiter creates allocations (MEDIUM)

**Location:** `internal/ratelimit/ratelimit.go:153`
```go
requestID := fmt.Sprintf("%d:%s", nowMs, uuid.New().String()[:8])
```

Each rate limit check generates a UUID (128-bit random + formatting). The `uuid.New().String()[:8]` pattern:
1. Generates full 36-char UUID string
2. Slices to first 8 chars (wastes 28 chars)
3. `fmt.Sprintf` concatenates with timestamp

**Recommendation:** Use `rand.Read` with a pre-allocated buffer or a counter-based ID for the sorted set member. The uniqueness requirement is modest (only needs to be unique within one sliding window for one key).

### F5: DynamoDB click increment is on the critical path (HIGH)

**Location:** `cmd/redirect/main.go:197`
```go
ok, err := h.store.IncrementClickCount(ctx, code, link.MaxClicks)
```

`IncrementClickCount` (`store.go:320`) performs a conditional DynamoDB UpdateItem that costs 3-8 ms. This is the single largest latency contributor on both HIT and MISS paths.

For links without `max_clicks` (the common case), the click count is only used for analytics and does not gate the redirect. Consider:
- **Option A:** For links with `max_clicks == nil`, move the increment to the SQS worker (batch 100 increments into one UpdateItem). The redirect returns immediately.
- **Option B:** Use a Redis counter for click tracking and periodically flush to DynamoDB. The Lua rate limit script pattern already demonstrates this approach.

**Risk:** Option A means `click_count` in DynamoDB lags by seconds. Acceptable for analytics; not acceptable for `max_clicks` enforcement.

**Recommendation:** Implement Option A for links without `max_clicks`; keep synchronous increment only when `max_clicks != nil`. This saves 3-8 ms on the majority of redirects.

### F6: json.Marshal in SQS goroutine allocates on each click (LOW)

**Location:** `cmd/redirect/main.go:262-264`
```go
clickEvent := map[string]interface{}{...}
body, err := json.Marshal(clickEvent)
```

Using `map[string]interface{}` forces reflection-based marshaling. A typed struct with `json.Marshal` or a `sync.Pool`'d `json.Encoder` would be faster. However, this runs in a background goroutine and does not affect redirect latency. Low priority.

### F7: Password form template execution allocates (LOW)

**Location:** `cmd/redirect/main.go:352-353`
```go
var buf strings.Builder
_ = passwordTmpl.Execute(&buf, struct{ Code string }{Code: code})
```

Each password-protected redirect allocates a new `strings.Builder`. Since password-protected links are rare, this is acceptable. A `sync.Pool` for the builder would eliminate the allocation but adds complexity for minimal gain.

---

## Summary: Latency Budget After Optimizations

| Step | Cache HIT (current) | Cache HIT (optimized) | Notes |
|---|---|---|---|
| Rate limit | 1-2 ms | 1-2 ms | No change |
| Cache lookup | 0.5-1 ms | 0.5-1 ms | No change |
| Click increment | 3-8 ms | 0 ms* | *Deferred to worker when max_clicks=nil |
| **Total** | **5-11 ms** | **2-3 ms** | 60-70% reduction |

| Step | Cache MISS (current) | Cache MISS (optimized) | Notes |
|---|---|---|---|
| Rate limit | 1-2 ms | 1-2 ms | No change |
| Cache lookup + neg check | 1-2 ms | 0.5-1 ms | Pipeline both GETs |
| DynamoDB GetItem | 3-8 ms | 3-8 ms | No change |
| Cache warm | 0.5-1 ms | 0 ms | Fire-and-forget goroutine |
| Click increment | 3-8 ms | 0 ms* | *Deferred to worker when max_clicks=nil |
| **Total** | **8-20 ms** | **4-11 ms** | 45-50% reduction |
