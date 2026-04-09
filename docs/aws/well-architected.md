# AWS Well-Architected Review -- Shorty URL Shortener

Review of the Shorty architecture against the five pillars of the AWS Well-Architected Framework. For each pillar: current design assessment, identified gaps, and prioritized recommendations.

**Review date:** April 2026
**Target environment:** Production (us-east-1)
**Architecture:** Serverless -- Lambda (Go/ARM64), API Gateway v2, DynamoDB, ElastiCache Redis, SQS FIFO, CloudFront, WAF, Cognito

---

## Pillar 1: Operational Excellence

### Current Design

| Area | Status | Details |
|------|--------|---------|
| Infrastructure as Code | GOOD | Terraform modules for all AWS resources (`deploy/terraform/modules/`) |
| CI/CD Pipeline | GOOD | GitHub Actions: spec-validate, lint, test, BDD, build, deploy, canary |
| Deployment Strategy | GOOD | Lambda alias weighted routing for canary deployments (10% -> 100%) |
| Automatic Rollback | GOOD | Error rate > 1% triggers automatic rollback via CloudWatch alarm |
| Spec-First Development | EXCELLENT | OpenAPI spec is single source of truth; generated stubs; BDD tests before implementation |
| Monitoring | GOOD | CloudWatch metrics, X-Ray tracing, custom dashboards (Grafana locally) |
| Structured Logging | GOOD | JSON structured logs with trace_id, span_id correlation |
| Runbooks | PLANNED | `docs/sre/` directory exists but runbooks are not yet written |

### Gaps

| Gap | Priority | Remediation |
|-----|----------|-------------|
| No operational runbooks | HIGH | Write runbooks for: DynamoDB throttling, Lambda cold start spikes, Redis failover, SQS DLQ processing, certificate renewal |
| No automated incident response | MEDIUM | Configure CloudWatch Composite Alarms -> SNS -> PagerDuty/Slack integration |
| No game days | LOW | Schedule quarterly game days: simulate Redis failure, DynamoDB throttling, Lambda concurrency exhaustion |
| CloudFormation drift detection not configured | MEDIUM | Enable Terraform Cloud or add `terraform plan` drift detection to weekly CI job |
| No change management process for Terraform | MEDIUM | Require `terraform plan` output as PR comment; manual approval gate for prod applies |

### Recommendations

1. **Write runbooks** (`docs/sre/runbooks/`) for the top 5 failure scenarios before production launch.
2. **Configure CloudWatch Composite Alarms** that combine multiple signals (error rate + latency + throttling) to reduce alert fatigue.
3. **Add Terraform plan output** as a GitHub Actions step that posts the plan diff as a PR comment for review.
4. **Implement operational dashboards** in CloudWatch (not just Grafana for local dev) showing: Lambda errors/throttles, DynamoDB consumed vs provisioned capacity, SQS queue depth, Redis cache hit ratio.

---

## Pillar 2: Security

### Current Design

| Area | Status | Details |
|------|--------|---------|
| IAM Least Privilege | EXCELLENT | Per-Lambda roles with minimal permissions, no wildcards except X-Ray/CW Metrics |
| Encryption at Rest | GOOD | DynamoDB SSE (AWS-managed KMS), S3 SSE-S3 |
| Encryption in Transit | GOOD | TLS 1.2+ enforced on all endpoints (CloudFront, API GW, ElastiCache in-transit encryption) |
| Secrets Management | GOOD | AWS Secrets Manager for IP salt, JWT keys, Redis AUTH token |
| PII Protection | EXCELLENT | IP addresses SHA-256 hashed with secret salt; no PII in logs |
| Authentication | GOOD | Cognito User Pool with Google SSO; JWT validation in Lambda authorizer |
| WAF Protection | GOOD | AWS WAF with rate limiting, bot control, OWASP rules, IP blocklist |
| CSRF Protection | GOOD | SameSite=Strict cookies, CSRF token on password forms |
| DDoS Protection | GOOD | CloudFront + AWS Shield Standard + WAF rate-based rules |
| No SQL Injection | GOOD | Parameterized DynamoDB SDK queries (no raw query construction) |

### Gaps

| Gap | Priority | Remediation |
|-----|----------|-------------|
| No KMS customer-managed keys (CMK) | LOW | Default AWS-managed keys are sufficient for MVP. Consider CMK for enterprise customers needing key rotation control. |
| No VPC Flow Logs | MEDIUM | Enable VPC Flow Logs to detect unauthorized network access attempts to ElastiCache |
| No AWS Config rules | MEDIUM | Enable AWS Config to detect non-compliant resources (e.g., unencrypted DynamoDB, public S3 buckets) |
| No GuardDuty | MEDIUM | Enable GuardDuty for threat detection on Lambda, DynamoDB, and S3 |
| TLS 1.3 not enforced | LOW | CloudFront supports TLS 1.3 but API Gateway v2 uses TLS 1.2 minimum. Accept TLS 1.2 for now. |
| No Security Hub | LOW | Enable Security Hub for centralized security findings aggregation after GuardDuty and Config are active |
| Redis AUTH token rotation not automated | HIGH | Implement Secrets Manager automatic rotation for the Redis AUTH token with a Lambda rotation function |
| No access logging on S3 artifact bucket | LOW | Enable S3 server access logging for audit trail |

### Recommendations

1. **Enable VPC Flow Logs** with CloudWatch Logs destination. Retain for 14 days. Alert on rejected traffic to ElastiCache subnet.
2. **Enable GuardDuty** in us-east-1. Monitor for credential compromise, unusual API calls, and Lambda invocation anomalies.
3. **Automate Redis AUTH token rotation** via Secrets Manager with a 90-day rotation schedule.
4. **Enforce TLS 1.2 minimum** on CloudFront custom domain using a Security Policy of `TLSv1.2_2021`.
5. **Add SCPs (Service Control Policies)** to prevent disabling CloudTrail, GuardDuty, or Config in the production account.

---

## Pillar 3: Reliability

### Current Design

| Area | Status | Details |
|------|--------|---------|
| Multi-AZ | GOOD | Lambda runs across 3 AZs (3 private subnets); ElastiCache Multi-AZ with automatic failover |
| DynamoDB Durability | EXCELLENT | DynamoDB stores data across 3 AZs automatically; 99.999% durability |
| Health Checks | PARTIAL | API Gateway has default health behavior; no custom health check endpoint |
| Retry Logic | GOOD | SQS FIFO with DLQ for failed click processing; DynamoDB retries via AWS SDK |
| Circuit Breaker | PLANNED | Documented in architecture but not yet implemented in Go code |
| Auto-Scaling | GOOD | Lambda auto-scales by default; DynamoDB clicks table has auto-scaling; reserved concurrency limits |
| Backup | GOOD | DynamoDB Point-in-Time Recovery (PITR) enabled |
| Disaster Recovery | PARTIAL | Single-region deployment; DynamoDB Global Tables planned but not configured |

### Gaps

| Gap | Priority | Remediation |
|-----|----------|-------------|
| No DynamoDB Global Tables | HIGH | Single-region failure would cause total outage. Configure Global Tables replication to us-west-2 for RTO < 5 min. |
| No custom health check endpoint | MEDIUM | Add `GET /health` endpoint that verifies DynamoDB and Redis connectivity |
| No circuit breaker implementation | MEDIUM | Implement circuit breaker in Go code for Redis and DynamoDB calls to prevent cascade failures |
| SQS DLQ has no processing | HIGH | DLQ messages are dropped silently. Implement a DLQ processor Lambda or CloudWatch alarm on DLQ depth. |
| No load shedding | MEDIUM | Under extreme load, the redirect Lambda should shed non-critical work (telemetry, analytics) to maintain core redirect functionality |
| ElastiCache single-cluster | LOW | Current design uses a single Redis cluster. At 50K RPS, consider Redis Cluster mode for horizontal scaling. |
| No chaos engineering | LOW | No failure injection testing. Plan Game Days with AWS Fault Injection Simulator (FIS). |

### Recommendations

1. **Configure DynamoDB Global Tables** to us-west-2 for disaster recovery. This satisfies the RPO < 1 min requirement (async replication lag is typically < 1 second).
2. **Add a `/health` endpoint** to the API Lambda that checks DynamoDB (GetItem on a sentinel key) and Redis (PING) with 2-second timeouts.
3. **Implement SQS DLQ alarm**: CloudWatch alarm on `ApproximateNumberOfMessagesVisible` > 0 on the DLQ, with SNS notification.
4. **Add circuit breaker** using a library like `sony/gobreaker` in the Redis client. If Redis is down, fall through to DynamoDB directly for redirect lookups.
5. **Document RTO/RPO** formally:
   - RTO target: < 5 minutes (met via multi-AZ Lambda + ElastiCache failover)
   - RPO target: < 1 minute (met via DynamoDB PITR + Global Tables replication)

---

## Pillar 4: Performance Efficiency

### Current Design

| Area | Status | Details |
|------|--------|---------|
| ARM64 (Graviton) | EXCELLENT | All Lambdas compiled for `GOARCH=arm64`. ~20% cheaper, ~10% faster for Go. |
| Provisioned Concurrency | GOOD | Redirect Lambda: 2 provisioned instances to eliminate cold starts on hot path |
| Caching Strategy | EXCELLENT | Redis cache-first for redirects; negative caching for 404s; sorted set rate limiter |
| Async Processing | EXCELLENT | Click events published to SQS asynchronously (goroutine + timeout) -- never blocks redirect |
| Memory Sizing | GOOD | Redirect: 512MB (benchmarked sweet spot for Go); API: 256MB; Worker: 128MB |
| Connection Pooling | GOOD | Redis connection pooling configured with min/max idle connections |
| GOMAXPROCS | GOOD | Set to 1 for Lambda (single vCPU allocation) |
| VPC Endpoints | GOOD | DynamoDB Gateway endpoint (free) avoids NAT costs; Interface endpoints for SQS, Secrets Manager, X-Ray |
| Batch Processing | GOOD | Worker uses BatchWriteItem (25 items); SQS batch receive (10 messages) |

### Gaps

| Gap | Priority | Remediation |
|-----|----------|-------------|
| No performance baseline | HIGH | No load test results documented. Run k6 baseline test and record p50/p99 latency before launch. |
| Cache hit ratio not monitored | MEDIUM | Add CloudWatch custom metric for cache hit/miss ratio. Alert if hit rate drops below 70%. |
| No connection reuse metrics | LOW | Instrument Redis connection pool metrics (active, idle, wait time) for right-sizing |
| Provisioned concurrency may be insufficient | MEDIUM | 2 instances handles ~2 concurrent requests without cold start. For 1K RPS, cold starts will occur during traffic spikes. Consider increasing to 5-10 for launch. |
| No CDN caching for 301 redirects | MEDIUM | All redirects bypass CloudFront cache. Links configured as 301 should be cacheable. |
| Worker batch size not tuned | LOW | SQS event source mapping batch size defaults to 10. Test with batch sizes of 25-50 for higher throughput. |

### Recommendations

1. **Run k6 load tests** (`tests/load/baseline.js`) and document results. Target: redirect p99 < 100ms at 1K RPS.
2. **Increase provisioned concurrency** to 5 for launch. Monitor cold start metrics for 1 week and adjust.
3. **Implement CloudFront caching** for 301 redirects with `Cache-Control: public, max-age=86400`. This reduces Lambda invocations and improves latency for repeat visitors.
4. **Add cache hit ratio metric** to CloudWatch. Create a dashboard widget showing hit rate over time.
5. **Benchmark Lambda memory settings** at 256/512/1024 MB for redirect Lambda. Document the cost-performance sweet spot.

---

## Pillar 5: Cost Optimization

### Current Design

| Area | Status | Details |
|------|--------|---------|
| Pay-per-Use | GOOD | Lambda, API Gateway, SQS, DynamoDB on-demand -- no idle cost for most services |
| ARM64 Savings | EXCELLENT | ARM64 Lambda is ~20% cheaper per GB-second than x86 |
| VPC Gateway Endpoint | GOOD | DynamoDB Gateway endpoint saves $0.045/GB vs NAT Gateway |
| Right-Sized Cache | GOOD | cache.t4g.medium for launch (23x headroom) -- not over-provisioned |
| DynamoDB Capacity Mode | GOOD | On-demand for bursty `links` table; provisioned for predictable `clicks` table |
| TTL-based Cleanup | GOOD | DynamoDB TTL handles expired link deletion automatically (no Lambda needed) |

### Gaps

| Gap | Priority | Remediation |
|-----|----------|-------------|
| No cost monitoring/budgets | HIGH | Configure AWS Budgets with alerts at 80%/100%/120% of estimates |
| No Savings Plans | MEDIUM | No committed-use discounts. Evaluate 1-year Compute Savings Plan after 1 month of production data. |
| SQS FIFO per-request cost is very high | HIGH | At 10K RPS, SQS FIFO costs ~$39K/month. Batch operations and/or switching to Kinesis Data Streams should be evaluated. |
| No log retention policies | MEDIUM | CloudWatch logs default to indefinite retention. Set 30-day retention for prod, 7-day for dev. |
| CloudWatch Logs ingestion cost | MEDIUM | At 10K RPS, log ingestion is ~$250/month. Reduce log verbosity in prod (ERROR + WARN only for redirect Lambda). |
| WAF Bot Control costs scale linearly | HIGH | At 10K RPS, Bot Control costs ~$26K/month. Scope Bot Control to API endpoints only, not redirect traffic. |
| No cost allocation tags | MEDIUM | Resources are not tagged for cost allocation reporting. Add Project/Environment/Component tags. |
| X-Ray costs at 100% sampling | HIGH | X-Ray at 100% sampling would cost >$13K/month at 1K RPS. Must use 1-5% sampling. |

### Recommendations

1. **Configure AWS Budgets** immediately with per-service and total monthly budget alerts.
2. **Scope WAF Bot Control** to API endpoints only. Redirect traffic does not need bot inspection (the redirect Lambda has its own rate limiting via Redis).
3. **Set log retention policies**: 30 days for prod Lambda logs, 14 days for WAF/API GW access logs, 7 days for dev.
4. **Implement SQS batch send** in the redirect Lambda. Buffer up to 10 click events before calling `SendMessageBatch`.
5. **Evaluate Kinesis Data Streams** as an alternative to SQS FIFO for click events at 10K+ RPS. Kinesis pricing is per-shard-hour ($0.015/shard-hour) rather than per-message, which is dramatically cheaper at high throughput.
6. **Add cost allocation tags** to all Terraform resources: `Project=shorty`, `Environment=dev|staging|prod`, `Component=redirect|api|worker|cache|queue|cdn|waf`.
7. **Set X-Ray sampling to 1%** for redirect Lambda, 10% for API Lambda, 5% for worker Lambda.

---

## Summary: Priority Matrix

| # | Finding | Pillar | Priority | Effort |
|---|---------|--------|----------|--------|
| 1 | Write operational runbooks | Ops Excellence | HIGH | Medium |
| 2 | Enable VPC Flow Logs | Security | MEDIUM | Low |
| 3 | Enable GuardDuty | Security | MEDIUM | Low |
| 4 | Automate Redis AUTH rotation | Security | HIGH | Medium |
| 5 | Configure DynamoDB Global Tables | Reliability | HIGH | Medium |
| 6 | Implement SQS DLQ alarm + processor | Reliability | HIGH | Low |
| 7 | Add /health endpoint | Reliability | MEDIUM | Low |
| 8 | Implement circuit breaker (Redis) | Reliability | MEDIUM | Medium |
| 9 | Run k6 load test baseline | Performance | HIGH | Low |
| 10 | Increase provisioned concurrency to 5 | Performance | MEDIUM | Low |
| 11 | Configure AWS Budgets | Cost | HIGH | Low |
| 12 | Scope WAF Bot Control to API only | Cost | HIGH | Low |
| 13 | Set CloudWatch log retention policies | Cost | MEDIUM | Low |
| 14 | Implement SQS batch send | Cost | HIGH | Medium |
| 15 | Evaluate Kinesis for click events | Cost | MEDIUM | High |
| 16 | Add cost allocation tags | Cost | MEDIUM | Low |

### Top 5 Actions Before Production Launch

1. **Configure AWS Budgets** -- prevent cost surprises on day 1
2. **Scope WAF Bot Control** -- avoid $26K+/month waste on redirect traffic
3. **Implement SQS batch operations** -- reduce SQS costs by 70-80%
4. **Run k6 baseline load test** -- validate p99 < 100ms target
5. **Write top 5 runbooks** -- ensure team can respond to incidents

---

## Appendix: Architecture Diagram Compliance

| AWS Service | Well-Architected Alignment |
|-------------|--------------------------|
| CloudFront | Edge protection, TLS termination, DDoS absorption |
| WAF | L7 filtering, rate limiting, bot control |
| API Gateway v2 | Request routing, throttling, CORS, access logging |
| Lambda (Go/ARM64) | Cost-efficient compute, auto-scaling, event-driven |
| DynamoDB | Fully managed, multi-AZ by default, auto-scaling |
| ElastiCache Redis | In-memory caching, rate limiting, multi-AZ failover |
| SQS FIFO | Decoupled async processing, exactly-once delivery |
| Cognito | Managed auth, SSO integration, token management |
| Secrets Manager | Centralized secrets, automatic rotation capable |
| X-Ray | Distributed tracing, service map, latency analysis |
| CloudWatch | Metrics, logs, alarms, dashboards |
| S3 | Artifact storage, log archival |
