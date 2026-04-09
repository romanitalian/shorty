# Lambda Configuration

All Shorty Lambda functions run as statically compiled Go binaries on ARM64 (Graviton2) using the `provided.al2023` custom runtime. See [ADR-004](../adr/004-go-arm64-lambda.md) for the rationale.

---

## Common Settings (all Lambdas)

| Setting | Value |
|---------|-------|
| Architecture | `arm64` |
| Runtime | `provided.al2023` |
| Handler | `bootstrap` (Go custom runtime convention) |
| Tracing | X-Ray active tracing enabled |
| Package type | ZIP (single `bootstrap` binary) |
| `GOMAXPROCS` | `1` (Lambda allocates one vCPU per execution environment) |
| SnapStart | **Not available** for `provided.al2023` -- use provisioned concurrency instead |

### Build command

```makefile
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bootstrap cmd/<service>/main.go
zip <service>.zip bootstrap
```

The `-s -w` flags strip debug symbols, reducing binary size by ~30% and improving cold start time.

---

## Lambda: `shorty-redirect`

The hot path. Every millisecond matters. Target: p99 < 100 ms, 10,000 RPS.

### Resource Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Memory | **512 MB** | More memory = more vCPU. Benchmarks show 512 MB is the sweet spot for Go Lambdas -- 256 MB is CPU-constrained under load, 1024 MB shows diminishing returns. At 512 MB Lambda receives ~1/3 vCPU which is sufficient for a cache-first I/O-bound function. |
| Timeout | **5 s** | p99 target is 100 ms; 5 s is the ceiling for hung connections to Redis/DynamoDB. Any invocation exceeding 5 s is a fault, not normal latency. |
| Provisioned concurrency | **2** | Eliminates cold starts for baseline traffic. VPC-attached cold starts add 200-300 ms on top of Go init (50-150 ms), which would violate the 100 ms p99 target. Cost: ~$22/month. |
| Reserved concurrency | **1000** | Main traffic absorber. Prevents runaway scaling (10,000 RPS with ~100 ms avg duration needs ~1,000 concurrent executions). Acts as a cost circuit-breaker. |

### Environment Variables

All sensitive values are stored in SSM Parameter Store (SecureString) or Secrets Manager and fetched at init time -- never as plaintext Lambda environment variables.

| Variable | Source | Description |
|----------|--------|-------------|
| `DYNAMODB_TABLE_LINKS` | Plaintext env | `links` |
| `SQS_QUEUE_URL` | Plaintext env | `https://sqs.{region}.amazonaws.com/{account}/shorty-clicks.fifo` |
| `REDIS_ADDR` | Plaintext env | ElastiCache primary endpoint (`shorty-redis.xxxxx.cache.amazonaws.com:6379`) |
| `REDIS_AUTH_TOKEN` | Secrets Manager | `shorty/redis-auth` -- fetched in `init()` |
| `IP_SALT` | Secrets Manager | `shorty/ip-salt` -- fetched in `init()` |
| `OTEL_SERVICE_NAME` | Plaintext env | `shorty-redirect` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Plaintext env | X-Ray collector endpoint (or Jaeger locally) |
| `GOMAXPROCS` | Plaintext env | `1` |
| `LOG_LEVEL` | Plaintext env | `info` (prod), `debug` (dev) |

### Lambda Function URLs vs API Gateway

The redirect Lambda is integrated via API Gateway v2, not Lambda Function URLs. API Gateway provides throttling, access logging, custom domains, and WAF integration that Function URLs lack. Function URLs would save ~$3.50/million requests but sacrifice observability and protection.

---

## Lambda: `shorty-api`

REST CRUD + stats endpoints. JWT-protected. Lower traffic, higher per-request complexity.

### Resource Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Memory | **256 MB** | API requests are I/O-bound (DynamoDB queries, Redis lookups). 256 MB provides adequate CPU. Stats aggregation with large result sets may benefit from 512 MB -- monitor and adjust. |
| Timeout | **10 s** | Stats queries can scan thousands of click records. DynamoDB Query pagination may require multiple round trips. 10 s accommodates this while staying well under the API Gateway 29 s hard limit. |
| Provisioned concurrency | **0** | API traffic is low-volume and latency-tolerant (p99 < 300 ms target for creation). Go cold starts (50-150 ms + 200-300 ms VPC) are acceptable. |
| Reserved concurrency | **200** | Prevents API traffic from consuming Lambda concurrency needed by the redirect function. |

### Environment Variables

| Variable | Source | Description |
|----------|--------|-------------|
| `DYNAMODB_TABLE_LINKS` | Plaintext env | `links` |
| `DYNAMODB_TABLE_CLICKS` | Plaintext env | `clicks` |
| `DYNAMODB_TABLE_USERS` | Plaintext env | `users` |
| `REDIS_ADDR` | Plaintext env | ElastiCache primary endpoint |
| `REDIS_AUTH_TOKEN` | Secrets Manager | `shorty/redis-auth` |
| `IP_SALT` | Secrets Manager | `shorty/ip-salt` |
| `COGNITO_USER_POOL_ID` | Plaintext env | Cognito User Pool ID for JWT validation |
| `COGNITO_CLIENT_ID` | Plaintext env | Cognito App Client ID |
| `OTEL_SERVICE_NAME` | Plaintext env | `shorty-api` |
| `GOMAXPROCS` | Plaintext env | `1` |
| `LOG_LEVEL` | Plaintext env | `info` |

---

## Lambda: `shorty-worker`

SQS FIFO consumer. Batch-processes click events. Latency is not user-facing.

### Resource Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Memory | **128 MB** | Minimal compute needed. The worker deserializes SQS messages and calls DynamoDB BatchWriteItem. No Redis, no crypto. 128 MB is the cheapest option and sufficient. |
| Timeout | **60 s** | SQS batch processing: up to 10 messages per batch, each requiring a DynamoDB BatchWriteItem (up to 25 items) and an UpdateItem on the links table. 60 s provides headroom for DynamoDB retries with exponential backoff. |
| Provisioned concurrency | **0** | Not latency-sensitive. Cold starts are acceptable -- click event processing can tolerate seconds of delay. |
| Reserved concurrency | **50** | Limits parallel SQS consumers. At 50 concurrent executions processing 10 messages each, throughput is 500 events/batch = ~5,000 events/s, sufficient for 10K RPS redirect traffic (not all redirects generate click events if rate-limited or cached). |

### SQS Event Source Mapping

| Setting | Value | Rationale |
|---------|-------|-----------|
| Batch size | **10** | Maximum for FIFO queues |
| Batch window | **5 s** | Wait up to 5 s to accumulate a full batch before invoking Lambda, reducing invocations at low traffic |
| Maximum concurrency | **50** | Matches reserved concurrency |
| Bisect batch on error | **true** | If a batch fails, split it in half and retry -- isolates poison messages |
| Maximum retry attempts | **3** | After 3 failures, send to DLQ |
| Report batch item failures | **true** | Partial batch failure reporting -- only retry failed items |

### Dead Letter Queue

| Setting | Value |
|---------|-------|
| DLQ name | `shorty-clicks-dlq.fifo` |
| Message retention | 14 days |
| Alarm | CloudWatch alarm on `ApproximateNumberOfMessagesVisible > 0` triggers SNS notification |

Failed click events land in the DLQ. These are analytics events, not user-facing, so data loss is tolerable but should be investigated. A separate Lambda or manual process can replay DLQ messages.

### Environment Variables

| Variable | Source | Description |
|----------|--------|-------------|
| `DYNAMODB_TABLE_CLICKS` | Plaintext env | `clicks` |
| `DYNAMODB_TABLE_LINKS` | Plaintext env | `links` |
| `OTEL_SERVICE_NAME` | Plaintext env | `shorty-worker` |
| `GOMAXPROCS` | Plaintext env | `1` |
| `LOG_LEVEL` | Plaintext env | `info` |

---

## VPC Configuration

All three Lambdas must run inside the VPC to access ElastiCache Redis. (The worker Lambda does not use Redis but is placed in the VPC for simplicity and to use VPC endpoints for DynamoDB/SQS.)

### Network Layout

```
VPC: 10.0.0.0/16

Private Subnets (Lambda + ElastiCache):
  10.0.1.0/24  (AZ-a)
  10.0.2.0/24  (AZ-b)
  10.0.3.0/24  (AZ-c)

No public subnets needed for Lambda.

NAT Gateway:
  Single NAT GW in AZ-a for dev/staging.
  One NAT GW per AZ for production (HA).
  Required for: Cognito JWKS endpoint, external API calls.
  NOT required for: DynamoDB, SQS, Secrets Manager, X-Ray (use VPC endpoints).
```

### VPC Endpoints (cost optimization -- avoid NAT Gateway data processing charges)

| Endpoint | Type | Cost | Justification |
|----------|------|------|---------------|
| `com.amazonaws.{region}.dynamodb` | Gateway | Free | High-volume DynamoDB traffic (10K+ RPS). Gateway endpoints have no hourly or data charges. |
| `com.amazonaws.{region}.sqs` | Interface | ~$7.20/mo per AZ | SQS SendMessage from redirect Lambda. Interface endpoints cost $0.01/hr per AZ + $0.01/GB data. |
| `com.amazonaws.{region}.secretsmanager` | Interface | ~$7.20/mo per AZ | Secrets fetched once at cold start. Low volume but avoids NAT dependency for init. |
| `com.amazonaws.{region}.xray` | Interface | ~$7.20/mo per AZ | Trace data submission. Medium volume. |
| `com.amazonaws.{region}.logs` | Interface | ~$7.20/mo per AZ | CloudWatch log ingestion. High volume. |

Total VPC endpoint cost (3 AZs, 4 interface endpoints): ~$86.40/month. This saves significantly vs routing all AWS API traffic through NAT Gateway at $0.045/GB.

### Security Groups

```
lambda-sg:
  Ingress: none (Lambda initiates all connections)
  Egress:
    - TCP 6379 → elasticache-sg (Redis)
    - TCP 443  → VPC endpoint security group (AWS APIs)
    - TCP 443  → 0.0.0.0/0 via NAT (Cognito JWKS, external URLs)

elasticache-sg:
  Ingress:
    - TCP 6379 ← lambda-sg ONLY
  Egress: none

vpce-sg (VPC endpoint interfaces):
  Ingress:
    - TCP 443 ← lambda-sg
  Egress: none
```

### Terraform Example

```hcl
resource "aws_lambda_function" "redirect" {
  function_name = "shorty-redirect"
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  memory_size   = 512
  timeout       = 5

  filename         = "${path.module}/artifacts/redirect.zip"
  source_code_hash = filebase64sha256("${path.module}/artifacts/redirect.zip")

  role = aws_iam_role.redirect_lambda.arn

  vpc_config {
    subnet_ids         = var.private_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  tracing_config {
    mode = "Active"
  }

  environment {
    variables = {
      DYNAMODB_TABLE_LINKS = aws_dynamodb_table.links.name
      SQS_QUEUE_URL        = aws_sqs_queue.clicks.url
      REDIS_ADDR           = aws_elasticache_replication_group.redis.primary_endpoint_address
      OTEL_SERVICE_NAME    = "shorty-redirect"
      GOMAXPROCS           = "1"
      LOG_LEVEL            = var.log_level
    }
  }

  depends_on = [aws_cloudwatch_log_group.redirect]
}

resource "aws_lambda_provisioned_concurrency_config" "redirect" {
  function_name                  = aws_lambda_function.redirect.function_name
  provisioned_concurrent_executions = 2
  qualifier                      = aws_lambda_alias.redirect_live.name
}

resource "aws_lambda_function_event_invoke_config" "redirect" {
  function_name = aws_lambda_function.redirect.function_name
  maximum_retry_attempts = 0  # redirect is synchronous -- no retries
}
```

---

## Cold Start Analysis

### Breakdown by Phase

| Phase | Duration | Notes |
|-------|----------|-------|
| Lambda service init | ~10 ms | AWS internal: download zip, create sandbox |
| VPC ENI attachment | 200-300 ms | Hyper-Plane ENI (improved from legacy 10+ s). First-time only per execution environment. |
| Go binary init (`init()`) | 50-100 ms | AWS SDK client creation, Redis connection, OTel setup |
| **Total cold start** | **260-410 ms** | Within the 500 ms target |

### Mitigation Strategies

1. **Provisioned concurrency (redirect: 2)** -- eliminates cold starts for baseline traffic entirely. These environments are always warm with ENI attached and `init()` completed.

2. **Init outside handler** -- all AWS SDK clients, Redis pool, and OTel tracer are initialized in `init()` / package-level `var` blocks. They persist across warm invocations (Lambda execution environment reuse).

3. **Minimal binary size** -- `CGO_ENABLED=0` + `-ldflags="-s -w"` produces a ~10 MB binary. Smaller binaries decompress faster during cold start.

4. **Lazy initialization for non-critical paths** -- Secrets Manager values are fetched in `init()` but cached in memory. The OTel tracer flush goroutine starts lazily.

5. **Connection pooling** -- Redis connection pool (`PoolSize: 5`) is established at init time. DynamoDB uses the AWS SDK's built-in HTTP connection pool.

### Cold Start Monitoring

Set up CloudWatch alarms on the `InitDuration` metric:

```hcl
resource "aws_cloudwatch_metric_alarm" "redirect_cold_start" {
  alarm_name          = "shorty-redirect-cold-start-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "InitDuration"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Maximum"
  threshold           = 500
  alarm_description   = "Redirect Lambda cold start exceeded 500ms target"
  dimensions = {
    FunctionName = aws_lambda_function.redirect.function_name
  }
  alarm_actions = [aws_sns_topic.alerts.arn]
}
```

---

## Lambda Aliases and Deployment

Use Lambda aliases for traffic shifting (canary deployments):

| Alias | Purpose |
|-------|---------|
| `live` | Production traffic. Provisioned concurrency is attached to this alias. |
| `canary` | Receives 5% of traffic during deployments via weighted alias routing. |

Provisioned concurrency must be attached to an **alias**, not `$LATEST`. This is a common misconfiguration -- `$LATEST` cannot have provisioned concurrency.

```hcl
resource "aws_lambda_alias" "redirect_live" {
  name             = "live"
  function_name    = aws_lambda_function.redirect.function_name
  function_version = aws_lambda_function.redirect.version

  routing_config {
    additional_version_weights = {
      # Set to new version weight (e.g., 0.05) during canary deployments
    }
  }
}
```

---

## Layers

No Lambda layers are used. Go compiles to a single static binary that includes all dependencies. Layers add cold start latency (extraction time) and complicate deployment. The only potential use case would be a shared X-Ray daemon extension, but the AWS X-Ray SDK for Go sends traces via the API (no daemon needed).

---

## Concurrency Budget

AWS account default Lambda concurrency: 1,000 (request increase to 3,000+ for production).

| Lambda | Reserved | Provisioned | Remaining for on-demand |
|--------|----------|-------------|------------------------|
| redirect | 1,000 | 2 | 998 |
| api | 200 | 0 | 200 |
| worker | 50 | 0 | 50 |
| **Total reserved** | **1,250** | | |
| **Unreserved pool** | **1,750** | | Shared across all other account Lambdas |

Request a concurrency limit increase to **3,000** before production launch. The redirect Lambda alone may need 1,000 concurrent executions at 10K RPS.
