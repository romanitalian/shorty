# Load Test Guide

> Procedures for running, interpreting, and reporting k6 load tests for Shorty.
> Test scripts are in `tests/load/`. Results are written to `tests/load/results/`.

---

## 1. Test Scenarios Overview

Shorty has four k6 load test scenarios, each targeting a different failure mode:

| Scenario | Script | Purpose | Duration | Peak RPS | Pass Criteria |
|---|---|---|---|---|---|
| **Baseline** | `tests/load/baseline.js` | Verify SLAs under normal load | ~6 min | 1,000 | p99 redirect < 100 ms, < 0.1% errors |
| **Stress** | `tests/load/stress.js` | Find the breaking point | ~9 min | 10,000 | p99 redirect < 200 ms, < 5% errors |
| **Spike** | `tests/load/spike.js` | Test sudden traffic surge and recovery | ~2.5 min | 5,000 | p99 redirect < 500 ms, < 5% errors, clean recovery |
| **Soak** | `tests/load/soak.js` | Detect resource leaks over time | ~32 min | 500 | p99 redirect < 100 ms, no latency degradation |

### Traffic Mix (All Scenarios)

All scenarios use the same traffic distribution:

| Operation | Weight | Endpoint | Expected Status |
|---|---|---|---|
| Redirect | 80% | `GET /{code}` on redirect service | 302, 404, 410 |
| Create | 15% | `POST /api/v1/shorten` on API service | 201, 429 |
| Stats | 5% | `GET /api/v1/links/{code}/stats` on API service | 200, 401, 403, 404 |

### Custom Metrics

Each scenario emits these custom metrics (in addition to k6 built-in metrics):

| Metric | Type | Description |
|---|---|---|
| `redirect_duration` | Trend | Redirect response time (ms) |
| `redirect_errors` | Rate | Redirect error rate |
| `create_duration` | Trend | Create response time (ms) |
| `create_errors` | Rate | Create error rate |
| `stats_duration` | Trend | Stats response time (ms) |
| `stats_errors` | Rate | Stats error rate |

Stress and spike scenarios add scenario-specific metrics (`breaking_point_reached`, `spike_phase_errors`, `recovery_phase_errors`).

---

## 2. Environment Setup

### Prerequisites

| Tool | Version | Install |
|---|---|---|
| k6 | >= 0.47 | `brew install k6` or [k6.io/docs/get-started](https://k6.io/docs/get-started/) |
| Docker + Docker Compose | >= 24.x | Required for local stack |
| Go | >= 1.22 | Required for building services |

### Local Environment (Default)

Start the full local stack:

```bash
make dev-up       # LocalStack + Redis + Grafana + Jaeger
make run-api      # API service on http://localhost:8080
make run-redirect # Redirect service on http://localhost:8081
```

Verify services are running:

```bash
curl -s http://localhost:8080/api/v1/health | jq .
curl -s http://localhost:8081/health | jq .
```

### Seed Test Data

Load test scripts use pre-seeded short codes (`test001` through `test010`). Seed them before running tests:

```bash
# Create test short codes (adjust BASE_URL if not localhost)
for i in $(seq -w 1 10); do
  curl -s -X POST http://localhost:8080/api/v1/shorten \
    -H "Content-Type: application/json" \
    -d "{\"original_url\": \"https://example.com/target/${i}\", \"custom_alias\": \"test0${i}\"}"
done
```

Verify seeded codes:

```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost:8081/test001
# Expected: 302
```

### Remote Environment (Dev/Staging)

Set environment variables to point at the remote target:

```bash
export BASE_URL="https://api.dev.shorty.io"
export REDIRECT_URL="https://s.dev.shorty.io"
export SHORT_CODES="abc1234,def5678,ghi9012"  # pre-seeded codes in dev
```

---

## 3. Pre-Test Checklist

Run through this checklist before every load test:

- [ ] Target environment is healthy (`/health` endpoints return 200)
- [ ] Test data is seeded (all `SHORT_CODES` return 302)
- [ ] No other load tests are running against the same environment
- [ ] Monitoring dashboards are open (Grafana locally, CloudWatch for AWS)
- [ ] `tests/load/results/` directory exists (created by `.gitkeep`)
- [ ] For AWS targets: confirm current DynamoDB capacity mode and provisioned limits
- [ ] For AWS targets: confirm Lambda concurrency limits are sufficient for test peak
- [ ] For stress/spike tests: alert on-call that synthetic load is expected

---

## 4. Test Execution

### Running Individual Scenarios

```bash
# Baseline (recommended first test for any environment)
k6 run tests/load/baseline.js

# Stress (find breaking point)
k6 run tests/load/stress.js

# Spike (sudden surge + recovery)
k6 run tests/load/spike.js

# Soak (long-running stability)
k6 run tests/load/soak.js

# Soak with custom duration (shorter for CI)
k6 run -e SOAK_DURATION=5m -e SOAK_RPS=200 tests/load/soak.js
```

### Pointing at a Remote Target

```bash
k6 run -e BASE_URL=https://api.dev.shorty.io \
       -e REDIRECT_URL=https://s.dev.shorty.io \
       -e SHORT_CODES=abc1234,def5678 \
       tests/load/baseline.js
```

### Running with Real-Time Output

```bash
# Stream metrics to stdout every 5 seconds
k6 run --out json=tests/load/results/baseline-raw.json tests/load/baseline.js

# Stream to Grafana Cloud k6 (if configured)
k6 run --out cloud tests/load/baseline.js
```

### Recommended Execution Order

For a full performance validation:

1. **Baseline** -- establishes normal-load SLA compliance.
2. **Stress** -- identifies the breaking point above baseline.
3. **Spike** -- verifies recovery after sudden surge.
4. **Soak** -- confirms stability over extended time.

Always run baseline first. If baseline fails, do not proceed to stress/spike/soak -- fix the issue first.

---

## 5. Results Interpretation

### Reading k6 Output

k6 prints a summary table at the end of each run. Key rows:

```
  redirect_duration.............: avg=12.5ms  min=3.2ms  med=10.1ms  max=89.3ms  p(90)=22.1ms  p(95)=35.4ms  p(99)=67.8ms
  redirect_errors...............: 0.02%   2 out of 8000
  http_req_duration.............: avg=15.3ms  min=2.1ms  med=11.2ms  max=234.5ms p(90)=28.3ms  p(95)=45.6ms  p(99)=98.2ms
  http_req_failed...............: 0.05%   5 out of 10000
```

### Threshold Pass/Fail

k6 exits with code 99 if any threshold is breached. Check the summary for lines marked with a cross:

```
  ✓ redirect_duration.............: p(99)<100    p(99)=67.8ms
  ✗ create_duration...............: p(99)<300    p(99)=342.1ms
```

### JSON Summary Files

Each scenario writes a JSON summary to `tests/load/results/`:

| Scenario | Output file |
|---|---|
| Baseline | `tests/load/results/baseline-summary.json` |
| Stress | `tests/load/results/stress-summary.json` |
| Spike | `tests/load/results/spike-summary.json` |
| Soak | `tests/load/results/soak-summary.json` |

### Interpreting Per-Scenario Results

**Baseline:**
- All thresholds must pass. Any failure indicates a regression.
- Compare `redirect_duration` p99 against the 100 ms SLA.
- `redirect_errors` rate must be < 0.1%.

**Stress:**
- Thresholds are relaxed (p99 < 200 ms, < 5% errors).
- Look for the `breaking_point_reached` metric -- the RPS level where 5xx errors or timeouts first appear.
- `total_errors` counter shows the absolute error count.
- Compare against previous stress runs to detect regression.

**Spike:**
- Focus on the `recovery_phase_errors` counter. During the spike phase, some errors are expected. During recovery (post-spike), errors should drop to zero within 30 seconds.
- If `recovery_phase_errors > 0`, the system did not recover cleanly -- investigate Lambda cold starts, Redis connection pool, or DynamoDB throttling.

**Soak:**
- Watch for **latency degradation over time**. The soak script flags any redirect > 500 ms and any create > 1000 ms.
- Check `soak_analysis.warnings` in the JSON summary for automated degradation detection.
- Compare p99 latency at the start (first 5 minutes) vs end (last 5 minutes). An increase > 20% suggests a resource leak.
- Common soak issues: Redis connection pool exhaustion, Lambda memory growth, DynamoDB auto-scaling lag.

---

## 6. Regression Criteria

A performance regression is declared when any of these conditions occur compared to the previous baseline run:

| Metric | Regression threshold |
|---|---|
| Redirect p99 latency | Increase > 20% |
| Redirect p50 latency | Increase > 30% |
| Create p99 latency | Increase > 25% |
| Error rate (any endpoint) | Increase > 2x |
| Stress breaking point RPS | Decrease > 15% |
| Soak max latency | Increase > 50% |

### Regression Response

1. **Block the PR** that introduced the regression.
2. **Compare** the current run's JSON summary with the previous baseline.
3. **Profile** the specific endpoint that regressed using Go pprof or Jaeger traces.
4. **Fix** the regression before merging.

### Baseline Tracking

Maintain a baseline history in version control or a shared spreadsheet:

| Date | Commit | Redirect p50 | Redirect p99 | Create p99 | Error Rate | Stress Breaking Point |
|---|---|---|---|---|---|---|
| YYYY-MM-DD | `abc1234` | X ms | Y ms | Z ms | 0.0X% | N,NNN RPS |

Update after each release or significant infrastructure change.

---

## 7. Common Pitfalls

### 1. Testing Against Cold Infrastructure

**Problem:** First test run after deployment hits Lambda cold starts, empty Redis cache, and DynamoDB with no adaptive capacity history. Results are artificially bad.

**Fix:** Run a 1-minute warm-up at 100 RPS before the actual test. The baseline script's 30-second ramp serves this purpose, but stress/spike tests may need an explicit warm-up phase.

### 2. Insufficient k6 VUs

**Problem:** k6 reports `dropped_iterations` -- the test could not generate enough requests because all VUs were busy waiting for responses.

**Fix:** Increase `maxVUs` in the scenario configuration. The stress test uses `maxVUs: 5000` -- if you see dropped iterations, the target service is too slow and VUs are blocked waiting for responses. This is itself a finding (the system cannot sustain the target RPS).

### 3. Local Network Bottleneck

**Problem:** When testing locally, the k6 machine's network stack becomes the bottleneck before the application does. Symptoms: high `http_req_blocked` metric (time waiting for a TCP connection).

**Fix:** Run k6 from a separate machine or use `k6 cloud` for distributed load generation.

### 4. Short Code Starvation

**Problem:** The 10 pre-seeded short codes all resolve to the same Redis cache partition. Real traffic would hit a wider key distribution.

**Fix:** Seed at least 100 short codes for stress tests. For baseline, 10 codes are sufficient because the test validates latency, not cache distribution.

### 5. Rate Limiter Interference

**Problem:** k6 generates all traffic from one IP (or a few IPs). The rate limiter (200 requests/minute per IP for redirects) kicks in early, causing legitimate 429 responses that inflate the error rate.

**Fix:** The test scripts accept 429 as a valid status code. However, if > 50% of requests are 429s, the test is measuring rate limiter performance, not application performance. Either:
- Increase the rate limit for the test IP range.
- Run k6 with distributed load generation from multiple IPs.
- Temporarily raise the rate limit threshold for the test environment.

### 6. DynamoDB Auto-Scaling Lag

**Problem:** Stress tests ramp from 0 to 10K RPS in 2 minutes. DynamoDB auto-scaling takes 5-15 minutes to react. The first stress run always shows DynamoDB throttling.

**Fix:** Pre-warm DynamoDB capacity before stress tests:
- For provisioned tables: manually set capacity to the expected peak, run the test, then restore auto-scaling.
- For on-demand tables: DynamoDB automatically handles up to 2x previous peak. Run a warm-up test at 50% target RPS first.

### 7. Clock Skew in Distributed Tests

**Problem:** When running k6 from multiple machines against a remote target, result timestamps may not align, making correlation with server-side metrics difficult.

**Fix:** Ensure all k6 machines use NTP. Tag results with the k6 instance ID for correlation.

---

## 8. Reporting Template

Use this template for load test reports. Copy and fill in after each test run.

```markdown
# Load Test Report

**Date:** YYYY-MM-DD
**Environment:** local / dev / staging / prod
**Commit:** {git short SHA}
**Tester:** {name}
**Scenario:** baseline / stress / spike / soak

## Configuration

- Base URL: {url}
- Redirect URL: {url}
- Short codes: {count} seeded codes
- k6 version: {version}
- Test duration: {duration}
- Peak RPS: {rps}

## Results Summary

| Metric | Value | Threshold | Pass/Fail |
|---|---|---|---|
| Redirect p50 | X ms | -- | -- |
| Redirect p99 | X ms | < 100 ms | PASS/FAIL |
| Redirect error rate | X% | < 0.1% | PASS/FAIL |
| Create p99 | X ms | < 300 ms | PASS/FAIL |
| Create error rate | X% | < 1% | PASS/FAIL |
| Stats p99 | X ms | < 500 ms | PASS/FAIL |
| Overall HTTP failure rate | X% | < 1% | PASS/FAIL |

## Observations

- {Notable findings, anomalies, or regressions}
- {Infrastructure behavior: Lambda concurrency, DynamoDB throttling, Redis metrics}
- {Comparison with previous run if available}

## Action Items

- [ ] {Any follow-up work needed}

## Artifacts

- k6 summary: `tests/load/results/{scenario}-summary.json`
- Grafana snapshot: {URL if available}
- Jaeger trace: {URL for a sample slow request if available}
```

---

## 9. CI Integration

### Baseline in CI Pipeline

The baseline test can run as a CI gate on PRs that modify performance-critical code (`cmd/redirect/`, `internal/cache/`, `internal/store/`, `internal/ratelimit/`):

```bash
# CI step: performance gate
make dev-up
make run-api &
make run-redirect &
sleep 10  # wait for services to start

# Seed test data
./tests/load/seed.sh

# Run baseline with shorter duration for CI
k6 run -e SHORT_CODES=$(cat tests/load/ci-codes.txt) tests/load/baseline.js

# k6 exits 99 on threshold breach -> CI fails
```

### Soak in Nightly Pipeline

The soak test runs nightly against the dev environment with reduced parameters:

```bash
k6 run -e SOAK_DURATION=10m -e SOAK_RPS=200 \
       -e BASE_URL=https://api.dev.shorty.io \
       -e REDIRECT_URL=https://s.dev.shorty.io \
       tests/load/soak.js
```

Stress and spike tests are run manually before releases, not in CI.
