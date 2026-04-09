# Capacity Planning -- Shorty URL Shortener

## Overview

This document provides capacity estimates for DynamoDB and ElastiCache Redis at three scale tiers: low (1K RPS), medium (10K RPS), and high (50K RPS) redirect traffic. All pricing uses **us-east-1** rates as of 2025.

---

## 1. DynamoDB -- Links Table

### 1.1 Storage Estimates

| Scale | Active Links | Avg Item Size | Raw Storage | With GSI (ALL) | Total |
|-------|-------------|---------------|-------------|----------------|-------|
| Small | 1,000 | 300 bytes | 0.3 MB | 0.6 MB | ~1 MB |
| Medium | 10,000 | 300 bytes | 3 MB | 6 MB | ~6 MB |
| Large | 100,000 | 300 bytes | 30 MB | 60 MB | ~60 MB |
| XL | 1,000,000 | 300 bytes | 300 MB | 600 MB | ~600 MB |

Note: GSI `owner_id-created_at-index` uses ALL projection, so storage is approximately 2x the base table.

### 1.2 Throughput Estimates (Redirect Reads)

The redirect Lambda reads from the `links` table only on cache miss. Cache hit rate depends on traffic distribution (power-law: popular links are cached).

| RPS (redirects) | Cache Hit Rate | DynamoDB Reads/s | RCU (eventually consistent) | RCU (strongly consistent) |
|-----------------|---------------|-----------------|----------------------------|--------------------------|
| 1,000 | 80% | 200 | 200 | 400 |
| 10,000 | 85% | 1,500 | 1,500 | 3,000 |
| 50,000 | 90% | 5,000 | 5,000 | 10,000 |

All redirect reads use **eventually consistent** mode (1 RCU per read for items < 4KB). The link item is ~300 bytes, well under the 4KB boundary.

### 1.3 Write Throughput

| Operation | Rate | WCU per Op | Total WCU/s |
|-----------|------|-----------|-------------|
| Link creation (peak) | 50/s | 1 | 50 |
| Click count increment (via worker) | batched | 1 per item | See below |
| Link update/delete | 5/s | 1 | 5 |

Click count increments are processed by the SQS worker Lambda using individual UpdateItem calls (not BatchWriteItem, because conditional expressions are needed). At 10K RPS redirects:
- Worker receives ~10,000 click events/second via SQS
- Worker batches are max 10 SQS messages, each containing 1 click
- UpdateItem for click_count: 10,000 WCU/s
- **Optimization:** aggregate click counts in the worker before writing (e.g., batch 100 clicks for the same code into a single `ADD click_count :100`). This reduces to ~100-500 WCU/s depending on traffic distribution.

### 1.4 Capacity Mode Recommendation

**On-Demand** for the `links` table.

Rationale:
- Write traffic is bursty (marketing campaigns can spike link creation 10x).
- Read traffic after cache is modest (~1,500 RCU at 10K RPS).
- On-demand costs more per RCU/WCU but eliminates throttling risk and auto-scaling lag.
- Break-even: on-demand becomes more expensive than provisioned when sustained traffic exceeds ~15% of peak capacity for >8 hours/day. For most URL shortener workloads, traffic is too spiky for provisioned to save money.

---

## 2. DynamoDB -- Clicks Table

### 2.1 Growth Rate

| Redirect RPS | Clicks/day | Clicks/month | Clicks/90 days (retention) |
|-------------|-----------|-------------|---------------------------|
| 1,000 | 86.4M | 2.6B | 7.8B |
| 10,000 | 864M | 25.9B | 77.8B |
| 50,000 | 4.32B | 129.6B | 388.8B |

These are theoretical maximums assuming sustained RPS. Realistic estimates assume 12-hour active periods:

| Redirect RPS (peak) | Avg daily factor | Clicks/day (realistic) | 90-day items |
|---------------------|-----------------|----------------------|--------------|
| 1,000 | 0.5 | 43.2M | 3.9B |
| 10,000 | 0.5 | 432M | 38.9B |
| 50,000 | 0.3 | 1.3B | 116.6B |

### 2.2 Storage Estimates

| Scale | 90-day items | Item size | Storage |
|-------|-------------|-----------|---------|
| 1K RPS | 3.9B | 240 bytes | ~870 GB |
| 10K RPS | 38.9B | 240 bytes | ~8.7 TB |
| 50K RPS | 116.6B | 240 bytes | ~26 TB |

**Important:** These numbers are very large. At 10K+ RPS, consider:
1. Pre-aggregating clicks in the worker (daily rollups) instead of storing every click event.
2. Reducing retention from 90 days to 30 days for free-tier users.
3. Sampling: store 1-in-10 clicks for high-volume links, extrapolate in stats queries.

### 2.3 GSI Storage and Throughput

GSI `code-date-index` with INCLUDE projection (4 fields: `country`, `device_type`, `referer_domain`, `ip_hash`):

| Field | Avg Size |
|-------|---------|
| PK (code) | 14 bytes |
| SK (created_at) | 8 bytes |
| country | 2 bytes |
| device_type | 7 bytes |
| referer_domain | 25 bytes |
| ip_hash | 64 bytes |
| **GSI item total** | **~120 bytes** |

GSI storage is approximately 50% of the base table size due to the reduced projection.

### 2.4 Write Throughput

| RPS | BatchWriteItem batches/s (25 items) | WCU/s (base table) | WCU/s (GSI) | Total WCU/s |
|-----|--------------------------------------|--------------------|-----------|-----------  |
| 1,000 | 40 | 1,000 | 500 | 1,500 |
| 10,000 | 400 | 10,000 | 5,000 | 15,000 |
| 50,000 | 2,000 | 50,000 | 25,000 | 75,000 |

### 2.5 Capacity Mode Recommendation

**Provisioned with auto-scaling** for the `clicks` table.

Rationale:
- Write pattern is predictable (proportional to redirect traffic).
- High sustained throughput makes provisioned cheaper than on-demand.
- Auto-scaling config: target utilization 70%, min capacity 100 WCU, max capacity 20,000 WCU, scale-in cooldown 300s, scale-out cooldown 60s.

```hcl
resource "aws_appautoscaling_target" "clicks_write" {
  max_capacity       = 20000
  min_capacity       = 100
  resource_id        = "table/shorty-clicks"
  scalable_dimension = "dynamodb:table:WriteCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "clicks_write" {
  name               = "DynamoDBWriteCapacityUtilization"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.clicks_write.resource_id
  scalable_dimension = aws_appautoscaling_target.clicks_write.scalable_dimension
  service_namespace  = aws_appautoscaling_target.clicks_write.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}
```

---

## 3. DynamoDB -- Users Table

### 3.1 Estimates

| Scale | Users | Storage | RCU/s | WCU/s |
|-------|-------|---------|-------|-------|
| Small | 100 | ~15 KB | < 1 | < 1 |
| Medium | 10,000 | ~1.5 MB | ~10 | ~10 |
| Large | 100,000 | ~15 MB | ~50 | ~50 |

**Capacity Mode:** On-Demand. Traffic is very low and unpredictable.

---

## 4. Redis (ElastiCache) -- Memory Sizing

### 4.1 Per-Key Memory Estimates

| Key Type | Payload Size | Redis Overhead | Total per Key |
|----------|-------------|---------------|---------------|
| `link:{code}` (cached link) | 300-500 bytes | ~100 bytes | ~500 bytes |
| `neg:{code}` (negative cache) | 1 byte | ~80 bytes | ~80 bytes |
| `rl:redir:{ip_hash}` (sorted set, 200 members max) | ~4,800 bytes | ~200 bytes | ~5,000 bytes |
| `rl:create:{ip_hash}` (sorted set, 5 members max) | ~120 bytes | ~200 bytes | ~320 bytes |
| `stats:{code}:aggregate` | ~500 bytes | ~100 bytes | ~600 bytes |

Redis overhead includes: key pointer, value pointer, TTL metadata, dict entry, and SDS string header.

### 4.2 Total Memory at Scale

**Scenario: 10K active links, 1K concurrent rate-limited IPs (typical medium traffic)**

| Component | Key Count | Memory per Key | Total |
|-----------|----------|---------------|-------|
| Cached links (hot subset) | 5,000 | 500 bytes | 2.5 MB |
| Negative cache | 1,000 | 80 bytes | 80 KB |
| Redirect rate limiters | 1,000 | 5,000 bytes | 5 MB |
| Create rate limiters | 200 | 320 bytes | 64 KB |
| Stats cache | 500 | 600 bytes | 300 KB |
| **Subtotal** | | | **~8 MB** |
| Redis base overhead | | | ~30 MB |
| Lua script cache | | | ~1 MB |
| **Total** | | | **~40 MB** |

**Scenario: 100K active links, 10K concurrent rate-limited IPs (high traffic)**

| Component | Key Count | Memory per Key | Total |
|-----------|----------|---------------|-------|
| Cached links | 50,000 | 500 bytes | 25 MB |
| Negative cache | 10,000 | 80 bytes | 800 KB |
| Redirect rate limiters | 10,000 | 5,000 bytes | 50 MB |
| Create rate limiters | 2,000 | 320 bytes | 640 KB |
| Stats cache | 5,000 | 600 bytes | 3 MB |
| **Subtotal** | | | **~80 MB** |
| Redis overhead | | | ~50 MB |
| **Total** | | | **~130 MB** |

### 4.3 Instance Type Recommendations

| Environment | Instance Type | Memory | vCPUs | Network | Monthly Cost |
|-------------|-------------|--------|-------|---------|-------------|
| Local dev | Docker Redis | 128 MB | shared | localhost | $0 |
| Dev/Staging | cache.t4g.micro | 0.5 GB | 2 | Up to 5 Gbps | ~$12/mo |
| Prod (low) | cache.t4g.medium | 3.09 GB | 2 | Up to 5 Gbps | ~$47/mo |
| Prod (medium) | cache.r7g.large | 13.07 GB | 2 | Up to 12.5 Gbps | ~$206/mo |
| Prod (high) | cache.r7g.xlarge | 26.32 GB | 4 | Up to 12.5 Gbps | ~$411/mo |

**Recommendation for launch:** `cache.t4g.medium` (3 GB). Our estimated peak usage is ~130 MB, giving 23x headroom. Upgrade to `cache.r7g.large` when memory usage exceeds 2 GB or when connection count exceeds 10,000.

### 4.4 Connection Pooling Configuration

| Parameter | Dev | Prod |
|-----------|-----|------|
| `PoolSize` (max active) | 5 | 10 |
| `MinIdleConns` | 1 | 2 |
| `MaxIdleConns` | 3 | 5 |
| `DialTimeout` | 5s | 3s |
| `ReadTimeout` | 2s | 1s |
| `WriteTimeout` | 2s | 1s |
| `PoolTimeout` | 3s | 2s |
| `ConnMaxIdleTime` | 300s | 240s |

---

## 5. Cost Estimates

### 5.1 DynamoDB Costs (On-Demand pricing, us-east-1)

Pricing (2025):
- Write Request Unit (WRU): $1.25 per million
- Read Request Unit (RRU): $0.25 per million
- Storage: $0.25 per GB/month

**Links table:**

| Traffic | Reads/mo | Writes/mo | Read Cost | Write Cost | Storage Cost | Total/mo |
|---------|---------|----------|-----------|------------|-------------|---------|
| 1K RPS | 518M | 13M | $129.50 | $16.25 | $0.01 | ~$146 |
| 10K RPS | 3.9B | 130M | $975.00 | $162.50 | $0.01 | ~$1,138 |
| 50K RPS | 13B | 650M | $3,250.00 | $812.50 | $0.06 | ~$4,063 |

**Clicks table (provisioned):**

Provisioned pricing:
- WCU: $0.00065 per WCU-hour
- RCU: $0.00013 per RCU-hour

| Traffic | Provisioned WCU | Provisioned RCU | WCU Cost/mo | RCU Cost/mo | Storage/mo | Total/mo |
|---------|----------------|----------------|-------------|-------------|-----------|---------|
| 1K RPS | 1,500 | 50 | $711 | $5 | $217 | ~$933 |
| 10K RPS | 15,000 | 200 | $7,110 | $19 | $2,175 | ~$9,304 |
| 50K RPS | 75,000 | 500 | $35,550 | $47 | $6,500 | ~$42,097 |

### 5.2 On-Demand vs Provisioned Break-Even (Clicks Table)

| Metric | On-Demand | Provisioned |
|--------|----------|-------------|
| WCU cost | $1.25/million requests | $0.00065/WCU-hour = $0.4745/WCU-month |
| Break-even | | ~380,000 writes/WCU/month |

A single provisioned WCU handles 1 write/second = 2.6M writes/month at a cost of $0.47/month. On-demand: 2.6M writes = $3.25/month.

**Provisioned is cheaper when utilization exceeds ~15% of provisioned capacity.** For the clicks table with predictable, sustained write traffic, provisioned with auto-scaling is the clear winner.

### 5.3 Redis (ElastiCache) Costs

| Environment | Instance | HA (replica) | Monthly Cost |
|-------------|---------|-------------|-------------|
| Dev | cache.t4g.micro | No | $12 |
| Staging | cache.t4g.micro x2 | Yes (multi-AZ) | $24 |
| Prod (launch) | cache.t4g.medium x2 | Yes (multi-AZ) | $94 |
| Prod (scale) | cache.r7g.large x2 | Yes (multi-AZ) | $412 |

### 5.4 Total Monthly Cost Summary

| Component | 1K RPS | 10K RPS | 50K RPS |
|-----------|--------|---------|---------|
| DynamoDB (links) | $146 | $1,138 | $4,063 |
| DynamoDB (clicks) | $933 | $9,304 | $42,097 |
| DynamoDB (users) | $5 | $10 | $50 |
| DynamoDB storage | $217 | $2,175 | $6,500 |
| ElastiCache Redis | $94 | $94 | $412 |
| **Total data layer** | **~$1,395** | **~$12,721** | **~$53,122** |

**Cost optimization levers:**
1. **Click aggregation** -- pre-aggregate clicks in the worker (daily rollups) instead of storing every raw event. Can reduce clicks WCU by 90%+.
2. **Reserved capacity** -- DynamoDB reserved capacity (1-year or 3-year) saves up to 77%. Applicable once traffic stabilizes.
3. **Click sampling** -- for high-volume links (>1K clicks/day), store 1-in-N clicks and multiply in stats. Reduces storage and WCU proportionally.
4. **Shorter retention** -- 30-day retention for free tier vs 90 days reduces storage by 67%.

---

## 6. Monitoring and Alerts

### 6.1 DynamoDB CloudWatch Metrics

| Metric | Warning Threshold | Critical Threshold |
|--------|------------------|-------------------|
| `ConsumedReadCapacityUnits` / `ProvisionedReadCapacityUnits` | > 70% | > 90% |
| `ConsumedWriteCapacityUnits` / `ProvisionedWriteCapacityUnits` | > 70% | > 90% |
| `ThrottledRequests` | > 0 (any throttle) | > 100/min |
| `SystemErrors` | > 0 | > 10/min |
| `UserErrors` (ConditionalCheckFailed) | Informational | -- |

### 6.2 Redis CloudWatch Metrics

| Metric | Warning Threshold | Critical Threshold |
|--------|------------------|-------------------|
| `DatabaseMemoryUsagePercentage` | > 60% | > 80% |
| `CurrConnections` | > 50,000 | > 60,000 |
| `Evictions` | > 100/min | > 1,000/min |
| `CacheHitRate` | < 70% | < 50% |
| `ReplicationLag` | > 1s | > 5s |
| `EngineCPUUtilization` | > 70% | > 90% |
