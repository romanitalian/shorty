# Runbook: High Error Rate

**Alert:** `RedirectHighErrorRate` / `SLOBurnRateCritical1h` / `SLOBurnRateWarning6h` / `LambdaHighErrorRate`
**Severity:** SEV1 (burn rate > 14.4x or error rate > 5%) / SEV2 (burn rate > 6x or error rate > 1%)
**SLO Impact:** Directly consumes the 30-day error budget. At 14.4x burn rate, 2% of monthly budget is exhausted per hour. Availability SLO target: 99.9% (43.2 min/month allowed downtime).

---

## Symptoms

- `RedirectHighErrorRate` alert: 5xx error rate > 1% for 2+ minutes.
- `SLOBurnRateCritical1h` alert: error budget burning at 14.4x rate.
- `LambdaHighErrorRate` alert: Lambda error rate > 5%.
- API Gateway returning 502 Bad Gateway or 500 Internal Server Error.
- Users reporting broken short links.
- Error budget dashboard showing rapid consumption.

---

## Diagnosis

### 1. Check for recent deployments (most common cause)

```bash
# Last deployment time for each function
for fn in shorty-redirect shorty-api shorty-worker; do
  echo "=== $fn ==="
  aws lambda get-function --function-name $fn \
    --query 'Configuration.{LastModified: LastModified, Version: Version, CodeSha256: CodeSha256}'
done

# List recent versions (check if a new version was published)
aws lambda list-versions-by-function \
  --function-name shorty-redirect \
  --query 'Versions[-5:].[Version, Description, LastModified]'

# Check current alias target
aws lambda get-alias --function-name shorty-redirect --name live
```

### 2. Check Lambda error metrics

```bash
# Error count per function (last 15 min)
for fn in shorty-redirect shorty-api shorty-worker; do
  echo "=== $fn ==="
  aws cloudwatch get-metric-statistics \
    --namespace AWS/Lambda \
    --metric-name Errors \
    --dimensions Name=FunctionName,Value=$fn \
    --start-time "$(date -u -d '15 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
    --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
    --period 60 --statistics Sum
done
```

### 3. Check API Gateway 5xx rate

```bash
aws cloudwatch get-metric-statistics \
  --namespace AWS/ApiGateway \
  --metric-name 5XXError \
  --dimensions Name=ApiId,Value={api_id} \
  --start-time "$(date -u -d '15 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

### 4. Analyze Lambda error logs

```sql
-- CloudWatch Insights: top error messages
filter @message like /ERROR/ or @status >= 500
| stats count(*) as error_count by @message
| sort error_count desc
| limit 20
```

```sql
-- CloudWatch Insights: error timeline
filter @message like /ERROR/
| fields @timestamp, @message, @logStream
| sort @timestamp desc
| limit 100
```

### 5. Check downstream dependencies

```bash
# DynamoDB errors
aws cloudwatch get-metric-statistics \
  --namespace AWS/DynamoDB \
  --metric-name SystemErrors \
  --dimensions Name=TableName,Value=links \
  --start-time "$(date -u -d '15 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum

# Redis availability (check if redis exporter is up)
# See redis-connection-exhaustion.md for Redis diagnostics
```

### 6. Check X-Ray service map (visual)

Open the X-Ray service map in the AWS console to identify which downstream service has elevated errors or latency. Filter by `shorty-redirect` service.

### 7. Check canary health (synthetic monitoring)

```bash
# If CloudWatch Synthetics is configured
aws synthetics get-canary --name shorty-redirect-canary \
  --query 'Canary.Status'

aws synthetics get-canary-runs --name shorty-redirect-canary \
  --query 'CanaryRuns[:5].[Status.State, Timeline.Started]'
```

---

## Mitigation

### Step 1: Rollback Lambda alias (if recent deployment)

```bash
# Get the previous version
CURRENT=$(aws lambda get-alias --function-name shorty-redirect --name live --query 'FunctionVersion' --output text)
PREVIOUS=$((CURRENT - 1))

# Rollback to previous version
aws lambda update-alias \
  --function-name shorty-redirect \
  --name live \
  --function-version $PREVIOUS

# Verify the rollback
aws lambda get-alias --function-name shorty-redirect --name live
```

Repeat for `shorty-api` if that function was also recently deployed.

### Step 2: If not deployment-related -- check dependencies

Run through the other runbooks based on log analysis:

| Error Pattern | Runbook |
|---------------|---------|
| `ThrottlingException` (DynamoDB) | [dynamodb-throttling.md](dynamodb-throttling.md) |
| `dial tcp: i/o timeout` (Redis) | [redis-connection-exhaustion.md](redis-connection-exhaustion.md) |
| `TooManyRequestsException` (Lambda) | [lambda-throttling.md](lambda-throttling.md) |
| SQS-related errors | [sqs-dlq-growth.md](sqs-dlq-growth.md) |

### Step 3: If the root cause is unknown -- reduce blast radius

```bash
# Reduce traffic to the function while investigating
# Option A: Enable CloudFront maintenance page (if configured)
# Option B: Reduce Lambda concurrency to limit error blast radius
aws lambda put-function-concurrency \
  --function-name shorty-redirect \
  --reserved-concurrent-executions 100
```

### Step 4: Monitor recovery

After mitigation, confirm SLIs are recovering:

```sql
-- CloudWatch Insights: error rate over time
filter @message like /shorty-redirect/
| stats
    count(*) as total,
    sum(case when @status >= 500 then 1 else 0 end) as errors,
    (sum(case when @status >= 500 then 1 else 0 end) * 100.0 / count(*)) as error_rate_pct
  by bin(1m) as time_window
| sort time_window desc
```

Wait for error rate to stay below 0.1% for at least 30 minutes before declaring resolved.

---

## Prevention

1. **Canary deployments:** Always deploy Lambda updates through a weighted alias (e.g., 5% canary for 15 minutes) before full rollout.
2. **Automated rollback:** Configure CodeDeploy with CloudWatch alarm-based rollback on `5XXError > 1%`.
3. **Pre-deployment testing:** Ensure `make test-all` passes (unit + BDD + integration + E2E) before deploying.
4. **Error budget policy:** Follow the error budget tiers in `docs/sre/slo.md` section 5.1. In Yellow/Orange zones, require SRE sign-off for deployments.
5. **Dependency health checks:** Lambda initialization should verify DynamoDB and Redis connectivity. Fail fast on init rather than serving errors.
6. **Circuit breakers:** The redirect Lambda falls back to DynamoDB when Redis is unavailable. Ensure this fallback path is tested regularly.

---

## Escalation

| Condition | Action |
|-----------|--------|
| Error rate > 1% after rollback | Page Engineering Lead -- root cause is not the deployment |
| SLO burn rate > 14.4x for > 15 min | Declare SEV1 incident per [incident-response.md](../incident-response.md) |
| Error budget < 10% remaining | Full deploy freeze per error budget policy |
| AWS service issue suspected (DynamoDB, Lambda errors) | Open AWS Support case (Severity: Critical for SEV1) |
| Data loss or corruption suspected | Engineering Lead + Security team |
