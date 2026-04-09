# Capacity Planning

Shorty URL Shortener -- resource consumption estimates, growth projections, cost analysis, and scaling policies.

---

## 1. Traffic Tiers

All projections use three tiers anchored to the architectural target of 10,000 RPS redirect traffic.

| Tier | Redirect RPS | API RPS | Click Events/s | Description |
|------|-------------|---------|----------------|-------------|
| **Baseline (1x)** | 1,000 | 50 | 1,000 | Post-launch steady state |
| **Target (10x)** | 10,000 | 500 | 10,000 | Architecture design point |
| **Growth (50x)** | 50,000 | 2,500 | 50,000 | 12-18 month growth target |

**Assumptions:**
- Traffic mix: 80% redirects, 15% API (CRUD), 5% stats queries.
- Every redirect generates one click event (published to SQS asynchronously).
- Redis cache hit ratio: 85% at baseline, 90% at target, 92% at growth (power-law distribution favors popular links).
- CloudFront cache hit ratio for redirects: 60% at baseline, 70% at target, 75% at growth.

---

## 2. Lambda Concurrency

Lambda concurrency = RPS x average duration (in seconds).

### Redirect Lambda (512 MB ARM64)

| Tier | Origin RPS (after CloudFront) | Avg Duration | Concurrent Executions | Provisioned Concurrency |
|------|------------------------------|-------------|----------------------|------------------------|
| Baseline | 400 (60% CF hit) | 15 ms | 6 | 10 |
| Target | 3,000 (70% CF hit) | 15 ms | 45 | 50 |
| Growth | 12,500 (75% CF hit) | 15 ms | 188 | 200 |

Cold start budget: 300-400 ms (Go ARM64). Provisioned concurrency eliminates cold starts for steady-state traffic. Auto-scaling policy tracks `ProvisionedConcurrencyUtilization` at 70% target.

**Account-level concurrency limit:** Default is 1,000 per region. At Growth tier, all three Lambdas combined need approximately 250 concurrent executions. Request a limit increase to 3,000 before reaching Target tier to provide headroom for spikes.

### API Lambda (256 MB ARM64)

| Tier | RPS | Avg Duration | Concurrent Executions |
|------|-----|-------------|----------------------|
| Baseline | 50 | 50 ms | 3 |
| Target | 500 | 50 ms | 25 |
| Growth | 2,500 | 50 ms | 125 |

No provisioned concurrency needed. API requests are not latency-critical enough to justify the cost. Cold starts (approximately 500 ms) are acceptable against the 300 ms p99 target because they are infrequent relative to traffic volume.

### Worker Lambda (128 MB ARM64)

| Tier | SQS Messages/s | Batch Size | Batches/s | Avg Batch Duration | Concurrent Executions |
|------|----------------|-----------|-----------|-------------------|----------------------|
| Baseline | 1,000 | 10 | 100 | 200 ms | 20 |
| Target | 10,000 | 10 | 1,000 | 200 ms | 200 |
| Growth | 50,000 | 10 | 5,000 | 200 ms | 1,000 |

At Growth tier, the worker Lambda requires 1,000 concurrent executions. This is the largest concurrency consumer. SQS FIFO throughput limit is 300 messages/second per message group ID. Since each short code is a separate message group, throughput scales horizontally with the number of distinct codes receiving clicks.

**SQS FIFO queue throughput:** With high-throughput mode enabled, the queue supports up to 30,000 messages/second (across all message groups). This is sufficient for the Growth tier.

---

## 3. DynamoDB Capacity

### 3.1 `links` Table

**Access patterns at each tier:**

| Operation | Tier | Read CU/s | Write CU/s | Notes |
|-----------|------|-----------|------------|-------|
| GetItem (redirect, cache miss) | Baseline | 150 | -- | 1,000 RPS x 15% miss rate = 150 |
| GetItem (redirect, cache miss) | Target | 1,000 | -- | 10,000 x 10% miss |
| GetItem (redirect, cache miss) | Growth | 4,000 | -- | 50,000 x 8% miss |
| PutItem (create link) | Baseline | -- | 50 | 50 creates/s |
| PutItem (create link) | Target | -- | 500 | 500 creates/s |
| PutItem (create link) | Growth | -- | 2,500 | 2,500 creates/s |
| UpdateItem (click count) | Baseline | -- | 1,000 | Every redirect increments |
| UpdateItem (click count) | Target | -- | 10,000 | (If synchronous; 0 if deferred to worker) |
| UpdateItem (click count) | Growth | -- | 50,000 | (If synchronous) |

**Capacity mode recommendation:** On-demand. Traffic is bursty (marketing campaigns, social media spikes). On-demand handles 2x previous peak instantly. Provisioned auto-scaling takes 5-15 minutes to react, which is too slow for viral link spikes.

**GSI: `owner_id-created_at-index`**

| Tier | Query RCU/s | Write Replication WCU/s |
|------|------------|------------------------|
| Baseline | 5 | 50 (mirrors base table writes) |
| Target | 50 | 500 |
| Growth | 250 | 2,500 |

### 3.2 `clicks` Table

| Tier | Write CU/s | Read CU/s | Notes |
|------|-----------|-----------|-------|
| Baseline | 1,000 | 50 | BatchWriteItem from worker |
| Target | 10,000 | 500 | Stats queries drive reads |
| Growth | 50,000 | 2,500 | |

**Capacity mode recommendation:** Provisioned with auto-scaling at Target and Growth tiers. Write volume is predictable (proportional to redirect traffic). Auto-scaling target: 70% utilization.

| Tier | Provisioned WCU | Provisioned RCU | Auto-scale Max WCU |
|------|----------------|----------------|-------------------|
| Baseline | On-demand | On-demand | -- |
| Target | 14,000 | 700 | 50,000 |
| Growth | 70,000 | 3,500 | 150,000 |

**Storage growth:**
- Each click record is approximately 240 bytes.
- 90-day TTL auto-deletes old records.
- At Target (10K RPS): 10,000 clicks/s x 86,400 s/day x 90 days x 240 B = approximately 18.7 TB over the retention window.
- DynamoDB storage cost: $0.25/GB/month. At 18.7 TB: approximately $4,675/month.

### 3.3 `users` Table

Negligible capacity requirements at all tiers. On-demand mode. Less than 100 CU/s combined even at Growth tier.

---

## 4. ElastiCache Redis

### Memory Sizing

| Component | Per-key Size | Key Count (Target) | Total Memory |
|-----------|-------------|-------------------|-------------|
| `link:{code}` cached links | ~500 B (JSON + overhead) | 50,000 active | ~25 MB |
| `neg:{code}` negative cache | ~100 B | 10,000 | ~1 MB |
| `rl:redir:{ip_hash}` sorted sets | ~5 KB (200 members) | 10,000 unique IPs | ~50 MB |
| `rl:create:{ip_hash}` sorted sets | ~500 B (5 members) | 2,000 unique IPs | ~1 MB |
| Redis overhead (fragmentation, buffers) | -- | -- | ~20 MB |
| **Total** | | | **~97 MB** |

| Tier | Active Cached Links | Unique IPs | Total Memory | Recommended Node |
|------|-------------------|-----------|-------------|-----------------|
| Baseline | 10,000 | 2,000 | ~40 MB | `cache.t4g.micro` (0.5 GB) |
| Target | 50,000 | 10,000 | ~100 MB | `cache.t4g.medium` (3 GB) |
| Growth | 200,000 | 50,000 | ~400 MB | `cache.t4g.medium` (3 GB) |

Even at Growth tier, total Redis memory is well under 1 GB. The `cache.t4g.medium` provides ample headroom. Upgrade to `cache.r7g.large` (13 GB) only if sorted set sizes grow significantly (rate limit window increase or member count increase).

### Connection Pool Scaling

With `PoolSize: 2` per Lambda instance:

| Tier | Lambda Instances (all functions) | Total Redis Connections | ElastiCache Limit |
|------|--------------------------------|------------------------|-------------------|
| Baseline | ~30 | ~60 | 65,000 |
| Target | ~270 | ~540 | 65,000 |
| Growth | ~1,300 | ~2,600 | 65,000 |

Connection count is never a bottleneck. The `maxclients` limit of 65,000 provides 25x headroom even at Growth tier.

### Redis Operations/Second

| Tier | EVALSHA (rate limit) | GET (cache) | SET (cache warm) | Total ops/s |
|------|---------------------|-------------|-----------------|------------|
| Baseline | 1,000 | 1,150 | 150 | ~2,300 |
| Target | 10,000 | 11,000 | 1,000 | ~22,000 |
| Growth | 50,000 | 54,000 | 4,000 | ~108,000 |

A single `cache.t4g.medium` node handles approximately 100,000 ops/s for simple GET/SET commands. At Growth tier, operations approach this limit. Mitigation options:

1. **Read replica:** Add a read replica and direct GET commands to it. EVALSHA (rate limit) stays on primary.
2. **Upgrade node type:** `cache.r7g.large` handles approximately 200,000 ops/s.
3. **CloudFront absorption:** Higher CF hit ratio reduces origin traffic and Redis operations proportionally.

---

## 5. SQS FIFO Throughput

| Tier | Messages/s | Batch Send (10 msgs) | Cost/month |
|------|-----------|---------------------|-----------|
| Baseline | 1,000 | 100 batches/s | ~$1,300 |
| Target | 10,000 | 1,000 batches/s | ~$13,000 |
| Growth | 50,000 | 5,000 batches/s | ~$65,000 |

SQS FIFO pricing: $0.50 per million requests. Each send and receive counts as one request.

**High-throughput FIFO mode** (required at Target and above): Supports up to 30,000 messages/second per queue, with up to 3,000 messages/second per message group ID. This is sufficient for all tiers.

**Cost optimization:** At Growth tier, SQS FIFO cost ($65K/month) becomes a significant line item. Consider migrating to DynamoDB Streams (approximately $2,600/month at the same volume) when the engineering trade-offs are acceptable. See `docs/performance/dynamodb.md` section 8 for the full trade-off analysis.

---

## 6. CloudFront

### Cache Hit Ratio Impact

CloudFront caching redirect responses (302 with `Cache-Control: public, max-age=60`) absorbs a significant portion of origin traffic:

| Tier | Total RPS | CF Hit Ratio | Origin RPS | Lambda Savings |
|------|----------|-------------|-----------|---------------|
| Baseline | 1,000 | 60% | 400 | 60% fewer invocations |
| Target | 10,000 | 70% | 3,000 | 70% fewer invocations |
| Growth | 50,000 | 75% | 12,500 | 75% fewer invocations |

Higher cache hit ratio is the single most effective cost optimization lever. Each percentage point increase in CF hit ratio reduces Lambda, DynamoDB, and Redis load proportionally.

**Tuning levers:**
- Increase `max-age` from 60s to 300s: raises hit ratio by approximately 10-15 percentage points, but increases staleness for deactivated links.
- Use `stale-while-revalidate`: serves stale content while revalidating in the background.
- CloudFront Functions for simple redirects: bypass Lambda entirely for the hottest links by maintaining a small key-value store at the edge.

### Data Transfer

| Tier | Requests/month | Avg Response Size | Data Transfer/month | Cost |
|------|---------------|------------------|--------------------|----|
| Baseline | 2.6B | 500 B (302 + headers) | ~1.3 TB | ~$110 |
| Target | 26B | 500 B | ~13 TB | ~$1,040 |
| Growth | 130B | 500 B | ~65 TB | ~$4,550 |

CloudFront data transfer pricing uses tiered rates. First 10 TB at $0.085/GB, next 40 TB at $0.080/GB, next 100 TB at $0.060/GB.

---

## 7. API Gateway v2 (HTTP API)

| Tier | Requests/month | Cost ($1.00/million) |
|------|---------------|---------------------|
| Baseline | 2.6B | $2,600 |
| Target | 26B | $26,000 |
| Growth | 130B | $130,000 |

**Cost optimization:** At Target tier and above, API Gateway becomes the largest cost component. Mitigation:
- CloudFront absorbs 60-75% of requests before they reach API Gateway.
- Effective API Gateway requests: 30-40% of total.
- With CF: Target tier API Gateway cost drops to approximately $7,800/month.

For Growth tier, consider migrating to ALB + Fargate or direct Lambda function URLs behind CloudFront to eliminate API Gateway costs entirely. Lambda function URLs are free (no per-request charge beyond Lambda invocation cost).

---

## 8. WAF

| Tier | Requests Evaluated/month | Cost ($0.60/million) |
|------|-------------------------|---------------------|
| Baseline | 2.6B | $1,560 |
| Target | 26B | $15,600 |
| Growth | 130B | $78,000 |

WAF is attached to CloudFront, so all requests are evaluated. Fixed costs: $5/month per web ACL + $1/month per rule. Variable cost dominates at scale.

**Cost optimization:** WAF rules can be scoped to specific URI patterns. Rate-based rules only evaluate requests that match the scope, reducing evaluation count. If WAF cost becomes prohibitive at Growth tier, move rate limiting entirely to the application layer (Redis Lua scripts) and reduce WAF to IP reputation lists only.

---

## 9. CloudWatch

| Component | Tier: Baseline | Tier: Target | Tier: Growth |
|-----------|---------------|-------------|-------------|
| Log ingestion (Lambda) | ~100 GB/mo ($50) | ~1 TB/mo ($500) | ~5 TB/mo ($2,500) |
| Log storage (3-month retention) | ~300 GB ($9) | ~3 TB ($90) | ~15 TB ($450) |
| Custom metrics (10 metrics) | $3 | $3 | $3 |
| Dashboards (5) | $15 | $15 | $15 |
| Alarms (20) | $2 | $2 | $2 |
| **Total** | **~$79** | **~$610** | **~$2,970** |

**Cost optimization:**
- Set log retention to 30 days (not 90) for redirect Lambda logs. Click data is in DynamoDB.
- Use structured JSON logging with field filtering to reduce log volume.
- Sample verbose logs at 1% in production (log every 100th successful redirect, log all errors).

---

## 10. Monthly Cost Projections

### Baseline Tier (1,000 RPS)

| Service | Monthly Cost | Notes |
|---------|------------|-------|
| Lambda (all functions) | $40 | Includes provisioned concurrency |
| DynamoDB (links, on-demand) | $520 | Reads + writes |
| DynamoDB (clicks, on-demand) | $3,250 | Write-heavy |
| DynamoDB (users) | $35 | Negligible |
| DynamoDB storage | $470 | ~1.9 TB (90-day clicks) |
| ElastiCache Redis | $60 | t4g.micro + 1 replica |
| SQS FIFO | $1,300 | Send + receive |
| CloudFront (transfer) | $110 | 1.3 TB |
| CloudFront (requests) | $2,600 | |
| API Gateway | $1,040 | After CF absorption (40%) |
| WAF | $1,560 | All requests evaluated |
| CloudWatch | $79 | Logs + metrics |
| **Total** | **~$11,064** | |

### Target Tier (10,000 RPS)

| Service | Monthly Cost | Notes |
|---------|------------|-------|
| Lambda (all functions) | $420 | Includes provisioned concurrency (50) |
| DynamoDB (links, on-demand) | $3,500 | |
| DynamoDB (clicks, provisioned) | $6,560 | Provisioned + auto-scaling |
| DynamoDB (users) | $350 | |
| DynamoDB storage | $4,675 | ~18.7 TB |
| ElastiCache Redis | $94 | t4g.medium + 1 replica |
| SQS FIFO | $13,000 | |
| CloudFront (transfer) | $1,040 | 13 TB |
| CloudFront (requests) | $7,800 | After CF 70% hit |
| API Gateway | $7,800 | After CF absorption |
| WAF | $15,600 | |
| CloudWatch | $610 | |
| **Total** | **~$61,449** | |

### Growth Tier (50,000 RPS)

| Service | Monthly Cost | Notes |
|---------|------------|-------|
| Lambda (all functions) | $2,100 | Includes provisioned concurrency (200) |
| DynamoDB (links, on-demand) | $17,500 | |
| DynamoDB (clicks, provisioned) | $32,800 | |
| DynamoDB (users) | $1,750 | |
| DynamoDB storage | $23,375 | ~93.5 TB |
| ElastiCache Redis | $94 | t4g.medium still sufficient |
| SQS FIFO | $65,000 | Largest line item |
| CloudFront (transfer) | $4,550 | 65 TB |
| CloudFront (requests) | $32,500 | After CF 75% hit |
| API Gateway | $32,500 | After CF absorption |
| WAF | $78,000 | |
| CloudWatch | $2,970 | |
| **Total** | **~$293,139** | |

---

## 11. Cost Optimization Levers

Ranked by impact at Target tier:

| Lever | Savings at Target | Effort | Notes |
|-------|------------------|--------|-------|
| **Lambda Function URLs instead of API Gateway** | ~$7,800/mo | Medium | Requires CloudFront origin reconfiguration |
| **DynamoDB Streams instead of SQS FIFO** | ~$10,400/mo | High | Trade-off: at-least-once semantics, shard coupling |
| **CloudFront hit ratio 70% to 85%** | ~$4,000/mo | Low | Increase `max-age` to 300s |
| **Lambda ARM64 (already using)** | ~$0 (already applied) | -- | 20% cheaper than x86 |
| **Redis PoolSize 10 to 2** | ~$0 (memory only) | Low | Reduces cold start by ~10 ms |
| **WAF scope reduction** | ~$5,000/mo | Low | Evaluate only non-CF-cached requests |
| **CloudWatch log sampling** | ~$250/mo | Low | Log 1% of successful redirects |
| **DynamoDB click table: provisioned vs on-demand** | ~$26,000/mo | Low | Already recommended |

### Top 3 Recommendations

1. **Replace API Gateway with Lambda Function URLs behind CloudFront.** Lambda function URLs have no per-request charge. At Target tier this saves $7,800/month. At Growth tier: $32,500/month.

2. **Evaluate DynamoDB Streams for click recording.** SQS FIFO at $13K/month (Target) is expensive. DynamoDB Streams would cost approximately $2,600/month. The trade-off is increased complexity (at-least-once delivery, shard management). See `docs/performance/dynamodb.md` section 8.

3. **Increase CloudFront cache TTL.** Moving from 60s to 300s `max-age` increases hit ratio from 70% to approximately 85%, reducing all origin costs by an additional 15%.

---

## 12. Scaling Triggers and Auto-Scaling Policies

### Lambda Auto-Scaling (Provisioned Concurrency)

```
Resource:            lambda:function:shorty-redirect:live
Metric:              ProvisionedConcurrencyUtilization
Target:              70%
Min capacity:        10
Max capacity:        200
Scale-out cooldown:  0 seconds (immediate)
Scale-in cooldown:   300 seconds (5 minutes)
```

### DynamoDB Auto-Scaling (clicks table)

```
Write capacity:
  Min:               2,000 WCU
  Max:               150,000 WCU
  Target utilization: 70%
  Scale-out cooldown: 60 seconds
  Scale-in cooldown:  300 seconds

Read capacity:
  Min:               500 RCU
  Max:               10,000 RCU
  Target utilization: 70%
  Scale-out cooldown: 60 seconds
  Scale-in cooldown:  300 seconds
```

### ElastiCache Scaling (Manual -- No Auto-Scaling)

ElastiCache does not support auto-scaling for node type changes. Scaling triggers require manual intervention or a scheduled Lambda:

| Metric | Warning Threshold | Action |
|--------|------------------|--------|
| `DatabaseMemoryUsagePercentage` | > 60% sustained 1 hour | Upgrade node type |
| `EngineCPUUtilization` | > 70% sustained 30 min | Upgrade node type or add read replica |
| `CurrConnections` | > 50,000 | Upgrade node type (higher `maxclients`) |
| `Evictions` | > 1,000/min sustained | Upgrade node type or reduce TTLs |

### SQS Scaling

SQS scales automatically. No configuration needed. The Lambda event source mapping controls concurrency:

```
Event source mapping:
  Batch size:                10
  Maximum batching window:   1 second
  Maximum concurrency:       1,000 (Growth tier)
  Function response types:   ReportBatchItemFailures
```

---

## 13. Capacity Review Cadence

| Review | Frequency | Trigger |
|--------|-----------|---------|
| Dashboard check | Daily (automated) | Slack bot posts utilization summary |
| Capacity review | Monthly | Engineering meeting |
| Cost review | Monthly | Finance + Engineering |
| Architecture review | Quarterly | When approaching next tier threshold |
| Load test | Per release + quarterly | Validate capacity assumptions |

### Tier Transition Checklist

Before moving from Baseline to Target:
- [ ] Request Lambda concurrency limit increase to 3,000
- [ ] Switch `clicks` table from on-demand to provisioned with auto-scaling
- [ ] Upgrade ElastiCache to `cache.t4g.medium` with replica
- [ ] Enable SQS FIFO high-throughput mode
- [ ] Increase provisioned concurrency from 10 to 50
- [ ] Run stress test at 10,000 RPS against staging
- [ ] Review and update CloudWatch alarms for new thresholds

Before moving from Target to Growth:
- [ ] Evaluate Lambda Function URLs to replace API Gateway
- [ ] Evaluate DynamoDB Streams to replace SQS FIFO
- [ ] Request Lambda concurrency limit increase to 5,000
- [ ] Add ElastiCache read replica (if not already present)
- [ ] Increase provisioned concurrency max to 200
- [ ] Enable DynamoDB Contributor Insights for hot partition detection
- [ ] Run stress test at 50,000 RPS against staging (distributed k6)
- [ ] Budget approval for approximately $293K/month AWS spend
