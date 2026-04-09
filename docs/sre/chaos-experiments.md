# Chaos Experiments

Shorty URL Shortener -- game day experiment designs for validating resilience under failure conditions.

**Prerequisites:**
- All experiments run against the **staging** environment only. Never run against production without explicit VP Engineering approval.
- The on-call engineer must be aware and available during all experiments.
- Each experiment has a maximum duration. If the abort conditions are met, terminate immediately.
- Monitoring dashboards (Grafana Overview, Cache Performance, Lambda Infrastructure) must be open during all experiments.

---

## Experiment 1: Redis Failure (Cache Unavailable)

### Hypothesis

When ElastiCache Redis becomes unavailable, the redirect Lambda falls back to DynamoDB for all cache lookups. The rate limiter fails open (allows all requests). Redirect p99 latency increases to under 200 ms but the service remains available. No data loss occurs.

### Background

The redirect hot path checks Redis first (`GET link:{code}`) and falls back to DynamoDB on cache miss or connection error. The rate limiter (`EVALSHA` Lua script) is configured to fail open -- if Redis is unreachable, requests proceed without rate limiting. WAF rate-based rules provide a secondary rate limiting layer.

### Method

1. **Baseline measurement (5 min):** Run k6 baseline test at 1,000 RPS. Record redirect p99, error rate, and cache hit ratio.

2. **Inject failure:** Modify the ElastiCache security group to deny all inbound traffic from the Lambda security group.
   ```bash
   aws ec2 revoke-security-group-ingress \
     --group-id sg-redis-XXXXX \
     --protocol tcp --port 6379 \
     --source-group sg-lambda-XXXXX
   ```

3. **Observe (10 min):** Continue k6 baseline test. Monitor:
   - `shorty_redirects_total{status}` -- should remain 2xx/3xx dominant
   - `shorty_redirect_duration_seconds` histogram -- p99 should increase
   - `shorty_cache_hits_total{result="miss"}` -- should spike to 100%
   - DynamoDB `ConsumedReadCapacityUnits` on `links` table -- should increase proportional to RPS
   - Lambda error logs -- should show Redis connection timeout, not panics

4. **Recovery:** Restore the security group rule.
   ```bash
   aws ec2 authorize-security-group-ingress \
     --group-id sg-redis-XXXXX \
     --protocol tcp --port 6379 \
     --source-group sg-lambda-XXXXX
   ```

5. **Post-recovery observation (5 min):** Verify cache hit ratio recovers to baseline within 5 minutes as the cache warms organically.

### Expected Outcome

| Metric | Baseline | During Failure | Recovery |
|--------|----------|---------------|----------|
| Redirect p99 | < 70 ms | < 200 ms | < 70 ms within 5 min |
| Error rate | < 0.1% | < 0.5% | < 0.1% |
| Cache hit ratio | > 85% | 0% | > 50% within 2 min |
| Availability | 99.9%+ | > 99.5% | 99.9%+ |

### Abort Conditions

- Error rate exceeds 5% for more than 2 minutes.
- Redirect p99 exceeds 500 ms for more than 2 minutes.
- DynamoDB `ThrottledRequests` exceeds 100/min (indicates DynamoDB cannot absorb the additional load).
- Any Lambda function crashes (not timeout, crash).

### Rollback

Restore the security group ingress rule (step 4 above). Redis connections re-establish automatically via go-redis reconnect logic. No Lambda redeployment needed.

### Validation Criteria (Pass/Fail)

- **Pass:** Service remains available (> 99.5%) with redirect p99 under 200 ms. No 5xx errors from DynamoDB fallback path. Rate limiter fails open without causing downstream overload.
- **Fail:** Service returns > 1% errors, or DynamoDB throttling causes cascading failure, or Lambda functions crash.

---

## Experiment 2: DynamoDB Throttling (Hot Partition Simulation)

### Hypothesis

When the `links` DynamoDB table experiences throttling, the redirect Lambda retries with exponential backoff. Redirect latency increases but remains under 500 ms for requests that succeed. The Redis cache absorbs most traffic, limiting the blast radius to cache-miss requests only.

### Background

DynamoDB throttles requests when consumed capacity exceeds provisioned capacity or when a single partition exceeds 3,000 RCU / 1,000 WCU. The AWS SDK automatically retries throttled requests with exponential backoff (up to 3 retries). Cache-hit requests never touch DynamoDB and are unaffected.

### Method

1. **Baseline measurement (5 min):** Record DynamoDB read/write latency, redirect p99, and cache hit ratio.

2. **Inject failure:** If using provisioned capacity, reduce `links` table read capacity to an artificially low level.
   ```bash
   aws dynamodb update-table \
     --table-name shorty-links-staging \
     --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5
   ```
   If using on-demand, use AWS Fault Injection Simulator (FIS) to inject DynamoDB errors:
   ```bash
   aws fis start-experiment \
     --experiment-template-id EXPT-dynamodb-throttle-XXXXX
   ```

3. **Observe (10 min):** Continue baseline load test. Monitor:
   - DynamoDB `ThrottledRequests` metric
   - `shorty_redirects_total{status="5xx"}` -- should increase for cache-miss requests
   - `shorty_redirect_duration_seconds` p99 -- should increase for cache-miss path only
   - `shorty_cache_hits_total{result="hit"}` -- cache-hit requests should be unaffected
   - Lambda logs for `ThrottlingException` and retry counts

4. **Recovery:** Restore table capacity.
   ```bash
   aws dynamodb update-table \
     --table-name shorty-links-staging \
     --provisioned-throughput ReadCapacityUnits=1500,WriteCapacityUnits=800
   ```

### Expected Outcome

| Metric | Baseline | During Throttling | Notes |
|--------|----------|------------------|-------|
| Cache-hit redirect p99 | < 15 ms | < 15 ms | Unaffected |
| Cache-miss redirect p99 | < 70 ms | < 500 ms | SDK retries add latency |
| Overall redirect p99 | < 70 ms | < 100 ms | 85% cache hits mask the issue |
| Error rate (cache miss) | < 0.1% | < 5% | After SDK retry exhaustion |
| Overall error rate | < 0.1% | < 1% | Diluted by cache hits |

### Abort Conditions

- Overall error rate exceeds 5% for more than 2 minutes.
- DynamoDB system errors (not throttling) appear.
- Other tables (`clicks`, `users`) begin throttling due to shared account capacity.

### Rollback

Restore table provisioned capacity or stop the FIS experiment. DynamoDB recovers immediately once capacity is available. No cache flush needed.

### Validation Criteria

- **Pass:** Cache-hit redirects are completely unaffected. Overall service availability stays above 99%. SDK retries handle transient throttling gracefully.
- **Fail:** Cache-hit path is affected (indicates incorrect fallback logic), or Lambda functions crash on `ThrottlingException`.

---

## Experiment 3: Lambda Cold Start Storm

### Hypothesis

When all warm Lambda instances are recycled simultaneously (simulating a deployment or a cold start storm after an idle period), the service experiences a brief latency spike (p99 up to 500 ms) lasting 30-60 seconds. After warm-up, latency returns to baseline. Provisioned concurrency instances are unaffected by the recycle.

### Background

Lambda cold start for the redirect function is 300-400 ms (Go ARM64). This includes AWS SDK initialization, Redis connection establishment, and Secrets Manager fetch. Provisioned concurrency eliminates cold starts for a fixed number of instances. Beyond that, on-demand instances incur cold starts.

### Method

1. **Baseline measurement (5 min):** Run k6 at 1,000 RPS. Record p99 latency and cold start count.

2. **Inject failure:** Force-recycle all non-provisioned Lambda instances by updating a dummy environment variable.
   ```bash
   aws lambda update-function-configuration \
     --function-name shorty-redirect-staging \
     --environment "Variables={CHAOS_EXPERIMENT=$(date +%s)}"
   ```
   This triggers a new version deployment, causing all existing warm instances to be replaced. Provisioned concurrency instances are also recycled but Lambda pre-initializes replacements.

3. **Immediately spike traffic:** Within 5 seconds of the configuration change, increase k6 load to 5,000 RPS to force many simultaneous cold starts.

4. **Observe (5 min):** Monitor:
   - Lambda `Duration` p99 (CloudWatch) -- should spike then return to baseline
   - Lambda `Init Duration` metric -- cold start time per instance
   - Lambda `ConcurrentExecutions` -- should spike as new instances initialize
   - Redirect error rate -- should remain under 1%
   - Provisioned concurrency utilization -- provisioned instances should handle initial requests

5. **Recovery:** Ramp k6 back to 1,000 RPS. Lambda instances warm up naturally.

### Expected Outcome

| Metric | Baseline | First 30s | After 60s |
|--------|----------|-----------|-----------|
| Redirect p99 | < 70 ms | < 500 ms | < 100 ms |
| Error rate | < 0.1% | < 1% | < 0.1% |
| Cold starts | 0/min | 50-100 | 0/min |
| Provisioned utilization | 30% | 100% | 30% |

### Abort Conditions

- Error rate exceeds 5% for more than 1 minute.
- Lambda `Throttles` metric indicates account-level concurrency limit reached.
- Provisioned concurrency instances fail to initialize (configuration error).

### Rollback

No explicit rollback needed. Remove the dummy environment variable if desired:
```bash
aws lambda update-function-configuration \
  --function-name shorty-redirect-staging \
  --environment "Variables={}"
```

### Validation Criteria

- **Pass:** Provisioned concurrency absorbs initial traffic while on-demand instances cold start. Service remains available throughout. Latency recovers to baseline within 60 seconds.
- **Fail:** Provisioned concurrency instances do not serve requests during the transition, or error rate exceeds 5%.

---

## Experiment 4: SQS Consumer Disabled (Click Recording Stops)

### Hypothesis

When the SQS worker Lambda is disabled, click events accumulate in the SQS FIFO queue. Redirect responses are completely unaffected because the SQS publish is asynchronous (fire-and-forget goroutine). When the worker is re-enabled, it drains the backlog without data loss.

### Background

The redirect Lambda publishes click events to SQS in a background goroutine with a 2-second timeout. The SQS publish is non-blocking -- even if SQS is slow, the redirect response is already sent. The worker Lambda processes these events and writes to the `clicks` DynamoDB table.

### Method

1. **Baseline measurement (5 min):** Run k6 at 1,000 RPS. Record redirect p99, SQS queue depth, and click write rate.

2. **Inject failure:** Disable the worker Lambda by setting reserved concurrency to 0.
   ```bash
   aws lambda put-function-concurrency \
     --function-name shorty-worker-staging \
     --reserved-concurrent-executions 0
   ```

3. **Observe (15 min):** Continue k6 at 1,000 RPS. Monitor:
   - Redirect p99 and error rate -- should be identical to baseline
   - SQS `ApproximateNumberOfMessagesVisible` -- should grow linearly
   - SQS `ApproximateAgeOfOldestMessage` -- should increase to experiment duration
   - `clicks` table write throughput -- should drop to 0
   - `links` table `click_count` -- should still increment (synchronous UpdateItem)

4. **Recovery:** Re-enable the worker Lambda.
   ```bash
   aws lambda delete-function-concurrency \
     --function-name shorty-worker-staging
   ```

5. **Post-recovery observation (10 min):** Monitor queue drain rate. Verify all queued messages are processed.

### Expected Outcome

| Metric | Baseline | Worker Disabled (15 min) | Post-Recovery |
|--------|----------|------------------------|---------------|
| Redirect p99 | < 70 ms | < 70 ms | < 70 ms |
| Redirect error rate | < 0.1% | < 0.1% | < 0.1% |
| SQS queue depth | ~0 | ~900,000 (1,000/s x 900s) | Drains to 0 in ~15 min |
| Oldest message age | < 5s | ~900s | Returns to < 5s |
| Click records in DynamoDB | Real-time | Stale (15 min lag) | Catches up |

### Abort Conditions

- Redirect error rate exceeds 0.5% (would indicate SQS publish is somehow blocking).
- SQS returns `QueueFull` errors (FIFO queue limit: 120,000 in-flight messages for high-throughput mode).
- DLQ depth increases (indicates message processing failures, not just backlog).

### Rollback

Re-enable worker Lambda (step 4 above). If SQS queue depth is too high for the worker to drain in reasonable time, increase worker concurrency temporarily:
```bash
aws lambda put-function-concurrency \
  --function-name shorty-worker-staging \
  --reserved-concurrent-executions 500
```

### Validation Criteria

- **Pass:** Redirect service is completely unaffected. SQS queue depth grows linearly and drains completely after worker re-enablement. Zero messages land in DLQ. No data loss.
- **Fail:** Redirect latency or error rate increases, or messages are lost, or DLQ receives messages.

---

## Experiment 5: CloudFront Origin Failure

### Hypothesis

When the API Gateway origin returns errors (simulating a Lambda failure behind CloudFront), CloudFront serves stale cached responses for URLs it has cached. Uncached URLs receive errors. CloudFront custom error pages return a friendly message for uncached error responses.

### Background

CloudFront caches redirect responses (302) with a TTL matching the `Cache-Control: max-age` header (60 seconds). If the origin is unavailable, CloudFront can serve stale cached responses if configured with `stale-if-error`. For uncached URLs, CloudFront passes the error through.

### Method

1. **Warm the cache (5 min):** Run k6 at 500 RPS against 100 known short codes to populate the CloudFront cache.

2. **Inject failure:** Update the CloudFront origin to point to a non-existent API Gateway stage.
   ```bash
   aws cloudfront update-distribution \
     --id DISTRIBUTION_ID \
     --distribution-config file://broken-origin-config.json
   ```
   Alternatively, use a WAF rule to block all origin requests:
   ```bash
   aws wafv2 update-web-acl \
     --scope CLOUDFRONT \
     --id WAF_ACL_ID \
     --default-action '{"Block": {}}' \
     --rules '[]' \
     --lock-token LOCK_TOKEN
   ```

3. **Observe (5 min):**
   - Request cached short codes: should return 302 from CF cache.
   - Request uncached short codes: should return CF custom error page (503).
   - Monitor CF `4xxErrorRate` and `5xxErrorRate` metrics.
   - Monitor CF `CacheHitRate` -- should be 100% for cached codes.

4. **Recovery:** Restore the original origin configuration or WAF default action.

5. **Post-recovery observation (5 min):** Verify all requests return normally. CF cache refreshes on TTL expiry.

### Expected Outcome

| Request Type | During Failure | Notes |
|-------------|---------------|-------|
| Cached short code | 302 redirect (from CF cache) | Served until TTL expires |
| Uncached short code | 503 custom error page | CF cannot reach origin |
| API requests (non-cached) | 503 error | API is not cached |
| Cached code after TTL expiry | 503 (or stale if `stale-if-error`) | Depends on configuration |

### Abort Conditions

- The origin configuration change propagates to production (always verify distribution ID is staging).
- CF cache serves incorrect redirect targets (data integrity issue).

### Rollback

Restore the original CloudFront distribution configuration. Propagation takes 5-15 minutes. During propagation, the broken configuration continues serving. For immediate recovery, use Route 53 failover to a static S3 maintenance page.

### Validation Criteria

- **Pass:** Cached responses continue serving correctly during origin failure. Custom error pages display for uncached requests. Recovery is clean after origin restoration.
- **Fail:** Cached responses serve wrong targets, or CF crashes/hangs, or recovery requires manual cache invalidation.

---

## Experiment 6: Network Partition Between Lambda and Redis

### Hypothesis

When network connectivity between Lambda and Redis degrades (increased latency, packet loss), the redirect Lambda's connection timeout (3 seconds) and read timeout (1 second) cause Redis operations to fail. The rate limiter fails open and the cache falls through to DynamoDB. Overall redirect latency increases by the timeout duration for the first affected request per Lambda instance; subsequent requests detect the broken connection faster.

### Background

This differs from Experiment 1 (Redis fully unavailable) in that connections may partially succeed. Intermittent connectivity is harder to handle than a clean failure because the client may wait for the full timeout on each operation rather than failing immediately.

### Method

1. **Baseline measurement (5 min):** Record redirect p99, Redis operation latency, and error rate.

2. **Inject failure:** Use AWS FIS or a network ACL to add latency and packet loss to the Redis subnet:
   ```bash
   # Option A: FIS network disruption
   aws fis start-experiment \
     --experiment-template-id EXPT-network-latency-XXXXX

   # Option B: NACL modification (add 500ms latency equivalent via deny/allow cycling)
   # This is coarse-grained; FIS is preferred.
   ```

   For local testing, use `tc` (traffic control) on the Docker network:
   ```bash
   docker exec -it shorty-redis tc qdisc add dev eth0 root netem delay 500ms loss 30%
   ```

3. **Observe (10 min):** Monitor:
   - Redis command latency (`shorty.cache.latency_ms`) -- should show bimodal distribution (fast successes + timeout failures)
   - `shorty_redirect_duration_seconds` p99 -- should increase
   - `shorty_cache_hits_total` -- hit ratio should degrade
   - Lambda logs for `i/o timeout` and `connection reset` errors
   - DynamoDB `ConsumedReadCapacityUnits` -- should increase as cache misses rise
   - Rate limiter behavior -- should fail open (allow requests)

4. **Recovery:** Remove the network disruption.
   ```bash
   # FIS: stop experiment
   aws fis stop-experiment --id EXPERIMENT_RUN_ID

   # Docker tc:
   docker exec -it shorty-redis tc qdisc del dev eth0 root
   ```

5. **Post-recovery observation (5 min):** Verify Redis connections recover and cache hit ratio returns to baseline.

### Expected Outcome

| Metric | Baseline | During Partition | Recovery |
|--------|----------|-----------------|----------|
| Redirect p99 | < 70 ms | < 1,500 ms (includes timeout) | < 70 ms within 2 min |
| Error rate | < 0.1% | < 2% | < 0.1% |
| Redis operation success rate | > 99% | 30-70% (depends on packet loss) | > 99% |
| DynamoDB read load | Baseline | 2-3x baseline | Baseline |

### Abort Conditions

- Error rate exceeds 10% for more than 2 minutes (indicates DynamoDB fallback is also failing).
- Lambda instances crash or enter a restart loop.
- Redis connection pool enters a state where it does not recover after the partition heals (connection leak).

### Rollback

Remove network disruption (step 4). If Redis connections do not recover within 2 minutes, recycle Lambda instances:
```bash
aws lambda update-function-configuration \
  --function-name shorty-redirect-staging \
  --description "recycle-connections-$(date +%s)"
```

### Validation Criteria

- **Pass:** go-redis handles intermittent failures gracefully (timeout, retry, fallback). Connections recover automatically after the partition heals. No manual intervention needed.
- **Fail:** Connection pool becomes permanently degraded, or Lambda instances do not recover without redeployment.

---

## Experiment 7: Single AZ Failure

### Hypothesis

When a single Availability Zone becomes unavailable, Lambda automatically schedules new instances in healthy AZs. ElastiCache multi-AZ failover promotes the replica to primary within 30 seconds. DynamoDB is unaffected (multi-AZ by default). Total service disruption is under 30 seconds.

### Method

1. **Baseline measurement (5 min):** Record all SLIs.

2. **Inject failure:** Use AWS FIS to simulate AZ impairment:
   ```bash
   aws fis start-experiment \
     --experiment-template-id EXPT-az-failure-XXXXX
   ```
   The FIS template should target a single AZ and affect:
   - EC2/Lambda networking in that AZ
   - ElastiCache node in that AZ (triggers failover if primary is there)

3. **Observe (15 min):** Monitor:
   - ElastiCache `ReplicationLag` and failover events
   - Lambda `ConcurrentExecutions` (should redistribute to healthy AZs)
   - Redirect p99 and error rate
   - API Gateway error rate

4. **Recovery:** Stop the FIS experiment. Verify all components return to multi-AZ state.

### Expected Outcome

| Phase | Duration | Redirect p99 | Error Rate |
|-------|----------|-------------|-----------|
| Failure detection | 0-10s | Elevated | < 5% |
| Failover in progress | 10-30s | < 500 ms | < 2% |
| Steady state (degraded) | 30s+ | < 100 ms | < 0.1% |
| Full recovery | Post-experiment | < 70 ms | < 0.1% |

### Abort Conditions

- Error rate exceeds 10% for more than 1 minute.
- ElastiCache failover does not complete within 60 seconds.
- DynamoDB becomes unavailable (should never happen -- it is always multi-AZ).
- The experiment affects more than one AZ.

### Rollback

Stop the FIS experiment. AWS services recover automatically. If ElastiCache failover leaves the cluster in a degraded state (single node, no replica), manually add a replica in the recovered AZ:
```bash
aws elasticache create-replication-group-member \
  --replication-group-id shorty-redis-staging \
  --preferred-availability-zones us-east-1b
```

### Validation Criteria

- **Pass:** Automatic failover completes within 30 seconds. Service availability stays above 99% during the event. No manual intervention needed.
- **Fail:** Failover takes more than 60 seconds, or service is completely unavailable during the failover, or manual intervention is required.

---

## Game Day Schedule

Run all experiments in a single game day session (half-day). Order matters -- earlier experiments should not leave residual effects that corrupt later experiments.

| Order | Experiment | Duration | Cumulative Time |
|-------|-----------|----------|----------------|
| 1 | SQS Consumer Disabled (#4) | 35 min | 0:35 |
| 2 | DynamoDB Throttling (#2) | 25 min | 1:00 |
| 3 | Redis Failure (#1) | 25 min | 1:25 |
| 4 | Network Partition (#6) | 25 min | 1:50 |
| 5 | Lambda Cold Start Storm (#3) | 20 min | 2:10 |
| 6 | CloudFront Origin Failure (#5) | 20 min | 2:30 |
| 7 | Single AZ Failure (#7) | 25 min | 2:55 |
| -- | Debrief + documentation | 30 min | 3:25 |

**Total estimated time: 3.5 hours.**

### Pre-Game Day Checklist

- [ ] Staging environment matches production configuration (Terraform same modules, different variables)
- [ ] k6 load test scripts are deployed and tested against staging
- [ ] All monitoring dashboards are accessible and showing staging metrics
- [ ] FIS experiment templates are created and validated (dry-run)
- [ ] On-call engineer is briefed and available
- [ ] Rollback procedures are documented and tested
- [ ] PagerDuty staging alerts are routed to the game day Slack channel (not to phones)
- [ ] Production environment is confirmed healthy before starting (game day should not coincide with production issues)

### Post-Game Day

- [ ] Document results for each experiment (pass/fail, actual vs expected metrics)
- [ ] File GitHub Issues for any findings (label: `chaos-engineering`)
- [ ] Update runbooks based on observed behavior vs documented procedures
- [ ] Schedule follow-up game day for any failed experiments (after fixes are applied)
- [ ] Present findings at the next engineering retrospective
