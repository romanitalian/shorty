# Lambda Memory Sizing Recommendations

> Lambda CPU scales linearly with memory: 1,769 MB = 1 vCPU.
> ARM64 (Graviton2) pricing: $0.0000133334 per GB-second.

---

## Memory-to-CPU Mapping

| Memory | vCPU Equivalent | Network Bandwidth |
|---|---|---|
| 128 MB | 0.07 vCPU | 10 Mbps |
| 256 MB | 0.14 vCPU | 25 Mbps |
| 512 MB | 0.29 vCPU | 50 Mbps |
| 1024 MB | 0.58 vCPU | 100 Mbps |
| 1536 MB | 0.87 vCPU | 150 Mbps |
| 1769 MB | 1.00 vCPU | ~175 Mbps |

---

## 1. Redirect Lambda (`cmd/redirect`)

### Memory Utilization Profile

The redirect Lambda's memory footprint consists of:

| Component | Estimated Size |
|---|---|
| Go runtime + binary | ~15 MB |
| AWS SDK clients (DynamoDB, SQS, SecretsManager) | ~10 MB |
| Redis client + connection pool (PoolSize=10) | ~5 MB |
| Request processing (per-invocation) | ~1 MB |
| **Total baseline** | **~31 MB** |

### Benchmarking Methodology

```bash
# Step 1: Deploy with test memory size
aws lambda update-function-configuration \
  --function-name shorty-redirect \
  --memory-size 512

# Step 2: Run k6 load test (1,000 VU, 60s per memory size)
k6 run -e TARGET=https://dev.shorty.example.com tests/load/baseline.js

# Step 3: Collect metrics from CloudWatch Lambda Insights
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Duration \
  --dimensions Name=FunctionName,Value=shorty-redirect \
  --start-time $(date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --period 60 \
  --statistics p50 p99 Maximum \
  --extended-statistics p95

# Step 4: Check memory utilization
aws logs filter-log-events \
  --log-group-name /aws/lambda/shorty-redirect \
  --filter-pattern "REPORT" \
  --limit 100 | jq '.events[].message' | grep -oP 'Max Memory Used: \K\d+'
```

### Expected Performance Profile

| Memory | vCPU | p50 (ms) | p99 (ms) | Cold Start (ms) | Cost per 1M invocations |
|---|---|---|---|---|---|
| 128 MB | 0.07 | ~40 | ~120 | ~800 | $0.27 |
| 256 MB | 0.14 | ~25 | ~60 | ~500 | $0.53 |
| **512 MB** | **0.29** | **~15** | **~35** | **~350** | **$1.07** |
| 1024 MB | 0.58 | ~10 | ~25 | ~250 | $2.13 |
| 1536 MB | 0.87 | ~8 | ~20 | ~200 | $3.20 |

### Recommendation: 512 MB

**Rationale:**
1. **p99 headroom:** At 512 MB, expected p99 of ~35 ms provides 65 ms headroom against the 100 ms target. This accounts for occasional DynamoDB tail latency spikes and GC pauses.
2. **CPU sufficiency:** The redirect handler is I/O-bound (Redis + DynamoDB), not CPU-bound. SHA-256 hashing and JSON unmarshal are the only CPU-intensive operations, completing in < 1 us each. Beyond 0.29 vCPU, there are diminishing returns.
3. **Memory headroom:** With ~31 MB baseline usage, 512 MB provides 16x headroom. This accommodates the Go GC heap (GOGC=100 means heap can double to ~60 MB) and burst scenarios.
4. **Cold start:** 350 ms cold start is acceptable given provisioned concurrency covers steady-state traffic.
5. **Cost efficiency:** At 10,000 RPS with 15 ms avg duration:
   - Invocations/month: 10,000 * 86,400 * 30 = 25.9 billion (theoretical max)
   - Realistic: ~100M invocations/month
   - Cost: 100M * $1.07/1M = ~$107/month compute
   - 1024 MB would be $213/month for ~30% latency improvement (diminishing returns)

### Redis Pool Size Adjustment

**Current:** `PoolSize: 10` (`cmd/redirect/main.go:389`)

Lambda processes one request at a time (single concurrent invocation per instance). A pool of 10 connections is wasteful:
- 10 idle connections * ~10 KB each = ~100 KB wasted memory per Lambda instance
- Connection establishment overhead during cold start: 10 * ~2 ms = ~20 ms

**Recommendation:** Set `PoolSize: 2` (one active + one spare for pipelining).

---

## 2. API Lambda (`cmd/api`)

### Memory Utilization Profile

| Component | Estimated Size |
|---|---|
| Go runtime + binary | ~20 MB (larger binary: CRUD + stats handlers) |
| AWS SDK clients | ~10 MB |
| Redis client | ~3 MB |
| oapi-codegen generated types | ~2 MB |
| Request processing | ~5 MB (JSON serialization of lists) |
| **Total baseline** | **~40 MB** |

### Recommendation: 256 MB

**Rationale:**
1. **Latency target:** p99 < 300 ms (NFR 3.1). At 256 MB, expected p99 ~100 ms for simple CRUD, ~200 ms for stats aggregation. Well within budget.
2. **Traffic pattern:** API endpoints receive 10-100x less traffic than redirects. Cost optimization is more important than tail latency.
3. **CPU needs:** CRUD operations are I/O-bound (DynamoDB reads/writes). Stats endpoints do more computation (aggregation) but are infrequent.
4. **Memory needs:** ListLinksByOwner can return up to 100 links (~20 KB JSON). Stats queries may aggregate larger datasets. 256 MB provides sufficient headroom.

### When to increase to 512 MB

- If stats aggregation queries exceed p99 300 ms target
- If the API Lambda adds heavier features (CSV export, bulk operations)

---

## 3. Worker Lambda (`cmd/worker`)

### Memory Utilization Profile

| Component | Estimated Size |
|---|---|
| Go runtime + binary | ~15 MB |
| AWS SDK (DynamoDB) | ~8 MB |
| SQS batch processing buffer | ~2 MB (25 events * ~1 KB each) |
| **Total baseline** | **~25 MB** |

### Recommendation: 128 MB

**Rationale:**
1. **No latency target:** The worker processes SQS messages asynchronously. There is no user-facing latency requirement. Processing time of 500 ms - 2 s per batch is acceptable.
2. **Pure I/O:** The worker's only operation is `BatchWriteItem` to DynamoDB. CPU usage is negligible (unmarshal SQS message, marshal DynamoDB items).
3. **Cost optimization:** At 128 MB, the worker is the cheapest possible configuration. SQS FIFO delivers up to 300 messages/second per message group; with batches of 10, the worker handles 3,000 clicks/second.
4. **Memory sufficiency:** 25 MB baseline leaves 100 MB headroom at 128 MB setting. Batch sizes are capped at 25 items (DynamoDB limit), so memory usage is bounded.

---

## Cost Analysis

### Monthly Cost at Target Traffic (10,000 redirect RPS)

| Lambda | Memory | Invocations/mo | Avg Duration | GB-seconds | Cost |
|---|---|---|---|---|---|
| redirect | 512 MB | 100M | 15 ms | 750,000 | $10.00 |
| api | 256 MB | 5M | 50 ms | 62,500 | $0.83 |
| worker | 128 MB | 10M | 200 ms | 250,000 | $3.33 |
| **Total** | | | | | **$14.16** |

Plus invocation cost: 115M * $0.20/1M = $23.00

**Total Lambda compute: ~$37/month** at 10K redirect RPS.

### Provisioned Concurrency Cost (redirect only)

Provisioned concurrency costs $0.0000041667 per GB-second (provisioned, regardless of usage):

| Provisioned Instances | Memory | Monthly Cost (provisioned) | Justification |
|---|---|---|---|
| 2 | 512 MB | $5.40 | Minimum: handles cold start for first 2 requests |
| 10 | 512 MB | $27.00 | Low traffic: covers baseline without cold starts |
| 50 | 512 MB | $135.00 | Medium traffic: covers 3,000 RPS steady |
| 150 | 512 MB | $405.00 | Full 10K RPS: eliminates all cold starts |

**Recommendation:**
- **Dev/staging:** Provisioned concurrency = 2 (just eliminate cold starts during testing)
- **Production launch:** Start with provisioned = 10, enable Application Auto Scaling
- **Production at scale:** Auto Scale between 10-150 based on `ProvisionedConcurrencyUtilization`

### Auto Scaling Configuration

```hcl
# Terraform: deploy/terraform/modules/lambda/main.tf
resource "aws_appautoscaling_target" "redirect" {
  max_capacity       = 150
  min_capacity       = 10
  resource_id        = "function:${aws_lambda_function.redirect.function_name}:${aws_lambda_alias.redirect_live.name}"
  scalable_dimension = "lambda:function:ProvisionedConcurrency"
  service_namespace  = "lambda"
}

resource "aws_appautoscaling_policy" "redirect" {
  name               = "shorty-redirect-autoscale"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.redirect.resource_id
  scalable_dimension = aws_appautoscaling_target.redirect.scalable_dimension
  service_namespace  = aws_appautoscaling_target.redirect.service_namespace

  target_tracking_scaling_policy_configuration {
    target_value = 0.7  # Scale up when 70% of provisioned capacity is in use
    predefined_metric_specification {
      predefined_metric_type = "LambdaProvisionedConcurrencyUtilization"
    }
  }
}
```

---

## Memory Utilization Monitoring

### CloudWatch Metrics to Track

```
# Per-function metrics (Lambda Insights)
aws/lambda/Duration                    — p50, p99, max
aws/lambda/MaxMemoryUsed              — track utilization ratio
aws/lambda/ConcurrentExecutions       — sizing provisioned concurrency
aws/lambda/ProvisionedConcurrencyUtilization — auto-scaling input

# Custom metrics to emit from Go code
shorty.lambda.memory_used_mb          — from /proc/self/status (VmRSS)
shorty.lambda.gc_pause_ns             — from runtime.ReadMemStats()
shorty.lambda.heap_alloc_mb           — from runtime.ReadMemStats()
```

### Alerting Thresholds

| Metric | Warning | Critical | Action |
|---|---|---|---|
| MaxMemoryUsed / MemorySize | > 70% | > 85% | Increase memory |
| Duration p99 | > 80 ms | > 100 ms | Investigate, increase memory |
| GC pause p99 | > 5 ms | > 10 ms | Increase GOGC or memory |
| ProvisionedConcurrencyUtilization | > 80% | > 95% | Check auto-scaling |
| ConcurrentExecutions | > 80% of account limit | > 90% | Request limit increase |

### Runtime Metrics Collection

Add to `cmd/redirect/main.go` init or handler wrapper:

```go
func reportMemStats() {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    // Emit as CloudWatch EMF (Embedded Metric Format) or OTel gauge
    telemetry.RecordGauge("lambda.heap_alloc_mb", float64(m.HeapAlloc)/(1024*1024))
    telemetry.RecordGauge("lambda.gc_pause_p99_ms", float64(m.PauseNs[(m.NumGC+255)%256])/1e6)
    telemetry.RecordGauge("lambda.num_gc", float64(m.NumGC))
}
```

Call `reportMemStats()` every N requests (e.g., every 100) to avoid overhead on every invocation.

---

## Lambda Power Tuning (Automated)

For precise sizing, use [AWS Lambda Power Tuning](https://github.com/alexcasalboni/aws-lambda-power-tuning):

```bash
# Deploy the Step Functions state machine (one-time)
sam deploy --template-url https://github.com/alexcasalboni/aws-lambda-power-tuning/...

# Run the tuning
aws stepfunctions start-execution \
  --state-machine-arn arn:aws:states:us-east-1:ACCOUNT:stateMachine:powerTuningStateMachine \
  --input '{
    "lambdaARN": "arn:aws:lambda:us-east-1:ACCOUNT:function:shorty-redirect",
    "powerValues": [128, 256, 512, 1024, 1536],
    "num": 50,
    "payload": "{\"rawPath\":\"/test123\",\"requestContext\":{\"http\":{\"method\":\"GET\",\"sourceIP\":\"203.0.113.1\"}},\"headers\":{}}",
    "parallelInvocation": true,
    "strategy": "balanced"
  }'
```

This runs 50 invocations at each memory size and produces a cost-vs-latency chart. Use this to validate the 512 MB recommendation with real data.

---

## Summary

| Lambda | Recommended Memory | Provisioned Concurrency | Est. Monthly Cost |
|---|---|---|---|
| redirect | 512 MB | 10 (auto-scale to 150) | $10 + $27-405 (PC) |
| api | 256 MB | 0 (on-demand) | $0.83 |
| worker | 128 MB | 0 (on-demand) | $3.33 |

**Total estimated Lambda cost at 10K RPS: $41-420/month** depending on provisioned concurrency scaling.
