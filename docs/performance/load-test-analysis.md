# Load Test Analysis

**Analyst:** Performance Engineer
**Source data:** `docs/qa/load-test-report.md` (2026-04-05, commit `a3f81d2`)
**Environment:** Local (LocalStack + Redis + Docker Compose, macOS Apple Silicon)
**Cross-references:** `docs/performance/critical-path.md`, `docs/performance/lambda-sizing.md`, `docs/performance/redis.md`, `docs/performance/dynamodb.md`

---

## 1. Results Summary

### 1.1 SLO Compliance Across All Scenarios

| Scenario | Peak RPS | Redirect p50 | Redirect p95 | Redirect p99 | p99 < 100 ms? | Error Rate | Duration |
|---|---|---|---|---|---|---|---|
| Baseline | 1,000 | 10.12 ms | 35.42 ms | 67.81 ms | PASS (32% headroom) | 0.025% | 5 min 40 s |
| Stress | 10,000 | 16.21 ms | 64.21 ms | 85.42 ms | PASS (15% headroom) | 0.052% | 9 min |
| Spike | 5,000 | 14.82 ms | 98.42 ms | 168.42 ms | FAIL (68% over) | 0.368% | 2 min 16 s |
| Soak | 500 | 9.42 ms | 34.21 ms | 62.14 ms | PASS (38% headroom) | 0.010% | 31 min 30 s |

**Verdict:** The 100 ms p99 SLO is met under steady-state load at all tested RPS levels (baseline, stress sustained, soak). The SLO is violated only during the spike scenario's cold-start window (first 15 seconds of a 0-to-5K-RPS surge). Under relaxed spike thresholds (p99 < 500 ms), all scenarios pass.

### 1.2 Non-Redirect Endpoints

| Endpoint | Baseline p99 | Stress p99 | Soak p99 | Target |
|---|---|---|---|---|
| Create (`POST /api/v1/shorten`) | 145.21 ms | 218.42 ms | 134.21 ms | < 300 ms |
| Stats (`GET /api/v1/links/{code}/stats`) | 98.42 ms | 178.42 ms | 92.14 ms | < 500 ms |

Both non-redirect endpoints meet their targets across all scenarios with comfortable margins.

### 1.3 Error Budget

| Scenario | Error Rate | Budget (threshold) | Budget consumed |
|---|---|---|---|
| Baseline | 0.025% | 0.1% | 25% |
| Stress | 0.052% | 5.0% | 1% |
| Spike | 0.368% | 5.0% | 7.4% |
| Soak | 0.010% | 0.5% | 2% |

Error budgets are healthy. The spike scenario's 0.368% error rate is composed of 682 errors during the surge (cold starts + connection pool saturation) and 18 residual errors during recovery.

---

## 2. Bottleneck Analysis

### 2.1 Primary Bottleneck: Lambda Cold Starts (Spike Scenario)

**Severity:** HIGH -- only factor causing SLO violation.

**Evidence:**
- Spike redirect p99 = 168.42 ms (68% above the 100 ms target).
- 47 estimated concurrent cold starts during the 0-to-5K surge.
- Individual cold-start requests measured at 200-800 ms.
- Errors concentrated in the first 10 seconds of the surge.
- After warm instances stabilized, p99 dropped below 65 ms (recovery phase).

**Root cause analysis (from `critical-path.md`):**
- Go ARM64 Lambda cold start is ~300-400 ms total.
- Largest contributors: AWS SDK config load (~100 ms), Secrets Manager fetch (~100-200 ms), Redis pool init (~20 ms).
- With provisioned concurrency = 2, only the first 2 concurrent requests avoid cold start. At 5K RPS (requiring ~75 concurrent instances at 15 ms avg duration), 73 instances must cold-start simultaneously.

**Latency decomposition during cold start:**
```
Request during cold start (worst case):
  Lambda init                   300-400 ms
  Rate limit (Redis EVALSHA)      1-2 ms
  Cache lookup (Redis GET)        0.5-1 ms
  Validation + response            0.01 ms
  --------------------------------
  Total:                        ~302-403 ms (explains the 168 ms p99 -- 
                                 many but not all requests hit cold starts)
```

### 2.2 Secondary Bottleneck: DynamoDB Throttling (Stress Scenario)

**Severity:** MEDIUM -- causes errors above 7,200 RPS but does not violate redirect SLO.

**Evidence:**
- First 5xx errors appeared at ~7,200 RPS.
- Breaking point (> 1% error rate) at ~8,500 RPS.
- 174 total errors, 842 dropped iterations.
- Errors were on create operations, not redirects.

**Root cause:** LocalStack DynamoDB lacks adaptive capacity and auto-scaling. In production with on-demand capacity, DynamoDB scales to 2x previous peak within 30 minutes. However, first-time traffic spikes above 4,000 WCU/s (the initial on-demand burst) will throttle until adaptive capacity kicks in.

**Why redirects survived:** The Redis cache-first pattern (see `critical-path.md` step [4]) shields redirect reads from DynamoDB pressure. Cache hits bypass DynamoDB entirely. Cache misses hit DynamoDB reads, which are cheaper (0.5 RCU per eventually consistent read vs. 1 WCU per write) and served from separate capacity.

### 2.3 Tertiary Bottleneck: Redis Connection Pool Saturation (Spike Scenario)

**Severity:** LOW -- contributed 10-40 ms latency during the first seconds of the spike only.

**Evidence:**
- Visible in `http_req_blocked` p99 spike during the first 10 seconds.
- Correlates with 47 new Lambda instances establishing Redis connections simultaneously.
- Resolved within ~5 seconds as connection pools stabilized.

**Root cause (from `redis.md`):** Current `PoolSize: 10` per Lambda instance means 47 cold-starting instances attempt to establish up to 470 connections in a burst. The actual bottleneck is TCP handshake + AUTH roundtrip time (~20 ms per connection), not Redis server capacity (65,000 connection limit on ElastiCache `cache.t4g.small`).

### 2.4 Non-Bottleneck: DynamoDB Click Increment on Critical Path

**Severity:** MEDIUM (latency, not errors) -- currently masked by overall headroom.

**Evidence (from `critical-path.md` F5):**
- `IncrementClickCount` adds 3-8 ms to every redirect (both cache HIT and MISS paths).
- At baseline, redirect p99 is 67.81 ms. Removing this synchronous DynamoDB call would save 3-8 ms, reducing p99 to ~60-64 ms.
- This is not causing SLO violations today, but it consumes 8% of the 100 ms budget on every request.

**Assessment:** This is a latency optimization opportunity, not a current bottleneck. It becomes important if other latency sources increase (e.g., cross-AZ Redis latency, DynamoDB tail latency under contention).

---

## 3. Resource Utilization Patterns

### 3.1 Lambda Concurrency

| Scenario | Peak RPS | Est. Concurrent Instances | Cold Starts | Provisioned (current) |
|---|---|---|---|---|
| Baseline | 1,000 | ~15 | Minimal (ramp-up) | 2 |
| Stress | 10,000 | ~150 | Gradual (2 min ramp) | 2 |
| Spike | 5,000 | ~75 | 47 simultaneous | 2 |
| Soak | 500 | ~8 | 0 (all warm) | 2 |

Formula: `concurrent_instances = RPS * avg_duration_seconds = 10000 * 0.015 = 150`

The stress scenario's gradual 2-minute ramp allows Lambda to scale without mass cold starts. The spike scenario's 5-second ramp overwhelms the provisioned concurrency of 2, forcing 47+ cold starts.

### 3.2 Redis Connections and Memory

| Metric | Soak Start | Soak End | Limit | Trend |
|---|---|---|---|---|
| Redis memory | 12 MB | 14 MB | 256 MB | Stable (+17%, cache fill) |
| Connections (est.) | 8 | 8 | 65,000 | Stable (warm instances constant) |

At stress peak (10K RPS, ~150 Lambda instances, `PoolSize: 10`): up to 1,500 Redis connections. With recommended `PoolSize: 2` (per `redis.md`), this drops to 300 connections.

**Cache hit ratio:** Not directly measured, but the redirect p50 of 10.12 ms (baseline) vs. cache-miss path budget of 8-20 ms (`critical-path.md`) suggests a high cache hit ratio (> 80%). With only 10 seeded short codes, warm cache is expected.

### 3.3 DynamoDB Capacity

| Metric | Baseline | Stress | Soak |
|---|---|---|---|
| Read throughput (est.) | < 200 RCU | < 2,000 RCU | < 100 RCU |
| Write throughput (est.) | ~150 WCU | ~1,500 WCU | ~75 WCU |
| Throttled requests | 0 | 174 (> 7.2K RPS) | 0 |

Read throughput is low because Redis cache absorbs most redirect reads. Write throughput is dominated by click increments (1 WCU per redirect) and link creation (5 WCU per create, at 15% of traffic).

### 3.4 Lambda Memory Utilization (Soak)

| Metric | Start | End (30 min) | Configured | Utilization |
|---|---|---|---|---|
| Lambda memory used | 42 MB | 48 MB | 128 MB | 37% |

The 6 MB growth over 30 minutes is consistent with normal Go GC heap behavior (see `allocations.md`: ~1,100 bytes per request at 500 RPS = ~33 MB/min allocation throughput, fully collected by GC). No memory leak detected. At the recommended 512 MB (`lambda-sizing.md`), utilization would be 9%, providing ample headroom.

---

## 4. Optimization Recommendations

Ordered by impact on SLO compliance and production readiness.

### P0: Required Before Production Launch

| # | Optimization | Impact | Effort | Reference |
|---|---|---|---|---|
| 1 | **Increase provisioned concurrency to 10, enable auto-scaling (10-150)** | Eliminates cold-start SLO violations in spike scenario. At 10 provisioned, the first 10 concurrent requests (covering ~670 RPS) have zero cold start. Auto-scaling to 150 covers the full 10K RPS target. | Low (Terraform change) | `lambda-sizing.md` Auto Scaling Configuration |
| 2 | **Move Secrets Manager fetch out of Lambda init** | Reduces cold start from ~300-400 ms to ~150-200 ms. Use KMS-encrypted environment variable for `IP_HASH_SALT` instead of Secrets Manager API call. Cold starts that do occur (during extreme spikes beyond provisioned capacity) complete faster. | Low (config change) | `critical-path.md` Cold Start Optimization #1 |
| 3 | **Pre-warm DynamoDB capacity before traffic events** | Prevents write throttling at > 7K RPS. Run a 5-minute warm-up at 50% expected peak before anticipated traffic surges. For on-demand tables, DynamoDB auto-scales to 2x previous peak. | Low (operational procedure) | `dynamodb.md` Section 2 |

### P1: Recommended Post-Launch

| # | Optimization | Impact | Effort | Reference |
|---|---|---|---|---|
| 4 | **Reduce Redis PoolSize from 10 to 2** | Reduces cold-start connection overhead from ~100 ms to ~20 ms. Reduces total Redis connections at 10K RPS from 1,500 to 300. | Trivial (config change) | `redis.md` Section 1 |
| 5 | **Defer click increment to SQS worker for links without max_clicks** | Saves 3-8 ms on the critical path for the majority of redirects. Reduces redirect p99 by ~10%. | Medium (code change) | `critical-path.md` F5 |
| 6 | **Pipeline Redis cache GET + negative cache GET** | Saves ~0.5 ms on cache-miss path by combining two sequential Redis roundtrips into one pipeline call. | Low (code change) | `critical-path.md` F2 |
| 7 | **Make cache warm (Redis SET on miss) fire-and-forget** | Saves ~0.5-1 ms on cache-miss path. The SET result does not affect the current request. | Trivial (code change) | `critical-path.md` F1 |

### P2: Monitoring and Ongoing

| # | Optimization | Impact | Effort | Reference |
|---|---|---|---|---|
| 8 | **Integrate baseline test into CI for redirect-critical code paths** | Catches performance regressions before merge. Fail if p99 degrades > 20%. | Medium (CI config) | `load-test-guide.md` Section 9 |
| 9 | **Run nightly soak test against dev** | Catches memory leaks and gradual degradation early. 10 min at 200 RPS is sufficient. | Low (CI config) | `load-test-guide.md` Section 9 |
| 10 | **Add Redis latency histograms to telemetry** | Enables alerting when Redis p99 GET exceeds 2 ms (same-AZ threshold). Currently no per-operation Redis latency visibility. | Low (code change) | `redis.md` monitoring section |

### P3: Future Optimization Levers (v2)

| # | Optimization | Impact | Effort | Reference |
|---|---|---|---|---|
| 11 | **CloudFront edge caching for permanent links** | Reduces Lambda invocations by 60-80% for popular links. Requires per-link `cache_redirect` flag. Trades click accuracy for latency and cost. | High (feature + infra) | `cloudfront-cache.md` Section 2 |
| 12 | **Switch cache serialization from JSON to MessagePack** | Saves ~0.3 ms per cache hit (unmarshal) and reduces Redis memory. Only worthwhile if cache hit path needs optimization. | Medium (code change) | `benchmarks.md` Section 4 |

---

## 5. Projected Impact of P0+P1 Optimizations

### Redirect p99 Projection (Cache HIT Path)

| Component | Current | After P0+P1 | Delta |
|---|---|---|---|
| Rate limit (Redis EVALSHA) | 1-2 ms | 1-2 ms | -- |
| Cache lookup (Redis GET) | 0.5-1 ms | 0.5-1 ms | -- |
| Click increment (DynamoDB) | 3-8 ms | 0 ms (deferred) | -3 to -8 ms |
| Response write | 0.01 ms | 0.01 ms | -- |
| **Total (warm instance)** | **5-11 ms** | **2-3 ms** | **-55 to -73%** |
| **Cold start impact** | **+300-400 ms** | **+150-200 ms** | **-50%** |

### Redirect p99 Projection (Cache MISS Path)

| Component | Current | After P0+P1 | Delta |
|---|---|---|---|
| Rate limit (Redis EVALSHA) | 1-2 ms | 1-2 ms | -- |
| Cache + negative check | 1-2 ms | 0.5-1 ms (pipelined) | -0.5 ms |
| DynamoDB GetItem | 3-8 ms | 3-8 ms | -- |
| Cache warm (Redis SET) | 0.5-1 ms | 0 ms (fire-and-forget) | -0.5 to -1 ms |
| Click increment (DynamoDB) | 3-8 ms | 0 ms (deferred) | -3 to -8 ms |
| **Total (warm instance)** | **8-20 ms** | **4-11 ms** | **-45 to -50%** |

### Projected SLO Compliance After Optimizations

| Scenario | Current p99 | Projected p99 | SLO (100 ms) | Headroom |
|---|---|---|---|---|
| Baseline (1K RPS) | 67.81 ms | ~40-50 ms | PASS | ~50% |
| Stress (10K RPS) | 85.42 ms | ~55-65 ms | PASS | ~35% |
| Spike (5K RPS, cold) | 168.42 ms | ~60-80 ms* | PASS* | ~20% |
| Soak (500 RPS) | 62.14 ms | ~35-45 ms | PASS | ~55% |

*Spike projection assumes provisioned concurrency auto-scaling covers 90%+ of concurrent instances. Remaining cold starts use the faster init (150-200 ms without Secrets Manager fetch), keeping individual cold-start requests at ~165-215 ms. With only ~5% of requests hitting cold start (vs. current 47 out of ~5000 concurrent), the p99 should remain below 100 ms.

---

## 6. Test Methodology Caveats

The following factors affect the accuracy of these results when extrapolating to production (AWS):

| Factor | Local Test Behavior | Production Behavior | Impact Direction |
|---|---|---|---|
| Lambda cold start | Simulated (Go process already running) | Real ARM64 cold start 300-400 ms | Production WORSE |
| DynamoDB latency | LocalStack in-process, ~1 ms | Eventually consistent read 2-5 ms | Production WORSE |
| Redis latency | Docker container, same host, ~0.1 ms | ElastiCache same-AZ, ~0.5-1 ms | Production WORSE |
| Network jitter | None (localhost) | VPC network, ~0.1-0.5 ms variance | Production WORSE |
| DynamoDB adaptive capacity | Not simulated | Auto-scales to 2x peak within minutes | Production BETTER |
| CloudFront | Not present | WAF + edge processing adds ~1-2 ms | Production WORSE |
| k6 machine | Single macOS host | Distributed load generation possible | Local results may undercount RPS ceiling |

**Net assessment:** Production latencies will be higher than local test results. The local baseline p99 of 67.81 ms should be treated as a lower bound. Adding ~10-20 ms for real network latency gives an estimated production baseline p99 of ~80-90 ms, still within the 100 ms SLO but with reduced headroom. This reinforces the importance of the P0 and P1 optimizations.

---

## 7. Recommended Next Steps

1. **Deploy provisioned concurrency = 10 with auto-scaling** (P0 #1) and re-run the spike test against the dev AWS environment. Target: spike p99 < 100 ms.
2. **Replace Secrets Manager init with KMS-encrypted env var** (P0 #2) and measure cold-start duration reduction with `aws logs filter-log-events --filter-pattern "REPORT"`.
3. **Implement click-increment deferral** (P1 #5) and re-run the baseline test. Target: redirect p99 reduction of 5-10 ms.
4. **Run the full 4-scenario suite against the dev AWS environment** after P0 changes are deployed. Local results provide directional confidence, but production validation is required before launch.
5. **Establish the baseline regression table** (per `load-test-guide.md` Section 6) with the production test results as the first entry.

---

## Appendix: Baseline Regression Entry

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Commit | `a3f81d2` |
| Environment | Local (Docker Compose) |
| Lambda memory | 128 MB (local process) |
| Redirect p50 | 10.12 ms |
| Redirect p95 | 35.42 ms |
| Redirect p99 | 67.81 ms |
| Error rate | 0.025% |
| Cache hit ratio | ~80% (estimated) |
| Stress breaking point | ~8,500 RPS |
| Soak degradation | -4.35% (improved) |
| Observed bottleneck | Lambda cold starts (spike), DynamoDB throttling (stress > 7.2K RPS) |
