# IAM Least-Privilege Matrix

Each Lambda function has a dedicated IAM execution role with only the permissions it needs. Resource ARNs are parameterized with `${AWS::AccountId}` and `${AWS::Region}`.

---

## Lambda: `redirect`

The hot path. Reads links, caches in Redis, publishes click events to SQS.

| Service | Actions | Resource ARN | Justification |
|---------|---------|-------------|---------------|
| DynamoDB | `dynamodb:GetItem` | `arn:aws:dynamodb:${Region}:${AccountId}:table/links` | Look up link record on cache miss |
| ElastiCache Redis | `GET`, `SET`, `EXPIRE`, `ZADD`, `ZRANGEBYSCORE`, `ZREMRANGEBYSCORE`, `ZCARD` | VPC network access (no IAM -- Redis AUTH token via Secrets Manager) | Cache reads/writes + rate limiter |
| SQS | `sqs:SendMessage` | `arn:aws:sqs:${Region}:${AccountId}:shorty-clicks.fifo` | Publish click events asynchronously |
| Secrets Manager | `secretsmanager:GetSecretValue` | `arn:aws:secretsmanager:${Region}:${AccountId}:secret:shorty/ip-salt-*` | Retrieve IP hashing salt |
| CloudWatch Logs | `logs:CreateLogGroup`, `logs:CreateLogStream`, `logs:PutLogEvents` | `arn:aws:logs:${Region}:${AccountId}:log-group:/aws/lambda/shorty-redirect:*` | Lambda execution logs |
| X-Ray | `xray:PutTraceSegments`, `xray:PutTelemetryRecords` | `*` | Distributed tracing |
| CloudWatch | `cloudwatch:PutMetricData` | `*` | Custom metrics (cache hit ratio, latency) |

**Not granted:** DynamoDB PutItem, UpdateItem, DeleteItem, Query. The redirect Lambda is read-only on DynamoDB.

---

## Lambda: `api`

Full CRUD on links, user profile, quota management. JWT-protected.

| Service | Actions | Resource ARN | Justification |
|---------|---------|-------------|---------------|
| DynamoDB | `dynamodb:GetItem`, `dynamodb:PutItem`, `dynamodb:UpdateItem`, `dynamodb:DeleteItem` | `arn:aws:dynamodb:${Region}:${AccountId}:table/links` | Full CRUD on link records |
| DynamoDB | `dynamodb:Query` | `arn:aws:dynamodb:${Region}:${AccountId}:table/links/index/owner_id-created_at-index` | List user's links (dashboard) |
| DynamoDB | `dynamodb:Query` | `arn:aws:dynamodb:${Region}:${AccountId}:table/clicks/index/code-date-index` | Read click data for stats endpoints |
| DynamoDB | `dynamodb:GetItem`, `dynamodb:PutItem`, `dynamodb:UpdateItem` | `arn:aws:dynamodb:${Region}:${AccountId}:table/users` | User profile CRUD + quota counters |
| ElastiCache Redis | `GET`, `SET`, `DEL`, `EXPIRE`, `ZADD`, `ZRANGEBYSCORE`, `ZREMRANGEBYSCORE`, `ZCARD` | VPC network access (Redis AUTH) | Cache invalidation on link update/delete + rate limiter |
| Secrets Manager | `secretsmanager:GetSecretValue` | `arn:aws:secretsmanager:${Region}:${AccountId}:secret:shorty/ip-salt-*` | IP hashing salt for anonymous link creation |
| Secrets Manager | `secretsmanager:GetSecretValue` | `arn:aws:secretsmanager:${Region}:${AccountId}:secret:shorty/jwt-*` | JWT validation keys (if not using Cognito JWKS directly) |
| CloudWatch Logs | `logs:CreateLogGroup`, `logs:CreateLogStream`, `logs:PutLogEvents` | `arn:aws:logs:${Region}:${AccountId}:log-group:/aws/lambda/shorty-api:*` | Lambda execution logs |
| X-Ray | `xray:PutTraceSegments`, `xray:PutTelemetryRecords` | `*` | Distributed tracing |
| CloudWatch | `cloudwatch:PutMetricData` | `*` | Custom metrics |

**Not granted:** SQS (API Lambda does not publish click events), S3, DynamoDB access to `clicks` table base table (only GSI query).

---

## Lambda: `worker`

SQS FIFO consumer. Batch-writes click events and increments click counters.

| Service | Actions | Resource ARN | Justification |
|---------|---------|-------------|---------------|
| SQS | `sqs:ReceiveMessage`, `sqs:DeleteMessage`, `sqs:GetQueueAttributes` | `arn:aws:sqs:${Region}:${AccountId}:shorty-clicks.fifo` | Consume click events from FIFO queue |
| DynamoDB | `dynamodb:BatchWriteItem` | `arn:aws:dynamodb:${Region}:${AccountId}:table/clicks` | Write click event records in batches |
| DynamoDB | `dynamodb:UpdateItem` | `arn:aws:dynamodb:${Region}:${AccountId}:table/links` | Increment `click_count` with ConditionalExpression |
| CloudWatch Logs | `logs:CreateLogGroup`, `logs:CreateLogStream`, `logs:PutLogEvents` | `arn:aws:logs:${Region}:${AccountId}:log-group:/aws/lambda/shorty-worker:*` | Lambda execution logs |
| X-Ray | `xray:PutTraceSegments`, `xray:PutTelemetryRecords` | `*` | Distributed tracing |
| CloudWatch | `cloudwatch:PutMetricData` | `*` | Custom metrics (batch size, processing latency) |

**Not granted:** Redis (worker does not cache), Secrets Manager (no IP hashing -- salt is embedded in the SQS message payload as `ip_hash`), S3, DynamoDB GetItem/Query on links, any access to `users` table.

---

## Summary Matrix

| Lambda | DynamoDB `links` | DynamoDB `clicks` | DynamoDB `users` | Redis | SQS | Secrets Manager | CloudWatch/X-Ray |
|--------|-----------------|-------------------|-------------------|-------|-----|-----------------|------------------|
| **redirect** | GetItem | -- | -- | GET/SET/rate-limit | SendMessage | GetSecretValue (ip-salt) | Logs + Traces + Metrics |
| **api** | GetItem, PutItem, UpdateItem, DeleteItem, Query (GSI) | Query (GSI) | GetItem, PutItem, UpdateItem | GET/SET/DEL/rate-limit | -- | GetSecretValue (ip-salt, jwt) | Logs + Traces + Metrics |
| **worker** | UpdateItem | BatchWriteItem | -- | -- | ReceiveMessage, DeleteMessage | -- | Logs + Traces + Metrics |

---

## Terraform IAM Policy Template

Each Lambda role is defined in `deploy/terraform/modules/lambda/iam.tf`. Example for the redirect Lambda:

```hcl
data "aws_iam_policy_document" "redirect_lambda" {
  # DynamoDB: read-only on links table
  statement {
    effect    = "Allow"
    actions   = ["dynamodb:GetItem"]
    resources = [aws_dynamodb_table.links.arn]
  }

  # SQS: send click events
  statement {
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.clicks.arn]
  }

  # Secrets Manager: IP salt
  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = ["arn:aws:secretsmanager:${var.region}:${data.aws_caller_identity.current.account_id}:secret:shorty/ip-salt-*"]
  }

  # CloudWatch Logs
  statement {
    effect    = "Allow"
    actions   = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents"
    ]
    resources = ["arn:aws:logs:${var.region}:${data.aws_caller_identity.current.account_id}:log-group:/aws/lambda/shorty-redirect:*"]
  }

  # X-Ray
  statement {
    effect    = "Allow"
    actions   = [
      "xray:PutTraceSegments",
      "xray:PutTelemetryRecords"
    ]
    resources = ["*"]
  }

  # CloudWatch Metrics
  statement {
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
  }
}
```

## Security Notes

1. **Redis has no IAM integration** -- access is controlled via VPC security groups (only Lambda subnets can reach the ElastiCache endpoint) and a Redis AUTH token stored in Secrets Manager.
2. **Wildcard resources** for X-Ray and CloudWatch Metrics are unavoidable -- these services do not support resource-level permissions.
3. **Secrets Manager ARNs use the `-*` suffix** to account for the random 6-character suffix DynamoDB appends to secret names.
4. **No `dynamodb:Scan`** is granted to any Lambda. All access patterns use GetItem, Query, or BatchWriteItem.
5. **The worker Lambda cannot read links** (no GetItem on `links`). It only increments `click_count` via UpdateItem with a condition expression.
