# Cost Optimization -- Shorty URL Shortener

Monthly cost breakdown and optimization strategies across three traffic tiers. All pricing uses **us-east-1** rates as of April 2026.

---

## 1. Traffic Tier Definitions

| Tier | Redirect RPS | API RPS (est.) | Link Creates/day | Active Links | MAU |
|------|-------------|---------------|------------------|-------------|-----|
| **Small (1K RPS)** | 1,000 | 50 | 5,000 | 100K | 10K |
| **Medium (10K RPS)** | 10,000 | 200 | 20,000 | 500K | 50K |
| **Large (50K RPS)** | 50,000 | 500 | 50,000 | 2M | 200K |

**Assumptions:**
- 12 active hours/day average (load factor 0.5)
- Cache hit rate: 80% (1K), 85% (10K), 90% (50K)
- Average Lambda duration: redirect 15ms, API 50ms, worker 200ms (per batch of 10)
- Redirect Lambda memory: 512 MB; API: 256 MB; Worker: 128 MB

---

## 2. Lambda Costs

### Pricing
- Invocations: $0.20 per 1M requests
- Duration: $0.0000133334 per GB-second (ARM64)
- Provisioned Concurrency: $0.0000041667 per GB-second (provisioned) + $0.0000097222 per GB-second (duration when running)

### 2.1 Redirect Lambda

| Metric | 1K RPS | 10K RPS | 50K RPS |
|--------|--------|---------|---------|
| Invocations/month | 2.59B | 25.9B | 129.6B |
| Invocation cost | $518 | $5,184 | $25,920 |
| Duration (GB-s/month) | 19.4M | 194M | 972M |
| Duration cost | $259 | $2,592 | $12,960 |
| Provisioned (2 instances, 512MB, 24/7) | $108 | $108 | $108 |
| **Subtotal** | **$885** | **$7,884** | **$38,988** |

### 2.2 API Lambda

| Metric | 1K RPS | 10K RPS | 50K RPS |
|--------|--------|---------|---------|
| Invocations/month | 130M | 518M | 1.3B |
| Invocation cost | $26 | $104 | $260 |
| Duration (GB-s/month) | 1.6M | 6.5M | 16.2M |
| Duration cost | $22 | $86 | $216 |
| **Subtotal** | **$48** | **$190** | **$476** |

### 2.3 Worker Lambda

| Metric | 1K RPS | 10K RPS | 50K RPS |
|--------|--------|---------|---------|
| Invocations/month (batches of 10) | 259M | 2.59B | 12.96B |
| Invocation cost | $52 | $518 | $2,592 |
| Duration (GB-s/month) | 6.5M | 64.8M | 324M |
| Duration cost | $11 | $108 | $540 |
| **Subtotal** | **$63** | **$626** | **$3,132** |

### Lambda Total

| Tier | Monthly Cost |
|------|-------------|
| 1K RPS | $996 |
| 10K RPS | $8,700 |
| 50K RPS | $42,596 |

---

## 3. DynamoDB Costs

### Pricing
- On-Demand WRU: $1.25 per million
- On-Demand RRU: $0.25 per million (eventually consistent)
- Provisioned WCU: $0.00065 per WCU-hour ($0.4745/WCU-month)
- Provisioned RCU: $0.00013 per RCU-hour ($0.0949/RCU-month)
- Storage: $0.25 per GB/month

### 3.1 Links Table (On-Demand)

| Metric | 1K RPS | 10K RPS | 50K RPS |
|--------|--------|---------|---------|
| Reads/month (after cache) | 518M | 3.9B | 13B |
| Read cost | $130 | $975 | $3,250 |
| Writes/month | 13M | 130M | 650M |
| Write cost | $16 | $163 | $813 |
| Storage | $0.02 | $0.15 | $0.60 |
| **Subtotal** | **$146** | **$1,138** | **$4,064** |

### 3.2 Clicks Table (Provisioned + Auto-Scaling)

| Metric | 1K RPS | 10K RPS | 50K RPS |
|--------|--------|---------|---------|
| Provisioned WCU (sustained) | 1,500 | 15,000 | 75,000 |
| WCU cost | $711 | $7,110 | $35,550 |
| Provisioned RCU | 50 | 200 | 500 |
| RCU cost | $5 | $19 | $47 |
| Storage (90-day retention) | $217 | $2,175 | $6,500 |
| **Subtotal** | **$933** | **$9,304** | **$42,097** |

### 3.3 Users Table (On-Demand)

| Metric | 1K RPS | 10K RPS | 50K RPS |
|--------|--------|---------|---------|
| Monthly cost | $5 | $10 | $50 |

### DynamoDB Total

| Tier | Monthly Cost |
|------|-------------|
| 1K RPS | $1,084 |
| 10K RPS | $10,452 |
| 50K RPS | $46,211 |

---

## 4. ElastiCache Redis

### Pricing (cache.t4g / cache.r7g, on-demand)

| Tier | Instance Type | HA Config | Hourly | Monthly |
|------|-------------|-----------|--------|---------|
| 1K RPS | cache.t4g.medium | Primary + Replica (Multi-AZ) | $0.130 | $94 |
| 10K RPS | cache.t4g.medium | Primary + Replica (Multi-AZ) | $0.130 | $94 |
| 50K RPS | cache.r7g.large | Primary + Replica (Multi-AZ) | $0.570 | $412 |

Data transfer within AZ: free. Cross-AZ replication: $0.01/GB (negligible for our data volume).

---

## 5. API Gateway v2 (HTTP API)

### Pricing
- $1.00 per million requests (first 300M)
- $0.90 per million requests (300M-1B)

| Tier | Requests/month | Monthly Cost |
|------|---------------|-------------|
| 1K RPS | 2.72B | $2,488 |
| 10K RPS | 26.4B | $23,760 |
| 50K RPS | 131B | $117,900 |

**Important:** These costs assume all traffic passes through API Gateway. With CloudFront in front, cached responses (static assets) bypass API Gateway entirely. The numbers above represent the worst case.

---

## 6. CloudFront

### Pricing (us-east-1, PriceClass_100)
- Requests: $0.0100 per 10,000 HTTPS requests
- Data transfer: $0.085/GB (first 10 TB)

| Tier | Requests/month | Data Transfer (est.) | Request Cost | Transfer Cost | Monthly Total |
|------|---------------|---------------------|-------------|--------------|--------------|
| 1K RPS | 2.72B | 500 GB | $2,720 | $43 | $2,763 |
| 10K RPS | 26.4B | 5 TB | $26,400 | $425 | $26,825 |
| 50K RPS | 131B | 25 TB | $131,000 | $2,125 | $133,125 |

**Note:** Redirect responses are ~300 bytes (302 + Location header). Data transfer estimates assume 200 bytes average response size across all request types.

---

## 7. SQS FIFO

### Pricing
- $0.50 per million requests (FIFO)
- Each API call (SendMessage, ReceiveMessage, DeleteMessage) is a separate request

| Tier | Messages/month | API Calls/month (Send + Receive + Delete) | Monthly Cost |
|------|---------------|------------------------------------------|-------------|
| 1K RPS | 2.59B | 7.78B | $3,888 |
| 10K RPS | 25.9B | 77.8B | $38,880 |
| 50K RPS | 129.6B | 388.8B | $194,400 |

**Critical optimization:** SQS FIFO batch operations (up to 10 messages per API call) reduce costs by up to 10x. See Section 10.

---

## 8. CloudWatch + X-Ray

### CloudWatch Logs
- Ingestion: $0.50/GB
- Storage: $0.03/GB/month

### CloudWatch Metrics
- Custom metrics: $0.30 per metric/month (first 10K)

### X-Ray
- Traces recorded: $5.00 per million traces
- Traces retrieved: $0.50 per million traces

| Tier | Log Volume/month | Custom Metrics | Traces Sampled (5%) | Monthly Total |
|------|-----------------|---------------|---------------------|--------------|
| 1K RPS | 50 GB | 20 metrics | 130M | $700 |
| 10K RPS | 500 GB | 30 metrics | 1.3B | $6,800 |
| 50K RPS | 2.5 TB | 40 metrics | 6.5B | $34,000 |

**Note:** X-Ray sampling at 5% is critical to control costs. At 100% sampling, X-Ray costs alone would exceed $13K/month at 1K RPS.

---

## 9. Other Services

### Cognito
- Free tier: first 50K MAU (with Cognito user pool)
- $0.0055 per MAU above 50K
- Social IdP (Google): $0.015 per MAU

| Tier | MAU | Monthly Cost |
|------|-----|-------------|
| 1K RPS | 10K | $0 (free tier) |
| 10K RPS | 50K | $0 (free tier) |
| 50K RPS | 200K | $2,250 |

### WAF
- Web ACL: $5.00/month
- Rules: $1.00/rule/month
- Requests: $0.60 per million requests
- Bot Control: $10.00/month + $1.00 per million requests

| Tier | Requests/month | Web ACL + Rules (7) | Request Cost | Bot Control | Monthly Total |
|------|---------------|--------------------|--------------| -----------|--------------|
| 1K RPS | 2.72B | $12 | $1,632 | $2,730 | $4,374 |
| 10K RPS | 26.4B | $12 | $15,840 | $26,410 | $42,262 |
| 50K RPS | 131B | $12 | $78,600 | $131,010 | $209,622 |

### S3 (artifacts, logs)
- Storage: $0.023/GB/month
- Estimated: $5-$20/month across all tiers (negligible)

---

## 10. Total Monthly Cost Summary

| Service | 1K RPS | 10K RPS | 50K RPS |
|---------|--------|---------|---------|
| Lambda | $996 | $8,700 | $42,596 |
| DynamoDB | $1,084 | $10,452 | $46,211 |
| ElastiCache Redis | $94 | $94 | $412 |
| API Gateway v2 | $2,488 | $23,760 | $117,900 |
| CloudFront | $2,763 | $26,825 | $133,125 |
| SQS FIFO | $3,888 | $38,880 | $194,400 |
| CloudWatch + X-Ray | $700 | $6,800 | $34,000 |
| Cognito | $0 | $0 | $2,250 |
| WAF | $4,374 | $42,262 | $209,622 |
| S3 | $10 | $15 | $20 |
| **Total (unoptimized)** | **$16,397** | **$157,788** | **$780,536** |

---

## 11. Cost Optimization Strategies

### 11.1 SQS Batch Optimization (HIGH IMPACT)

**Problem:** SQS FIFO is the #1 or #2 cost driver at every tier due to per-API-call pricing.

**Solution:** Use batch operations:
- `SendMessageBatch` (up to 10 messages): buffer click events in the redirect Lambda and send in batches
- `ReceiveMessage` with `MaxNumberOfMessages=10`: already done via Lambda SQS event source mapping
- `DeleteMessageBatch`: Lambda SQS integration handles this automatically

**Impact:** Reduces SQS costs by up to 70-80%.

| Tier | Before | After (batch) | Savings |
|------|--------|--------------|---------|
| 1K RPS | $3,888 | $778 | $3,110 |
| 10K RPS | $38,880 | $7,776 | $31,104 |
| 50K RPS | $194,400 | $38,880 | $155,520 |

### 11.2 CloudFront Caching for Permanent Redirects

**Problem:** Every redirect hits Lambda through API Gateway and CloudFront, all charging per-request.

**Solution:** For links configured as 301 (permanent) redirects, set `Cache-Control: public, max-age=86400`. CloudFront caches these and serves subsequent requests without hitting origin.

**Impact:** If 30% of redirects can be cached (permanent links with high repeat traffic):
- 30% reduction in Lambda invocations for redirect
- 30% reduction in API Gateway requests
- 30% reduction in CloudFront origin requests

| Tier | Combined Savings (Lambda + APIGW + CF) |
|------|---------------------------------------|
| 1K RPS | ~$1,250 |
| 10K RPS | ~$12,200 |
| 50K RPS | ~$60,000 |

### 11.3 Click Aggregation in Worker (HIGH IMPACT)

**Problem:** Storing every individual click event generates massive DynamoDB write throughput and storage costs.

**Solution:** Aggregate clicks in the worker Lambda before writing:
- Buffer clicks per code for 1-minute windows
- Write a single aggregated record per code per minute instead of individual clicks
- Maintain per-minute counters with country/device/referer breakdowns

**Impact:** Reduces clicks table WCU by 90%+ and storage by 80%+.

| Tier | DynamoDB Clicks Before | After Aggregation | Savings |
|------|----------------------|-------------------|---------|
| 1K RPS | $933 | $150 | $783 |
| 10K RPS | $9,304 | $1,200 | $8,104 |
| 50K RPS | $42,097 | $5,000 | $37,097 |

### 11.4 WAF Bot Control: Targeted Mode

**Problem:** WAF Bot Control at $1.00/million requests is extremely expensive at high traffic.

**Solution:**
- Use **Targeted** bot control (not Common) -- only inspect suspicious traffic
- Apply Bot Control rules only to `/api/v1/shorten` and `/api/v1/links` paths, not to redirect traffic
- Use scope-down statements to exclude known-good traffic patterns

**Impact:** Reduces WAF Bot Control request volume by 80-95%.

| Tier | Before | After (targeted, scoped) | Savings |
|------|--------|-------------------------|---------|
| 1K RPS | $4,374 | $500 | $3,874 |
| 10K RPS | $42,262 | $2,000 | $40,262 |
| 50K RPS | $209,622 | $5,000 | $204,622 |

### 11.5 Lambda Compute Savings Plan

**Solution:** 1-year Compute Savings Plan commitment covers Lambda duration costs at ~17% discount.

| Tier | Duration Cost Before | After (Savings Plan) | Savings |
|------|---------------------|---------------------|---------|
| 1K RPS | $292 | $242 | $50 |
| 10K RPS | $2,786 | $2,312 | $474 |
| 50K RPS | $13,716 | $11,384 | $2,332 |

### 11.6 DynamoDB Reserved Capacity

**Solution:** 1-year reserved capacity for the `clicks` table (predictable, sustained writes).

- Reserved WCU: $0.000271/WCU-hour (vs $0.00065 on-demand provisioned) = 58% savings
- Requires upfront payment

| Tier | Provisioned Cost | Reserved Cost | Savings |
|------|-----------------|--------------|---------|
| 1K RPS | $716 | $301 | $415 |
| 10K RPS | $7,129 | $2,994 | $4,135 |
| 50K RPS | $35,597 | $14,951 | $20,646 |

### 11.7 ElastiCache Reserved Nodes

**Solution:** 1-year reserved nodes for ElastiCache (~30% savings).

| Tier | On-Demand | Reserved | Savings |
|------|----------|----------|---------|
| 1K RPS | $94 | $66 | $28 |
| 10K RPS | $94 | $66 | $28 |
| 50K RPS | $412 | $288 | $124 |

### 11.8 Log Retention Policies

**Solution:** Set CloudWatch log retention to minimize storage costs:
- Lambda execution logs: 30 days (dev), 90 days (prod)
- WAF logs: 14 days (high volume)
- API Gateway access logs: 30 days
- Archive to S3 Glacier for compliance (90 days+ retention at $0.004/GB/month)

**Impact:** Reduces CloudWatch storage costs by 60-80% over default (indefinite retention).

### 11.9 X-Ray Sampling Rate

**Solution:** Reduce X-Ray sampling to control costs:
- Redirect Lambda: 1% sampling (high volume, low variance)
- API Lambda: 10% sampling
- Worker Lambda: 5% sampling
- Increase to 100% temporarily for debugging

**Impact:** Reduces X-Ray costs by 80-95%.

---

## 12. Optimized Cost Summary

Applying strategies 11.1 through 11.9:

| Service | 1K RPS | 10K RPS | 50K RPS |
|---------|--------|---------|---------|
| Lambda | $946 | $8,226 | $40,264 |
| DynamoDB | $446 | $3,548 | $14,114 |
| ElastiCache Redis | $66 | $66 | $288 |
| API Gateway v2 | $1,742 | $16,632 | $82,530 |
| CloudFront | $1,934 | $18,778 | $93,188 |
| SQS FIFO | $778 | $7,776 | $38,880 |
| CloudWatch + X-Ray | $200 | $1,500 | $7,000 |
| Cognito | $0 | $0 | $2,250 |
| WAF | $500 | $2,000 | $5,000 |
| S3 | $10 | $15 | $20 |
| **Total (optimized)** | **$6,622** | **$58,541** | **$283,534** |
| **Savings vs unoptimized** | **60%** | **63%** | **64%** |

---

## 13. Cost Monitoring and Alerts

### AWS Budgets Configuration

| Budget | Threshold | Action |
|--------|-----------|--------|
| Monthly total | 80% of estimate | Email alert |
| Monthly total | 100% of estimate | SNS + Slack alert |
| Monthly total | 120% of estimate | Auto-apply Service Control Policy limiting new resource creation |
| Per-service (Lambda) | 110% of estimate | Email alert |
| Per-service (DynamoDB) | 110% of estimate | Email alert |
| Per-service (SQS) | 110% of estimate | Email alert |

### Cost Explorer Tags

All resources must be tagged for cost allocation:

```
Project: shorty
Environment: dev | staging | prod
Component: redirect | api | worker | cache | queue | cdn | waf
```

### Monthly Review Checklist

- [ ] Review Cost Explorer for unexpected spikes
- [ ] Check DynamoDB auto-scaling metrics (over-provisioned WCU = wasted money)
- [ ] Review CloudWatch log volume (are we logging too much in prod?)
- [ ] Check X-Ray trace volume and adjust sampling rates
- [ ] Review WAF request metrics (are Bot Control costs proportional to value?)
- [ ] Evaluate Savings Plan coverage and utilization

---

## 14. Break-Even Analysis: On-Demand vs Provisioned DynamoDB

### Links Table

| Metric | On-Demand | Provisioned (auto-scaling) |
|--------|----------|---------------------------|
| Cost at 1K RPS | $146/mo | ~$120/mo (at 70% utilization) |
| Cost at 10K RPS | $1,138/mo | ~$900/mo |
| Break-even utilization | -- | 15% of provisioned capacity |
| Risk | None (auto-scales) | Throttling during spikes if auto-scaling lag |

**Recommendation:** Stay on-demand for `links` table. Traffic is bursty (marketing campaigns) and the cost difference is small relative to the throttling risk.

### Clicks Table

| Metric | On-Demand | Provisioned (auto-scaling) |
|--------|----------|---------------------------|
| Cost at 10K RPS | ~$14,000/mo | ~$9,300/mo |
| Break-even utilization | -- | 15% |
| Traffic predictability | Proportional to redirects | Highly predictable |

**Recommendation:** Provisioned with auto-scaling for `clicks` table. Write pattern is predictable and sustained. 30-35% savings justify the operational complexity.
