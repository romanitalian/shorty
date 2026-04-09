# Service Level Objectives (SLOs)

Shorty URL Shortener -- SLO/SLI definitions, error budgets, and burn rate alerting.

---

## 1. SLO Summary

| SLI | SLO Target | Measurement Window |
|-----|------------|-------------------|
| Availability (redirect) | 99.9% | 30-day rolling |
| Redirect latency (p99) | < 100 ms | 5-minute rolling |
| API latency (p99) | < 300 ms | 5-minute rolling |
| Error rate (5xx) | < 0.1% | 5-minute rolling |

---

## 2. SLI Definitions

### 2.1 Availability SLI

**Definition:** The ratio of successful redirect responses (HTTP 2xx + 3xx) to total redirect requests, excluding client errors (4xx) caused by invalid input.

**Formula:**

```
Availability = (total_redirect_requests - server_errors) / total_redirect_requests * 100
```

**Measurement sources:**

| Environment | Source | Metric / Query |
|-------------|--------|---------------|
| Production (AWS) | CloudWatch Metrics | API Gateway `5XXError` and `Count` metrics for the redirect route |
| Production (AWS) | CloudWatch Logs Insights | See query below |
| Local | Prometheus | `shorty_redirects_total{status=~"5.."}` / `shorty_redirects_total` |

**CloudWatch Insights query -- Availability:**

```sql
filter @message like /shorty-redirect/
| stats
    count(*) as total,
    sum(case when @status >= 500 then 1 else 0 end) as errors,
    (1 - sum(case when @status >= 500 then 1 else 0 end) / count(*)) * 100 as availability_pct
| filter ispresent(@status)
```

**API Gateway CloudWatch metric query:**

```sql
SELECT
  (1 - METRICS('5XXError') / METRICS('Count')) * 100 AS availability_pct
FROM SCHEMA("AWS/ApiGateway", ApiId, Stage, Route)
WHERE ApiId = '{api_id}'
  AND Route = 'GET /{code}'
  AND Stage = 'live'
PERIOD 300
```

**Prometheus (local):**

```promql
# 30-day rolling availability
(
  1 - (
    sum(increase(shorty_redirects_total{status=~"5.."}[30d]))
    /
    sum(increase(shorty_redirects_total[30d]))
  )
) * 100
```

### 2.2 Redirect Latency SLI

**Definition:** The 99th percentile of redirect request duration, measured from API Gateway request receipt to response sent. Includes cache lookup (Redis), DynamoDB fallback, and SQS publish.

**Measurement sources:**

| Environment | Source | Metric |
|-------------|--------|--------|
| Production | CloudWatch | API Gateway `Latency` metric (p99 statistic) |
| Production | X-Ray | Service map latency percentiles for `shorty-redirect` |
| Local | Prometheus | `shorty_redirect_duration_seconds` histogram |

**CloudWatch Insights query -- p99 latency:**

```sql
filter @message like /shorty-redirect/
| stats pct(@duration, 99) as p99_ms by bin(5m) as time_window
```

**Prometheus (local):**

```promql
histogram_quantile(0.99, sum(rate(shorty_redirect_duration_seconds_bucket[5m])) by (le))
```

### 2.3 API Latency SLI

**Definition:** The 99th percentile of API request duration for all `/api/v1/*` endpoints.

**CloudWatch Insights query:**

```sql
filter @message like /shorty-api/
| stats pct(@duration, 99) as p99_ms by bin(5m) as time_window
```

**Prometheus (local):**

```promql
histogram_quantile(0.99, sum(rate(shorty_redirect_duration_seconds_bucket{service="shorty-api"}[5m])) by (le))
```

### 2.4 Error Rate SLI

**Definition:** The percentage of all requests (redirect + API) that return a 5xx status code.

**CloudWatch Insights query:**

```sql
filter @message like /shorty/
| stats
    sum(case when @status >= 500 then 1 else 0 end) / count(*) * 100 as error_rate_pct
  by bin(5m) as time_window
```

**Prometheus (local):**

```promql
sum(rate(shorty_redirects_total{status=~"5.."}[5m]))
/
sum(rate(shorty_redirects_total[5m]))
* 100
```

---

## 3. Error Budget

### 3.1 Monthly Error Budget (30 days)

With a 99.9% availability SLO:

| Metric | Value |
|--------|-------|
| Allowed downtime per month | 43.2 minutes |
| Allowed downtime per quarter | 129.6 minutes (~2.16 hours) |
| Allowed downtime per year | 525.6 minutes (~8.76 hours) |
| Error budget (as request ratio) | 0.1% of total requests |

**Calculation:**

```
Monthly minutes       = 30 * 24 * 60 = 43,200 minutes
Error budget          = 43,200 * (1 - 0.999) = 43.2 minutes
```

At 10,000 RPS baseline:

```
Total requests/month  = 10,000 * 60 * 60 * 24 * 30 = 25,920,000,000
Allowed failures      = 25,920,000,000 * 0.001 = 25,920,000 failed requests
```

### 3.2 Quarterly Error Budget

```
Quarterly minutes     = 90 * 24 * 60 = 129,600 minutes
Error budget          = 129,600 * (1 - 0.999) = 129.6 minutes
```

### 3.3 Error Budget Consumption Tracking

Error budget consumed is tracked as a percentage:

```
budget_consumed_pct = (actual_downtime_minutes / 43.2) * 100   # monthly
```

**CloudWatch Insights -- budget consumption:**

```sql
filter @message like /shorty-redirect/ and @status >= 500
| stats count(*) as error_count by bin(1d)
```

---

## 4. Burn Rate Alerting

Burn rate measures how fast the error budget is being consumed relative to the SLO window. A burn rate of 1.0 means the budget is consumed evenly over the window. Higher burn rates indicate accelerated consumption.

### 4.1 Burn Rate Thresholds

| Severity | Burn Rate | Window | Budget Consumed | Action |
|----------|-----------|--------|-----------------|--------|
| **Critical (SEV1)** | 14.4x | 1 hour | 2% of monthly budget | Page on-call immediately |
| **Critical (SEV1)** | 6x | 6 hours | 5% of monthly budget | Page on-call immediately |
| **Warning (SEV2)** | 3x | 24 hours | 10% of monthly budget | Alert to Slack, investigate |
| **Info (SEV3)** | 1x | 72 hours | 10% of monthly budget | Notify during business hours |

### 4.2 Burn Rate Calculation

```
burn_rate = (error_rate_in_window / (1 - SLO)) 
```

For 99.9% SLO:

```
burn_rate = error_rate_in_window / 0.001
```

**Example:** If 2% of requests fail in a 1-hour window:

```
burn_rate = 0.02 / 0.001 = 20x  --> Critical alert
```

### 4.3 Multi-Window Burn Rate Alerts (Prometheus)

To reduce false positives, each alert uses two windows: a long window for significance and a short window for recency.

**Critical -- 1h/5m multi-window (14.4x burn):**

```promql
# Long window: 1-hour error rate exceeds 14.4x burn rate
(
  sum(rate(shorty_redirects_total{status=~"5.."}[1h]))
  /
  sum(rate(shorty_redirects_total[1h]))
) > (14.4 * 0.001)
and
# Short window: confirms the issue is still active
(
  sum(rate(shorty_redirects_total{status=~"5.."}[5m]))
  /
  sum(rate(shorty_redirects_total[5m]))
) > (14.4 * 0.001)
```

**Warning -- 6h/30m multi-window (6x burn):**

```promql
(
  sum(rate(shorty_redirects_total{status=~"5.."}[6h]))
  /
  sum(rate(shorty_redirects_total[6h]))
) > (6 * 0.001)
and
(
  sum(rate(shorty_redirects_total{status=~"5.."}[30m]))
  /
  sum(rate(shorty_redirects_total[30m]))
) > (6 * 0.001)
```

### 4.4 CloudWatch Alarms (Production)

```hcl
# Critical: >2% error budget consumed in 1 hour
resource "aws_cloudwatch_metric_alarm" "redirect_burn_rate_critical" {
  alarm_name          = "shorty-redirect-burn-rate-critical"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 3
  metric_name         = "5XXError"
  namespace           = "AWS/ApiGateway"
  period              = 60
  statistic           = "Average"
  threshold           = 0.0144  # 14.4x burn rate = 1.44% error rate
  alarm_description   = "Redirect error budget burn rate critical (14.4x). 2% of monthly budget consumed in 1 hour."
  dimensions = {
    ApiId = var.api_gateway_id
    Stage = "live"
    Route = "GET /{code}"
  }
  alarm_actions = [aws_sns_topic.pagerduty_critical.arn]
  ok_actions    = [aws_sns_topic.pagerduty_critical.arn]
}

# Warning: >5% error budget consumed in 6 hours
resource "aws_cloudwatch_metric_alarm" "redirect_burn_rate_warning" {
  alarm_name          = "shorty-redirect-burn-rate-warning"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 6
  datapoints_to_alarm = 4
  metric_name         = "5XXError"
  namespace           = "AWS/ApiGateway"
  period              = 300
  statistic           = "Average"
  threshold           = 0.006  # 6x burn rate = 0.6% error rate
  alarm_description   = "Redirect error budget burn rate warning (6x). 5% of monthly budget consumed in 6 hours."
  dimensions = {
    ApiId = var.api_gateway_id
    Stage = "live"
    Route = "GET /{code}"
  }
  alarm_actions = [aws_sns_topic.slack_warnings.arn]
  ok_actions    = [aws_sns_topic.slack_warnings.arn]
}
```

---

## 5. Error Budget Policy

### 5.1 Budget Status Tiers

| Budget Remaining | Status | Policy |
|-----------------|--------|--------|
| > 50% | **Green** | Normal feature velocity. Deployments proceed without additional gates. |
| 25-50% | **Yellow** | Increased caution. All deployments require SRE sign-off. Risk assessments mandatory for infrastructure changes. |
| 10-25% | **Orange** | Feature freeze. Only bug fixes and reliability improvements may be deployed. Mandatory canary deployments with 5% traffic for 30 minutes minimum. |
| < 10% | **Red** | Full freeze. Only P1/P2 incident fixes may be deployed. All engineering effort directed at reliability. Postmortem required for any further budget consumption. |
| 0% (exhausted) | **Exhausted** | Emergency freeze. No deployments except security patches and incident rollbacks. Weekly SLO review meetings until budget replenishes. Escalate to engineering leadership. |

### 5.2 Budget Review Cadence

| Review | Frequency | Attendees |
|--------|-----------|-----------|
| SLO dashboard check | Daily (automated Slack post) | On-call engineer |
| Error budget review | Weekly | SRE + Tech Lead |
| Quarterly SLO review | Every 90 days | SRE + Engineering + Product |

### 5.3 Budget Replenishment

Error budget is calculated on a **30-day rolling window**. Budget replenishes as older errors age out of the window. There is no manual reset.

---

## 6. SLO Per Service

### 6.1 shorty-redirect (hot path)

| SLI | SLO | Rationale |
|-----|-----|-----------|
| Availability | 99.9% | Revenue-impacting -- failed redirects break shortened links for all users |
| p50 latency | < 20 ms | Cache-first path (Redis); DynamoDB fallback should be rare |
| p99 latency | < 100 ms | Documented in requirements-init.md section 3.1 |
| Error rate (5xx) | < 0.1% | Includes Lambda errors, DynamoDB/Redis failures, timeouts |

**Key metrics (Prometheus):**
- `shorty_redirects_total{code, status}` -- success/failure counter
- `shorty_redirect_duration_seconds` -- histogram with buckets: 10ms, 25ms, 50ms, 100ms, 250ms, 500ms
- `shorty_cache_hits_total{result="hit"|"miss"}` -- cache performance

### 6.2 shorty-api (CRUD + stats)

| SLI | SLO | Rationale |
|-----|-----|-----------|
| Availability | 99.9% | Dashboard and management operations |
| p99 latency | < 300 ms | Higher complexity (auth, DynamoDB queries, pagination) |
| Error rate (5xx) | < 0.1% | Excludes 401/403/404 (client errors) |

**Key metrics (Prometheus):**
- `shorty_links_created_total{plan}` -- link creation by plan tier
- `shorty_rate_limit_hits_total{type, plan}` -- rate limiter activations

### 6.3 shorty-worker (SQS consumer)

| SLI | SLO | Rationale |
|-----|-----|-----------|
| Processing latency | < 30 s from SQS publish to DynamoDB write | Not user-facing; analytics pipeline |
| Error rate | < 1% | Click events are best-effort; DLQ catches failures |
| DLQ depth | < 100 messages | Non-zero DLQ indicates systematic failure |

**Key metrics (Prometheus):**
- `shorty_click_queue_depth` -- SQS approximate message count
- `shorty_worker_batch_duration_seconds` -- batch processing time

---

## 7. Dependency SLIs

These are not Shorty SLOs but upstream dependencies whose health directly affects Shorty SLOs.

| Dependency | Health Signal | Alert Threshold |
|------------|--------------|-----------------|
| DynamoDB | `ThrottledRequests > 0` | Any throttle for 2 consecutive minutes |
| DynamoDB | `SystemErrors > 0` | Any system error |
| ElastiCache Redis | `EngineCPUUtilization > 80%` | Sustained for 5 minutes |
| ElastiCache Redis | `CurrConnections` approaching `maxclients` | > 80% of pool limit |
| SQS FIFO | `ApproximateAgeOfOldestMessage > 60s` | Messages backing up |
| SQS DLQ | `ApproximateNumberOfMessagesVisible > 0` | Any message in DLQ |
| Lambda | `Throttles > 0` | Any throttle event |
| API Gateway | `IntegrationLatency p99 > 200ms` | Backend slow |

**CloudWatch Insights -- DynamoDB throttling:**

```sql
filter @message like /DynamoDB/ and @message like /ThrottlingException/
| stats count(*) as throttle_count by bin(5m) as time_window
```

---

## 8. SLO Dashboards

### 8.1 Grafana (Local)

The SLO overview is displayed on the Grafana "Overview" dashboard provisioned from `config/grafana/dashboards/`. Key panels:
- 30-day rolling availability percentage
- Error budget remaining (gauge: green/yellow/orange/red)
- p99 latency vs target (time series with threshold line)
- Burn rate (current 1h, 6h, 24h windows)

### 8.2 CloudWatch (Production)

Create a CloudWatch dashboard named `shorty-slo` with widgets:
- Availability (5-min granularity, 30-day window)
- Error budget remaining (custom metric published by a scheduled Lambda)
- p99 latency by function
- Burn rate alarm status

---

## 9. Appendix: Histogram Bucket Configuration

The `shorty_redirect_duration_seconds` histogram uses the following buckets, aligned with latency targets:

```go
// From internal/telemetry or handler initialization
prometheus.DefBuckets replaced with:
Buckets: []float64{0.010, 0.025, 0.050, 0.100, 0.250, 0.500}
// 10ms, 25ms, 50ms, 100ms (p99 target), 250ms, 500ms
```

This bucket layout provides high resolution around the 100ms p99 target while capturing outliers up to 500ms.
