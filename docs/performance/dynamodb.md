# DynamoDB Performance Guide

> Performance characteristics of Shorty's DynamoDB single-table design,
> capacity planning, conditional write behavior, and architectural trade-offs.
> Companion to `docs/db/dynamodb-access-patterns.md` (the access pattern matrix).

---

## 1. Single-Table Design Performance Characteristics

Shorty uses three physical tables (`links`, `clicks`, `users`) rather than a true single-table design. Each table has a focused access pattern set, which simplifies capacity planning and avoids noisy-neighbor effects between read-heavy redirect traffic and write-heavy click recording.

### Why Not One Physical Table?

| Factor | Single Table | Separate Tables (current) |
|---|---|---|
| Capacity mode | Must provision for worst-case across all patterns | Each table sized independently |
| Hot partition blast radius | A viral link's clicks affect link lookups | Click writes isolated from link reads |
| GSI fan-out | GSIs replicate all writes, even unrelated ones | GSIs only on tables that need them |
| TTL deletion throughput | Competes with all other writes | Click TTL deletions isolated |
| Operational visibility | One set of CloudWatch metrics | Per-table metrics for targeted alerting |

The separate-table approach trades DynamoDB best-practice elegance for operational clarity. At Shorty's scale (3 tables, < 20 access patterns total), the overhead is negligible.

### Partition Behavior

DynamoDB automatically splits partitions when throughput exceeds 3,000 RCU or 1,000 WCU per partition, or when partition size exceeds 10 GB. With Base62-random short codes as partition keys, the `links` table distributes uniformly. The `clicks` table concentrates writes by `LINK#{code}`, which can create hot partitions for viral links.

---

## 2. Hot Partition Detection

### `links` Table: Low Risk

Partition key `LINK#{code}` uses randomly generated Base62 codes (7-8 characters). With 62^7 = 3.5 trillion possible codes, key distribution is inherently uniform. No single partition accumulates disproportionate traffic under normal operation.

**Monitoring:** CloudWatch `ConsumedReadCapacityUnits` and `ConsumedWriteCapacityUnits` with the `TableName` dimension. If any single partition exceeds 3,000 RCU, the `ThrottledRequests` metric increases.

### `clicks` Table: Moderate Risk

A viral link receiving 100,000 clicks/hour concentrates all writes on a single partition key `LINK#{code}`. At 28 writes/second (100K / 3600), this is well within the 1,000 WCU per-partition limit. However, a link going mega-viral (1M+ clicks/hour = 278 writes/s) approaches the limit.

**Mitigations (layered):**

1. **SQS FIFO batching:** The worker Lambda processes clicks in batches of up to 25 via `BatchWriteItem`. This reduces the number of DynamoDB API calls (though WCU consumption is the same).
2. **DynamoDB adaptive capacity:** Automatically isolates hot items to dedicated partitions within minutes. This handles transient spikes.
3. **Click-count limit (`max_clicks`):** Links with a click limit naturally cap their write volume.
4. **90-day TTL:** Old click records are automatically deleted, preventing unbounded partition growth.

**Escalation path (if adaptive capacity is insufficient):**

Write-sharding the partition key: `LINK#{code}#SHARD#{rand(0,9)}` distributes writes across 10 partitions. Reads use scatter-gather (10 parallel queries merged client-side). This adds read complexity and latency (~2x) but provides 10x write throughput per code. Only implement if `ThrottledRequests` on the `clicks` table exceeds the alarm threshold.

### `users` Table: Negligible Risk

Partition key `USER#{cognito_sub}` uses UUIDs. Traffic is extremely low (< 100 operations/second total). No hot partition concern.

### Detection Procedure

1. **CloudWatch alarm:** `ThrottledRequests > 0` for any table triggers investigation.
2. **CloudWatch Contributor Insights:** Enable on `links` and `clicks` tables to identify the top 10 most-accessed partition keys.
3. **Application logging:** The DynamoDB adapter should log `ConditionalCheckFailedException` counts per code, which correlates with hot keys.

---

## 3. Conditional Write Performance

### L1: CreateLink (Collision-Safe PutItem)

```
PutItem on LINK#{code} / META
ConditionExpression: attribute_not_exists(PK)
```

**Performance characteristics:**

- **Happy path (no collision):** 1 WCU, single-digit ms latency. The condition check adds no measurable overhead -- DynamoDB evaluates conditions during the write operation, not as a separate read.
- **Collision path:** `ConditionalCheckFailedException` is returned in ~5 ms. The application retries with a new code (up to 3 retries, then extends code length by 1 character).
- **Collision probability:** With 7-character Base62 codes and 1 million existing links, collision probability is ~0.03% per attempt. Expected retries per creation: ~0.0003. Collision handling is effectively free at projected scale.

**Transactional variant (X1):** When creating a link, the application uses `TransactWriteItems` to atomically create the link AND update the user's quota counter. This costs 2 WCU (2x single write) and adds ~5 ms latency compared to a plain PutItem.

### L8: IncrementClickCount (Conditional UpdateItem)

```
UpdateItem on LINK#{code} / META
SET click_count = click_count + :one, updated_at = :now
ConditionExpression: is_active = :true AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)
```

**Performance characteristics:**

- **Happy path (active link, under limit):** 1 WCU, single-digit ms latency.
- **Rejected path (inactive or limit reached):** `ConditionalCheckFailedException` in ~5 ms. The redirect Lambda returns HTTP 410 Gone.
- **Concurrency safety:** DynamoDB conditional writes are serializable per item. Two concurrent increments on the same `LINK#{code}` are serialized -- one succeeds, the other retries. At high concurrency (1,000+ concurrent increments on the same code), DynamoDB's internal locking can cause `ProvisionedThroughputExceededException` even under the WCU limit. The SQS FIFO architecture mitigates this by serializing click-count updates per code through the message group ID.

### Write Contention at Scale

DynamoDB item-level locking means concurrent writes to the same item are serialized. For the `links` table, the click-count increment is the only contended write path.

| Concurrent writers per code | Expected behavior |
|---|---|
| 1-10 | No contention. All succeed immediately. |
| 10-100 | Occasional retries (< 1%). DynamoDB handles transparently. |
| 100-1,000 | Increased p99 latency (50-100 ms). Retries increase. |
| 1,000+ | Throttling likely. SQS FIFO prevents this scenario. |

The SQS FIFO worker processes click-count increments sequentially per `MessageGroupId` (which is the short code). This guarantees at most 1 concurrent writer per code, eliminating contention entirely.

---

## 4. GSI Performance: `owner_id-created_at-index`

### Index Design

```
GSI: owner_id-created_at-index
Partition key: owner_id (String)
Sort key: created_at (String, ISO 8601)
Projection: ALL
Table: links
```

### Query Pattern (L7: ListLinks)

```
Query on GSI
KeyCondition: owner_id = :uid
ScanIndexForward: false (newest first)
Limit: 20 (page size)
FilterExpression: is_active = :active (optional)
```

**Performance characteristics:**

- **Latency:** Single-digit ms for the first page. Pagination via `ExclusiveStartKey` adds no overhead.
- **RCU cost:** 0.5 RCU per 4 KB read (eventually consistent). A page of 20 links at ~300 bytes each = ~6 KB = 1 RCU per page.
- **Write amplification:** Every write to the `links` table is replicated to the GSI. At 50 link creations/second, GSI write cost is 50 WCU/s additional.

### Hot GSI Partition Risk

A power user with 10,000 links creates a moderately large GSI partition. The `Query` operation scans within this partition, which is efficient. The risk is write throughput: if a user creates links at high rate (> 100/minute), their `owner_id` partition in the GSI could throttle.

**Mitigation:** The `daily_link_quota` enforced in the `users` table (via conditional write U3) caps creation rate per user. Free tier: 50/day, Pro: 500/day. Even at 500/day, the write rate is < 0.006 WCU/s per user -- negligible.

### FilterExpression Cost

The optional `is_active = :active` filter runs **after** the Query reads data from the GSI. If a user has 100 links but 80 are deleted (soft-delete), the query still reads all 100 and filters to 20. The RCU cost reflects the pre-filter data size.

**Optimization (if needed):** Add `is_active` as a GSI sort key prefix: `GSI SK = {is_active}#{created_at}`. This allows the condition to be part of the `KeyConditionExpression`, eliminating wasted reads. Only implement if users frequently have > 50% deleted links.

---

## 5. BatchWriteItems for Click Events

### Current Pattern (C2)

The SQS worker Lambda receives up to 10 SQS messages per batch (configurable). Each message contains one click event. The worker writes these as a `BatchWriteItem` call with up to 25 PutItem requests.

```
BatchWriteItem
  - PutItem: LINK#{code1} / CLICK#{ts}#{uuid}
  - PutItem: LINK#{code1} / CLICK#{ts}#{uuid}
  - PutItem: LINK#{code2} / CLICK#{ts}#{uuid}
  ...up to 25 items
```

### Performance Characteristics

| Aspect | Value |
|---|---|
| Max items per batch | 25 |
| Max payload size | 16 MB (far above our ~6 KB per batch) |
| WCU cost | 1 WCU per item (items < 1 KB at ~240 bytes) |
| Latency | 10-30 ms for a batch of 25 (single network roundtrip) |
| Partial failure | `UnprocessedItems` contains failed writes; must retry |

### Partial Failure Handling

`BatchWriteItem` does not fail atomically -- individual items can fail while others succeed. The `UnprocessedItems` response field contains items that were not written, typically due to throughput throttling.

**Retry strategy:**
1. Retry `UnprocessedItems` with exponential backoff (100 ms, 200 ms, 400 ms, max 3 retries).
2. If retries exhaust, return the SQS batch as partially failed. SQS re-delivers the failed messages after the visibility timeout.
3. SQS FIFO deduplication (5-minute window) prevents duplicate click records from retried messages.

### BatchWriteItem vs TransactWriteItems

`BatchWriteItem` is chosen over `TransactWriteItems` because:
- Click events are independent -- no atomicity requirement across items.
- `BatchWriteItem` costs 1 WCU per item; `TransactWriteItems` costs 2 WCU per item.
- `BatchWriteItem` supports 25 items; `TransactWriteItems` supports only 100 items but at 2x cost.

### Throughput at Scale

At 10,000 redirect RPS with 80% cache hit rate, the click recording path sees ~10,000 events/second (every redirect generates a click event regardless of cache).

| Batch size | Batches/second | DynamoDB WCU/s | API calls/second |
|---|---|---|---|
| 10 | 1,000 | 10,000 | 1,000 |
| 25 | 400 | 10,000 | 400 |

At 10,000 WCU/s, the `clicks` table requires provisioned capacity with auto-scaling or on-demand mode. See section 6.

---

## 6. On-Demand vs Provisioned Capacity at 10K RPS

### Capacity Requirements at 10K RPS Steady State

From the access pattern matrix capacity summary:

| Table | Read CU/s | Write CU/s |
|---|---|---|
| `links` | ~1,025 | ~550 |
| `clicks` | ~1,500 | ~10,000 |
| `users` | ~25 | ~100 |

### On-Demand Mode

On-demand automatically scales to handle traffic spikes without capacity planning. Pricing is per-request:

| Table | Monthly Read Cost | Monthly Write Cost | Total |
|---|---|---|---|
| `links` | $0.25/M * 1,025 * 2.6M = ~$665 | $1.25/M * 550 * 2.6M = ~$1,788 | ~$2,453 |
| `clicks` | $0.25/M * 1,500 * 2.6M = ~$975 | $1.25/M * 10,000 * 2.6M = ~$32,500 | ~$33,475 |
| `users` | ~$17 | ~$325 | ~$342 |
| **Total** | | | **~$36,270/mo** |

*(2.6M seconds/month = 30 days)*

### Provisioned Mode with Auto-Scaling

Provisioned capacity is ~5x cheaper per unit than on-demand:

| Table | Provisioned RCU | Provisioned WCU | Monthly Cost |
|---|---|---|---|
| `links` | 1,500 RCU (target 70%) | 800 WCU (target 70%) | ~$425 |
| `clicks` | 2,000 RCU (target 70%) | 14,000 WCU (target 70%) | ~$6,560 |
| `users` | 50 RCU | 150 WCU | ~$55 |
| **Total** | | | **~$7,040/mo** |

### Recommendation

| Table | Mode | Rationale |
|---|---|---|
| `links` | **On-demand** | Traffic is bursty (marketing campaigns, social media spikes). On-demand handles 2x previous peak instantly. Provisioned auto-scaling takes 5-15 minutes to react. Cost difference is ~$2K/mo vs ~$425/mo, but burst handling is worth the premium. |
| `clicks` | **Provisioned + auto-scaling** | Write volume is predictable (proportional to redirect traffic). Auto-scaling target 70% utilization with 14,000 WCU base. Scale-up policy: add 40% capacity when utilization > 70% for 2 consecutive minutes. Cost savings: ~$26K/mo. |
| `users` | **On-demand** | Negligible volume. On-demand cost is < $350/mo. Not worth the operational overhead of provisioned capacity planning. |

### Auto-Scaling Configuration for `clicks`

```
Minimum capacity:  2,000 WCU / 500 RCU
Maximum capacity:  50,000 WCU / 10,000 RCU
Target utilization: 70%
Scale-out cooldown: 60 seconds
Scale-in cooldown:  300 seconds (5 min, conservative to avoid flapping)
```

**Critical:** Auto-scaling reacts to `ConsumedWriteCapacityUnits` metric, which has 1-minute granularity. A sudden 10x spike takes 2-3 minutes to fully scale. During this window, DynamoDB burst capacity (300 seconds of unused capacity) absorbs the load. If burst capacity is depleted, requests throttle.

For truly unpredictable spikes, switch `clicks` to on-demand temporarily during known events (product launches, marketing campaigns) and switch back to provisioned afterward.

---

## 7. Redis Cache vs DAX: Why Redis

### DAX (DynamoDB Accelerator) Overview

DAX is a fully managed, DynamoDB-compatible cache that sits in front of DynamoDB. It supports the same API (GetItem, Query, etc.) with microsecond read latency.

### Why Shorty Uses Redis Instead of DAX

| Factor | DAX | Redis (ElastiCache) | Winner |
|---|---|---|---|
| **Read latency** | ~200-400 us (microseconds) | ~500-1000 us | DAX |
| **Write-through cache** | Automatic (writes to DAX update DynamoDB) | Manual (application manages cache invalidation) | DAX |
| **Rate limiting** | Not supported (cache only) | Lua scripts, sorted sets | Redis |
| **Negative caching** | Returns cache miss (no negative cache) | Custom negative cache entries with TTL | Redis |
| **Lambda cold start** | DAX client SDK initialization: ~300 ms | Redis TCP connection: ~20 ms | Redis |
| **Multi-purpose** | DynamoDB cache only | Cache + rate limiter + negative cache + stats aggregation | Redis |
| **Cost (t4g.medium equivalent)** | ~$72/mo (dax.t3.medium, no smaller option) | ~$47/mo (cache.t4g.medium) | Redis |
| **VPC requirement** | Required (same as Redis) | Required | Tie |
| **TTL control** | Item-level, inherited from DynamoDB TTL | Key-level, application-controlled | Redis |

**The decisive factor is multi-purpose usage.** Shorty requires Redis for rate limiting (Lua sorted-set script) regardless of the caching choice. Adding DAX would mean running two in-memory data stores:

- DAX for DynamoDB caching (~$72/mo)
- Redis for rate limiting + negative cache (~$47/mo)
- Total: ~$119/mo

Using Redis for everything: ~$47/mo. The 200-400 us latency advantage of DAX is irrelevant when the redirect p99 budget is 100 ms and the Redis cache lookup is < 1 ms.

### When DAX Would Make Sense

- If Shorty dropped Redis rate limiting (e.g., moved to WAF-only rate limiting).
- If read latency requirements tightened to sub-millisecond (< 500 us p99).
- If the DynamoDB cache invalidation logic became too complex to manage manually.

None of these scenarios are projected.

---

## 8. DynamoDB Streams vs SQS: Click Event Architecture

### Current Architecture: SQS FIFO

```
Redirect Lambda
  |
  [async goroutine] --> SQS FIFO queue --> Worker Lambda
                                            |
                                            BatchWriteItem (clicks table)
                                            UpdateItem (click_count on links table)
```

### Alternative: DynamoDB Streams

```
Redirect Lambda
  |
  UpdateItem (click_count on links table)
  |
  DynamoDB Stream trigger --> Worker Lambda
                               |
                               PutItem (clicks table)
```

### Trade-off Analysis

| Factor | SQS FIFO | DynamoDB Streams | Winner |
|---|---|---|---|
| **Redirect Lambda decoupling** | Fire-and-forget SQS publish (~2 ms) | Must write to DynamoDB in the critical path (~5-8 ms) | SQS |
| **Click-count atomicity** | Worker does UpdateItem (off critical path) | Stream triggers on the UpdateItem (click already counted) | DynamoDB Streams |
| **Ordering guarantee** | FIFO with MessageGroupId per code | Shard-level ordering by sequence number | Tie |
| **Exactly-once processing** | FIFO deduplication (5-min window) | At-least-once (must handle duplicates) | SQS |
| **Batch size** | Up to 10 messages per Lambda invocation | Up to 100 records per Lambda invocation (configurable) | DynamoDB Streams |
| **Failure handling** | Dead-letter queue after N retries | Retry until success or record expires (24h) | SQS |
| **Cost** | $0.50 per million FIFO requests | $0.02 per 100K read units (much cheaper) | DynamoDB Streams |
| **Operational complexity** | Standard SQS monitoring | Shard management, iterator age monitoring | SQS |

### Why SQS FIFO Is Correct for Shorty

1. **Critical path latency:** The redirect Lambda must respond in < 100 ms. Writing to SQS asynchronously (goroutine with 2 ms timeout) keeps click recording off the critical path. With DynamoDB Streams, the click-count increment (UpdateItem) would be on the critical path, adding 5-8 ms.

2. **Exactly-once semantics:** SQS FIFO provides content-based deduplication. If the redirect Lambda's SQS publish retries (e.g., timeout on first attempt), the duplicate message is automatically suppressed. DynamoDB Streams delivers at-least-once, requiring the worker to implement idempotency (e.g., conditional PutItem with `attribute_not_exists`).

3. **Dead-letter queue:** Failed SQS messages move to a DLQ after 3 retries. This provides a clear inspection and replay mechanism. DynamoDB Streams retries indefinitely until the record expires (24 hours), which can block the entire shard if one record consistently fails.

4. **Decoupled scaling:** SQS Lambda concurrency is independent of the `links` table. DynamoDB Streams Lambda concurrency is tied to the number of stream shards (1 per partition), which can create processing bottlenecks if the table has few partitions.

### When DynamoDB Streams Would Be Better

- If click-count accuracy is critical and must be synchronous with the redirect (e.g., real-time click auctions).
- If cost optimization is paramount (Streams are ~25x cheaper than SQS FIFO at high volume).
- If the architecture already requires Streams for another purpose (e.g., cross-region replication, event sourcing).

---

## 9. Monitoring and Alerting

### Key CloudWatch Metrics

| Metric | Table | Warning | Critical | Action |
|---|---|---|---|---|
| `ThrottledRequests` | All | > 0 sustained | > 100/min | Check hot partitions, increase capacity |
| `ConsumedWriteCapacityUnits` | clicks | > 70% of provisioned | > 90% | Auto-scaling should handle; verify scale-up fired |
| `ConsumedReadCapacityUnits` | links | > 70% of on-demand baseline | > 90% | Check cache hit rate; Redis may be down |
| `SystemErrors` | All | > 0 | > 10/min | AWS-side issue; check service health dashboard |
| `UserErrors` | All | Baseline + 50% | Baseline + 100% | Likely application bug (malformed requests) |
| `SuccessfulRequestLatency` | All | p99 > 10 ms | p99 > 25 ms | Investigate item size growth or hot partitions |
| `ReplicationLatency` | GSIs | > 1 second | > 5 seconds | GSI under-provisioned; increase GSI WCU |

### DynamoDB Contributor Insights

Enable on `links` and `clicks` tables to identify:
- Most accessed partition keys (top 10 by read/write operations).
- Most throttled partition keys.
- Traffic patterns over time (useful for capacity planning).

Cost: $0.50 per 100,000 contributor insight events. At 10K RPS, approximately $130/month per table. Enable during performance investigations, disable during steady state to save cost.

### Application-Level Metrics

Emit from the DynamoDB store adapter:

```
shorty.dynamodb.latency_ms       -- per-operation latency histogram
shorty.dynamodb.throttled        -- throttled request counter
shorty.dynamodb.conditional_fail -- ConditionalCheckFailedException counter
shorty.dynamodb.batch_retries    -- BatchWriteItem UnprocessedItems retry counter
shorty.dynamodb.transaction_fail -- TransactWriteItems failure counter
```
