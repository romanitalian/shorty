# Incident Response Procedure

Shorty URL Shortener -- incident detection, triage, mitigation, resolution, and postmortem process.

---

## 1. Severity Levels

| Severity | Definition | Examples | Response Time | Update Cadence | Resolution Target |
|----------|-----------|----------|---------------|----------------|-------------------|
| **SEV1 -- Critical** | Complete service outage or data loss. All or majority of redirects failing. Error budget burn rate > 14.4x. | Lambda function down, DynamoDB table unavailable, DNS failure, data corruption | **5 minutes** (page on-call) | Every 15 min | 1 hour |
| **SEV2 -- Major** | Significant degradation. Partial outage or SLO violation. Error budget burn rate > 6x. | Redis down (fallback to DynamoDB), p99 > 500ms, elevated 5xx rate (>1%), single AZ failure | **15 minutes** (page on-call) | Every 30 min | 4 hours |
| **SEV3 -- Minor** | Noticeable degradation, not user-impacting at scale. Error budget burn rate > 3x. | p99 latency > 200ms, cache hit ratio < 80%, DynamoDB throttling (low), SQS queue depth growing | **1 hour** (Slack alert) | Every 2 hours | 24 hours |
| **SEV4 -- Low** | Minor issue, no user impact. Informational. | Cold start rate elevated, rate limiter hits spike (potential bot), DLQ messages accumulating | **Next business day** | As needed | 1 week |

---

## 2. On-Call Rotation

### 2.1 Rotation Structure

| Role | Coverage | Escalation Timeout |
|------|----------|-------------------|
| **Primary on-call** | 24/7, 1-week rotation | 5 min (SEV1), 15 min (SEV2) |
| **Secondary on-call** | 24/7, 1-week rotation (shadow) | Auto-escalation if primary unresponsive after timeout |
| **Engineering lead** | Business hours, escalation only | Paged for SEV1 if not resolved in 30 min |

### 2.2 On-Call Expectations

- Acknowledge pages within the response time SLA for the severity level.
- Laptop and internet access required during on-call shifts.
- No alcohol consumption during on-call shifts.
- Handoff meeting at rotation change (brief review of active issues, recent incidents, known risks).
- On-call engineer has authority to roll back deployments, scale infrastructure, and page additional engineers.

### 2.3 Tools

| Tool | Purpose |
|------|---------|
| PagerDuty | Paging, escalation, on-call schedule |
| Slack `#shorty-incidents` | Real-time incident coordination |
| Slack `#shorty-alerts` | Automated alert notifications |
| AWS CloudWatch | Metrics, logs, alarms |
| Grafana (local/staging) | Dashboard monitoring |
| Jaeger / X-Ray | Distributed tracing |

---

## 3. Incident Lifecycle

```
Detection --> Triage --> Mitigation --> Resolution --> Postmortem
   |            |           |              |              |
 Alert or    Assign      Stop the       Fix root       Learn and
 human       severity,   bleeding       cause          improve
 report      IC role
```

### 3.1 Detection

Incidents are detected through:

1. **Automated alerts** -- CloudWatch alarms, Prometheus AlertManager rules, PagerDuty
2. **Error budget burn rate** -- SLO violation alerts (see `docs/sre/slo.md`)
3. **Human report** -- Customer complaint, internal observation
4. **Synthetic monitoring** -- Periodic health check requests to `GET /{known-code}`

### 3.2 Triage (first 5 minutes)

The on-call engineer performs initial triage:

1. **Acknowledge the page** in PagerDuty.
2. **Open `#shorty-incidents`** in Slack and post the initial assessment.
3. **Determine severity** using the definitions in section 1.
4. **Assign Incident Commander (IC)** -- for SEV1/SEV2, the on-call engineer is IC until explicitly handed off.
5. **Quick diagnostics:**

```bash
# Check Lambda errors (last 15 minutes)
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Errors \
  --dimensions Name=FunctionName,Value=shorty-redirect \
  --start-time $(date -u -d '15 minutes ago' +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 60 --statistics Sum

# Check API Gateway 5xx rate
aws cloudwatch get-metric-statistics \
  --namespace AWS/ApiGateway \
  --metric-name 5XXError \
  --dimensions Name=ApiId,Value={api_id} \
  --start-time $(date -u -d '15 minutes ago' +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 60 --statistics Sum

# Check recent deployments
aws lambda get-function --function-name shorty-redirect \
  --query 'Configuration.LastModified'
```

**CloudWatch Insights -- quick error scan:**

```sql
filter @message like /ERROR/ or @status >= 500
| fields @timestamp, @message, @logStream
| sort @timestamp desc
| limit 50
```

### 3.3 Mitigation (stop the bleeding)

Goal: restore service as fast as possible. Root cause investigation comes later.

**Common mitigations (in order of preference):**

| Action | When | Command |
|--------|------|---------|
| **Rollback Lambda** | Recent deployment suspected | `aws lambda update-alias --function-name shorty-redirect --name live --function-version {previous_version}` |
| **Increase concurrency** | Lambda throttling | `aws lambda put-function-concurrency --function-name shorty-redirect --reserved-concurrent-executions 2000` |
| **Scale DynamoDB** | Read/write throttling | `aws dynamodb update-table --table-name links --provisioned-throughput ReadCapacityUnits=5000,WriteCapacityUnits=1000` |
| **Flush Redis** | Corrupt cache data | `redis-cli -h {redis_endpoint} FLUSHDB` (caution: temporary latency spike) |
| **Enable WAF rate limiting** | DDoS or abuse | Update WAF rate-based rule threshold |
| **Disable worker** | Worker causing cascading failure | `aws lambda put-function-concurrency --function-name shorty-worker --reserved-concurrent-executions 0` |

### 3.4 Resolution

After mitigation stabilizes the service:

1. Identify and fix the root cause.
2. Deploy the fix through the normal CI/CD pipeline (with canary if applicable).
3. Verify SLIs have returned to normal for at least 30 minutes.
4. Close the incident in PagerDuty.
5. Post final status update to `#shorty-incidents`.

### 3.5 Postmortem

Required for all SEV1 and SEV2 incidents. Optional for SEV3. Not required for SEV4.

**Timeline:**
- Postmortem document drafted within **48 hours** of resolution.
- Review meeting within **5 business days**.
- Action items assigned and tracked in GitHub Issues with the `postmortem` label.

---

## 4. Escalation Path

```
On-call engineer (Primary)
    |
    | (5 min unresponsive, or needs help)
    v
On-call engineer (Secondary)
    |
    | (SEV1 not resolved in 30 min, or domain expertise needed)
    v
Engineering Lead
    |
    | (SEV1 not resolved in 1 hour, or customer-impacting data issue)
    v
VP Engineering / CTO
    |
    | (AWS infrastructure issue beyond our control)
    v
AWS Support (Enterprise Support plan)
```

### Escalation Triggers

| Condition | Escalation Target |
|-----------|-------------------|
| On-call unresponsive after timeout | Secondary on-call |
| SEV1 not mitigated within 30 min | Engineering Lead |
| AWS service issue suspected | AWS Support case (Severity: Critical) |
| Data loss or security breach | Engineering Lead + Security team |
| Customer-facing status page update needed | Engineering Lead + Communications |

---

## 5. Communication Templates

### 5.1 Slack -- Incident Declaration (`#shorty-incidents`)

```
:rotating_light: INCIDENT DECLARED

Severity: SEV{N}
Incident Commander: @{name}
Summary: {brief description of what is happening}
Impact: {user-facing impact}
Detection: {alert name / human report}
Status: INVESTIGATING

Tracking: {link to incident doc or PagerDuty URL}
```

### 5.2 Slack -- Status Update

```
:arrows_counterclockwise: INCIDENT UPDATE -- SEV{N}

Time: {HH:MM UTC}
Status: {INVESTIGATING | MITIGATING | MONITORING | RESOLVED}
Update: {what changed since last update}
Next steps: {planned actions}
ETA: {estimated time to resolution, if known}
```

### 5.3 Slack -- Incident Resolved

```
:white_check_mark: INCIDENT RESOLVED

Severity: SEV{N}
Duration: {start time} to {end time} ({total duration})
Summary: {what happened}
Root cause: {brief root cause}
Impact: {quantified impact -- error count, affected users, SLO budget consumed}
Postmortem: {link} (draft due by {date})
```

### 5.4 Status Page -- Investigating

```
Title: Degraded redirect performance
Body: We are investigating reports of increased latency and errors
      for URL redirects. Short link creation and the dashboard are
      not affected. We will provide an update within 30 minutes.
```

### 5.5 Status Page -- Resolved

```
Title: Redirect performance restored
Body: The issue causing increased redirect latency has been resolved.
      A configuration change was applied at {HH:MM UTC} that restored
      normal performance. Total duration: {X} minutes. We will publish
      a full postmortem within 48 hours.
```

---

## 6. Runbook Index

Each runbook provides step-by-step instructions for investigating and resolving a specific alert. Runbooks are stored in `docs/sre/runbooks/`.

| Alert | Runbook | Severity |
|-------|---------|----------|
| Error rate > 1% for 5 min | [high-error-rate.md](runbooks/high-error-rate.md) | SEV1/SEV2 |
| Redirect p99 > 500ms for 5 min | [high-latency.md](runbooks/high-latency.md) | SEV2 |
| DynamoDB throttled requests | [dynamodb-throttling.md](runbooks/dynamodb-throttling.md) | SEV2/SEV3 |
| Redis connection failures | [redis-unavailable.md](runbooks/redis-unavailable.md) | SEV2 |
| SQS queue depth > 10,000 | [sqs-queue-depth.md](runbooks/sqs-queue-depth.md) | SEV3 |
| Lambda cold starts > 10/min | [lambda-cold-starts.md](runbooks/lambda-cold-starts.md) | SEV3 |
| Rate limit hits > 1,000/min | [rate-limit-attack.md](runbooks/rate-limit-attack.md) | SEV3/SEV4 |

---

## 7. Common Failure Scenarios and Quick Mitigations

### 7.1 Lambda Throttling

**Symptoms:** `Throttles` metric > 0, elevated 5xx errors, API Gateway returning 502/429.

**Quick mitigation:**
1. Check current reserved concurrency: `aws lambda get-function-concurrency --function-name shorty-redirect`
2. Increase reserved concurrency: `aws lambda put-function-concurrency --function-name shorty-redirect --reserved-concurrent-executions 2000`
3. If account-level limit reached, request increase via AWS Support.
4. Check if a specific code is receiving abnormal traffic (potential abuse) and block via WAF if needed.

**CloudWatch Insights query:**

```sql
filter @type = "REPORT" and @message like /shorty-redirect/
| stats count(*) as invocations, max(@duration) as max_duration_ms by bin(1m)
```

### 7.2 DynamoDB Throttling

**Symptoms:** `ThrottledRequests` > 0 on `links` or `clicks` table. Elevated latency. Possible 5xx errors if retries exhausted.

**Quick mitigation:**
1. Check which table and operation: `aws cloudwatch get-metric-data` for `ConsumedReadCapacityUnits` and `ConsumedWriteCapacityUnits`.
2. If using provisioned capacity, increase: `aws dynamodb update-table --table-name links --provisioned-throughput ReadCapacityUnits=10000,WriteCapacityUnits=2000`
3. If using on-demand, check for hot partitions -- a single `LINK#{code}` receiving disproportionate traffic.
4. Enable DynamoDB auto-scaling if not already configured.

### 7.3 Redis Connection Exhaustion

**Symptoms:** `CurrConnections` near limit. Redirect latency spike. `shorty_cache_hits_total{result="miss"}` elevated. Connection timeout errors in logs.

**Quick mitigation:**
1. Check connection count: `redis-cli -h {endpoint} INFO clients`
2. Check pool configuration: Redis pool is set to `PoolSize: 5` per Lambda execution environment. With 1,000 concurrent Lambdas, max connections = 5,000.
3. If ElastiCache `maxclients` is the bottleneck, scale to a larger node type.
4. If connection leak suspected, redeploy the redirect Lambda to recycle execution environments: `aws lambda update-function-configuration --function-name shorty-redirect --description "recycle $(date +%s)"`

### 7.4 High Error Rate

**Symptoms:** `5XXError` > 1% on API Gateway. `shorty_redirects_total{status=~"5.."}` spiking. Error budget burning fast.

**Quick mitigation:**
1. Check if a recent deployment caused the issue: `aws lambda list-versions-by-function --function-name shorty-redirect --query 'Versions[-2:]'`
2. If recent deployment, rollback: `aws lambda update-alias --function-name shorty-redirect --name live --function-version {previous}`
3. If not deployment-related, check downstream dependencies (DynamoDB, Redis, SQS).
4. Check Lambda logs for specific error patterns:

```sql
filter @message like /ERROR/ and @message like /shorty-redirect/
| fields @timestamp, @message
| sort @timestamp desc
| limit 100
```

### 7.5 SQS DLQ Growing

**Symptoms:** `ApproximateNumberOfMessagesVisible > 0` on `shorty-clicks-dlq.fifo`. CloudWatch alarm triggered.

**Quick mitigation:**
1. Check DLQ message content: `aws sqs receive-message --queue-url {dlq_url} --max-number-of-messages 10`
2. Check worker Lambda errors: `aws logs filter-log-events --log-group-name /aws/lambda/shorty-worker --filter-pattern "ERROR"`
3. Common causes:
   - DynamoDB `clicks` table throttling -- increase capacity
   - Malformed click event -- fix serialization bug, redeploy worker
   - Worker Lambda timeout -- check if batch size is too large
4. Once root cause is fixed, replay DLQ messages:
   ```bash
   aws sqs start-message-move-task \
     --source-arn arn:aws:sqs:{region}:{account}:shorty-clicks-dlq.fifo \
     --destination-arn arn:aws:sqs:{region}:{account}:shorty-clicks.fifo
   ```

---

## 8. Postmortem Template

All postmortems are **blameless**. The goal is systemic improvement, not assigning blame to individuals.

Store postmortems in `docs/sre/postmortems/YYYY-MM-DD-{slug}.md`.

```markdown
# Postmortem: {Title}

**Date:** {YYYY-MM-DD}
**Severity:** SEV{N}
**Duration:** {start} to {end} ({total duration})
**Incident Commander:** {name}
**Author:** {name}

## Summary

{1-2 sentence summary of what happened and the impact.}

## Impact

- Error budget consumed: {X}%
- Affected requests: {count}
- User-facing impact: {description}
- Revenue impact: {if applicable}

## Timeline (UTC)

| Time | Event |
|------|-------|
| HH:MM | {Alert fired / Issue detected} |
| HH:MM | {On-call acknowledged} |
| HH:MM | {Triage completed, severity assigned} |
| HH:MM | {Mitigation action taken} |
| HH:MM | {Service restored} |
| HH:MM | {Root cause identified} |
| HH:MM | {Permanent fix deployed} |
| HH:MM | {Incident resolved} |

## Root Cause

{Detailed description of what caused the incident. Use the 5-whys technique.}

### 5 Whys

1. Why did {symptom}? Because {cause 1}.
2. Why did {cause 1}? Because {cause 2}.
3. Why did {cause 2}? Because {cause 3}.
4. Why did {cause 3}? Because {cause 4}.
5. Why did {cause 4}? Because {root cause}.

## Detection

- How was the incident detected? {Alert / human / customer report}
- Detection latency: {time from incident start to detection}
- Could we have detected it faster? {yes/no, how}

## Mitigation

{What was done to stop the bleeding? Was it effective? How long did it take?}

## Resolution

{What was done to permanently fix the root cause?}

## Lessons Learned

### What went well

- {Thing that worked}
- {Thing that worked}

### What went poorly

- {Thing that did not work}
- {Thing that did not work}

### Where we got lucky

- {Thing that could have been worse}

## Action Items

| Action | Owner | Priority | Issue |
|--------|-------|----------|-------|
| {Description} | {name} | P1/P2/P3 | #{issue_number} |
| {Description} | {name} | P1/P2/P3 | #{issue_number} |
| {Description} | {name} | P1/P2/P3 | #{issue_number} |
```

---

## 9. Incident Response Checklist (Quick Reference)

For the on-call engineer. Print this or bookmark it.

### On Alert Trigger

- [ ] Acknowledge page in PagerDuty
- [ ] Open `#shorty-incidents` in Slack
- [ ] Post incident declaration (section 5.1 template)
- [ ] Determine severity (section 1)
- [ ] Check recent deployments (`aws lambda get-function --query Configuration.LastModified`)
- [ ] Check CloudWatch dashboards (`shorty-slo`, `shorty-overview`)
- [ ] Check X-Ray service map for errors
- [ ] Identify failing component (Lambda, DynamoDB, Redis, SQS)
- [ ] Apply mitigation from section 7 or relevant runbook
- [ ] Post status update every {cadence per severity}

### After Mitigation

- [ ] Verify SLIs returned to normal (check for 30 min)
- [ ] Post resolution message (section 5.3 template)
- [ ] Update status page if it was updated during incident
- [ ] Close PagerDuty incident
- [ ] For SEV1/SEV2: create postmortem document within 48 hours
- [ ] Schedule postmortem review meeting within 5 business days
- [ ] File action items as GitHub Issues with `postmortem` label
