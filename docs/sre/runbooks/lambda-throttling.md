# Runbook: Lambda Throttling

**Alert:** `LambdaThrottling` -- Lambda function is being throttled
**Severity:** SEV2/SEV3
**SLO Impact:** Throttled invocations return 502/429, directly consuming error budget (99.9% availability target)

---

## Symptoms

- `LambdaThrottling` alert firing in Prometheus / PagerDuty.
- CloudWatch `Throttles` metric > 0 for any `shorty-*` function.
- API Gateway returning 502 Bad Gateway or 429 Too Many Requests.
- Elevated 5xx error rate on the redirect or API service.
- Users reporting "link not working" or intermittent failures.

---

## Diagnosis

### 1. Identify which function is throttled

```bash
# Check throttle counts for all shorty functions (last 15 min)
for fn in shorty-redirect shorty-api shorty-worker; do
  echo "=== $fn ==="
  aws cloudwatch get-metric-statistics \
    --namespace AWS/Lambda \
    --metric-name Throttles \
    --dimensions Name=FunctionName,Value=$fn \
    --start-time "$(date -u -d '15 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
    --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
    --period 60 --statistics Sum
done
```

### 2. Check current concurrency configuration

```bash
# Reserved concurrency per function
aws lambda get-function-concurrency --function-name shorty-redirect
aws lambda get-function-concurrency --function-name shorty-api
aws lambda get-function-concurrency --function-name shorty-worker

# Account-level concurrency limit
aws lambda get-account-settings --query 'AccountLimit.ConcurrentExecutions'
```

### 3. Check provisioned concurrency (redirect Lambda)

```bash
aws lambda list-provisioned-concurrency-configs \
  --function-name shorty-redirect

# Check provisioned concurrency utilization
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name ProvisionedConcurrencyUtilization \
  --dimensions Name=FunctionName,Value=shorty-redirect Name=Resource,Value=shorty-redirect:live \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Maximum
```

### 4. Check concurrent execution count

```bash
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name ConcurrentExecutions \
  --dimensions Name=FunctionName,Value=shorty-redirect \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Maximum
```

### 5. Check for traffic spike or abuse

```bash
# CloudWatch Insights -- top short codes by request volume
aws logs start-query \
  --log-group-name /aws/lambda/shorty-redirect \
  --start-time "$(date -u -d '30 minutes ago' +%s)" \
  --end-time "$(date -u +%s)" \
  --query-string '
    filter @type = "REPORT"
    | stats count(*) as invocations, max(@duration) as max_duration_ms by bin(1m)
  '
```

---

## Mitigation

### Step 1: Increase reserved concurrency (immediate)

```bash
# Increase to 2000 (adjust based on account limit)
aws lambda put-function-concurrency \
  --function-name shorty-redirect \
  --reserved-concurrent-executions 2000
```

### Step 2: Scale provisioned concurrency (redirect Lambda)

```bash
# Scale up provisioned concurrency to reduce cold starts during burst
aws lambda put-provisioned-concurrency-config \
  --function-name shorty-redirect \
  --qualifier live \
  --provisioned-concurrent-executions 50
```

Note: Provisioned concurrency takes a few minutes to allocate. Monitor the `ProvisionedConcurrencyUtilization` metric.

### Step 3: If account-level limit reached

```bash
# Check total account usage
aws lambda get-account-settings

# Request limit increase via AWS Support
aws support create-case \
  --subject "Lambda concurrent execution limit increase" \
  --communication-body "Shorty URL shortener experiencing throttling. Request increase from current limit to 5000." \
  --service-code "lambda" \
  --category-code "general-guidance" \
  --severity-code "urgent"
```

### Step 4: Block abusive traffic (if single code is receiving abnormal traffic)

If a single short code is generating disproportionate traffic, add a WAF rate-based rule to throttle the specific pattern.

```bash
# Check WAF for existing rules
aws wafv2 get-web-acl --name shorty-waf --scope REGIONAL --id {waf_id}
```

---

## Prevention

1. **Auto-scaling provisioned concurrency:** Configure Application Auto Scaling on the redirect Lambda provisioned concurrency based on `ProvisionedConcurrencyUtilization`.
2. **Reserved concurrency headroom:** Keep reserved concurrency at least 2x the observed peak. Review monthly.
3. **Account limit planning:** Monitor account-level concurrency usage. Request increases proactively before expected traffic growth.
4. **WAF rate limiting:** Ensure WAF rate-based rules are in place to cap per-IP request rates before they exhaust Lambda concurrency.
5. **CloudFront caching:** For popular short codes, ensure CloudFront cache behavior is correctly configured to absorb repeat requests before they hit Lambda.

---

## Escalation

| Condition | Action |
|-----------|--------|
| Throttling continues after increasing reserved concurrency | Escalate to Engineering Lead -- may need architecture review |
| Account-level limit reached | Open AWS Support case (Severity: Urgent) |
| Throttling caused by DDoS/abuse | Engage Security team + update WAF rules |
| SLO burn rate enters critical (14.4x) | Follow [high-error-rate.md](high-error-rate.md) escalation path |
