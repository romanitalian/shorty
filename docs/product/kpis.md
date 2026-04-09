# Shorty Key Performance Indicators (KPIs)

## Performance Metrics

| Metric | Description | Target | Measurement | Alert Threshold |
|---|---|---|---|---|
| Redirect latency (p50) | Median redirect response time | < 20 ms | `shorty_redirect_duration_seconds` histogram | > 50 ms |
| Redirect latency (p99) | 99th percentile redirect response time | < 100 ms | `shorty_redirect_duration_seconds` histogram | > 200 ms |
| Link creation latency (p99) | 99th percentile create response time | < 300 ms | API handler duration histogram | > 500 ms |
| Lambda cold start duration | Time for Lambda init (outside handler) | < 500 ms | Lambda INIT_START to REPORT | > 1,000 ms |
| Redirect throughput | Sustained redirects per second | 10,000 RPS | `shorty_redirects_total` rate | N/A (capacity target) |
| Cache hit ratio | Redis cache hits / total redirect lookups | > 95% | `shorty_cache_hit_ratio` | < 85% |
| Cache miss latency overhead | Additional latency on cache miss (DynamoDB fallback) | < 30 ms | cache miss span duration | > 50 ms |

## Reliability Metrics

| Metric | Description | Target | Measurement | Alert Threshold |
|---|---|---|---|---|
| Availability | Uptime as percentage of total time | 99.9% (8.7h downtime/yr) | Synthetic checks + error rate | < 99.5% (rolling 30d) |
| Error rate (5xx) | Percentage of 5xx responses | < 0.1% | HTTP 5xx / total requests | > 1% over 5 min |
| Error rate (redirect) | 5xx on redirect path specifically | < 0.01% | Redirect 5xx / redirect total | > 0.1% |
| RTO (Recovery Time Objective) | Time to recover from failure | < 5 min | Incident response metrics | N/A |
| RPO (Recovery Point Objective) | Max acceptable data loss window | < 1 min | DynamoDB replication lag | N/A |
| SQS message processing success | Click events successfully written | > 99.9% | Processed / received messages | < 99% |
| SQS dead letter queue depth | Failed messages in DLQ | 0 (steady state) | DLQ ApproximateNumberOfMessages | > 0 |
| DynamoDB throttle events | Number of throttled read/write requests | 0 | DynamoDB ThrottledRequests | > 0 |

## Business Metrics

| Metric | Description | Target (Month 1) | Target (Month 6) | Measurement |
|---|---|---|---|---|
| DAU (Daily Active Users) | Unique authenticated users per day | 100 | 1,000 | Unique user IDs in API requests |
| Links created / day | Total new short links per day | 500 | 5,000 | `shorty_links_created_total` rate per 24h |
| Redirects / day | Total redirects per day | 5,000 | 100,000 | `shorty_redirects_total` rate per 24h |
| Redirect-to-create ratio | Average redirects per created link | > 10 | > 20 | Redirects / links created (rolling 7d) |
| Guest-to-registered conversion | Anonymous users who register | 5% | 10% | New registrations / unique anonymous IPs |
| Links per user (avg) | Average active links per registered user | 5 | 15 | Total active links / registered users |
| Retention (7-day) | Users who return within 7 days | 30% | 50% | Cohort analysis on login events |
| Retention (30-day) | Users who return within 30 days | 15% | 30% | Cohort analysis on login events |

## Rate Limiting & Abuse Prevention Metrics

| Metric | Description | Target | Measurement | Alert Threshold |
|---|---|---|---|---|
| Rate limit hit rate (redirect) | Percentage of redirects that are rate-limited | < 0.1% | `shorty_rate_limit_hits_total{type="redirect"}` / redirects | > 1% |
| Rate limit hit rate (creation) | Percentage of creations that are rate-limited | < 1% | `shorty_rate_limit_hits_total{type="create"}` / creates | > 5% |
| Blocked IPs (WAF) | Unique IPs blocked by WAF per hour | < 50 | WAF BlockedRequests metric | > 500/hour |
| Password brute-force attempts | Rate-limited password submission events | < 10/day | `shorty_rate_limit_hits_total{type="password"}` | > 100/day |
| Malicious URL blocks | URLs blocked by Safe Browsing API | Tracked (no target) | `shorty_url_blocked_total` | Anomaly detection |
| Rate limit total hits / min | All rate limiter activations per minute | Tracked | `shorty_rate_limit_hits_total` sum rate | > 1,000/min |

## Infrastructure Metrics

| Metric | Description | Target | Measurement | Alert Threshold |
|---|---|---|---|---|
| Lambda concurrent executions | Peak simultaneous Lambda invocations | < 80% of account limit | Lambda ConcurrentExecutions | > 90% of limit |
| Lambda cold start rate | Percentage of invocations that are cold starts | < 5% (redirect) | INIT_START events / total invocations | > 15% |
| Lambda errors | Unhandled Lambda errors | 0 | Lambda Errors metric | > 0 |
| DynamoDB consumed RCU | Read capacity units consumed | < 80% provisioned | DynamoDB ConsumedReadCapacityUnits | > 90% provisioned |
| DynamoDB consumed WCU | Write capacity units consumed | < 80% provisioned | DynamoDB ConsumedWriteCapacityUnits | > 90% provisioned |
| Redis memory usage | ElastiCache memory utilization | < 70% | ElastiCache DatabaseMemoryUsagePercentage | > 85% |
| Redis connection count | Active Redis connections | < 80% max | ElastiCache CurrConnections | > 90% max |
| SQS queue depth | Messages waiting in the click events queue | < 100 | SQS ApproximateNumberOfMessagesVisible | > 1,000 |
| SQS message age | Age of oldest message in queue | < 30 sec | SQS ApproximateAgeOfOldestMessage | > 5 min |
| CloudFront cache hit ratio | Percentage of requests served from edge cache | > 80% (for 301 redirects) | CloudFront CacheHitRate | < 60% |

## Observability Health Metrics

| Metric | Description | Target | Measurement |
|---|---|---|---|
| Trace sampling rate | Percentage of requests with full traces | 100% (local), 10% (prod) | Trace count / request count |
| Log ingestion lag | Delay between event and log availability | < 5 sec (local), < 30 sec (CloudWatch) | Timestamp comparison |
| Dashboard data freshness | Age of newest data point on Grafana dashboards | < 15 sec | Dashboard query timestamps |
| Alert delivery latency | Time from threshold breach to notification | < 2 min | Alert timestamp vs metric timestamp |

---

## Measurement and Reporting

### Data Sources

| Source | Metrics Covered |
|---|---|
| Prometheus (local) / CloudWatch (prod) | All application and infrastructure metrics |
| Grafana dashboards | Visual monitoring; 5 dashboards auto-provisioned |
| OpenTelemetry traces (Jaeger/X-Ray) | Latency breakdown, cache hit/miss, error attribution |
| DynamoDB Streams / queries | Business metrics (links, clicks, users) |
| WAF logs | Abuse prevention metrics |

### Review Cadence

| Frequency | Metrics Reviewed | Audience |
|---|---|---|
| Real-time | Availability, error rate, latency p99, queue depth | On-call SRE |
| Daily | DAU, links created, redirects, rate limit hits | Product + Engineering |
| Weekly | Retention, conversion, cache hit ratio, cold start rate | Product + Engineering + Leadership |
| Monthly | All KPIs, trend analysis, capacity planning | Full team review |

### SLO Error Budget

- **Availability SLO:** 99.9% over 30-day rolling window
- **Error budget:** 43.2 minutes of downtime per 30 days
- **Budget consumed > 50%:** Engineering prioritizes reliability over features
- **Budget consumed > 80%:** Feature freeze until reliability is restored
- **Budget consumed > 100%:** Incident review and post-mortem required
