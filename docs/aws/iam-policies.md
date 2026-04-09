# IAM Policies -- Shorty URL Shortener

Exact IAM policy documents for each Lambda execution role. All ARNs are parameterized. No wildcard actions. No wildcard resources except where AWS requires it (X-Ray, CloudWatch Metrics).

---

## 1. Trust Policy (shared by all Lambda roles)

Every Lambda execution role uses the same trust policy allowing the Lambda service to assume the role.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "lambda.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

---

## 2. Redirect Lambda Role

**Role name:** `shorty-redirect-lambda-role`

The hot path. Read-only on DynamoDB `links`, publishes click events to SQS FIFO, retrieves IP hashing salt from Secrets Manager.

### Permission Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DynamoDBReadLinks",
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem"
      ],
      "Resource": "arn:aws:dynamodb:${region}:${account_id}:table/shorty-links"
    },
    {
      "Sid": "SQSSendClickEvents",
      "Effect": "Allow",
      "Action": [
        "sqs:SendMessage"
      ],
      "Resource": "arn:aws:sqs:${region}:${account_id}:shorty-clicks.fifo"
    },
    {
      "Sid": "SecretsManagerIPSalt",
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue"
      ],
      "Resource": "arn:aws:secretsmanager:${region}:${account_id}:secret:shorty/ip-salt-*"
    },
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "arn:aws:logs:${region}:${account_id}:log-group:/aws/lambda/shorty-redirect:*"
    },
    {
      "Sid": "XRayTracing",
      "Effect": "Allow",
      "Action": [
        "xray:PutTraceSegments",
        "xray:PutTelemetryRecords"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CloudWatchMetrics",
      "Effect": "Allow",
      "Action": [
        "cloudwatch:PutMetricData"
      ],
      "Resource": "*"
    },
    {
      "Sid": "VPCNetworkInterfaces",
      "Effect": "Allow",
      "Action": [
        "ec2:CreateNetworkInterface",
        "ec2:DescribeNetworkInterfaces",
        "ec2:DeleteNetworkInterface"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "ec2:Vpc": "arn:aws:ec2:${region}:${account_id}:vpc/${vpc_id}"
        }
      }
    }
  ]
}
```

**Notes:**
- ElastiCache Redis has no IAM integration. Access is controlled via VPC security groups and Redis AUTH token (stored in Secrets Manager, retrieved via the `SecretsManagerIPSalt` statement pattern).
- `VPCNetworkInterfaces` is required because the Lambda runs inside a VPC to access ElastiCache. The condition restricts ENI creation to the project VPC.
- No `dynamodb:UpdateItem` -- the redirect Lambda is read-only on DynamoDB. Click count increments are handled by the worker Lambda.

---

## 3. API Lambda Role

**Role name:** `shorty-api-lambda-role`

Full CRUD on `links`, user profile management, click stats queries. JWT-protected endpoints.

### Permission Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DynamoDBCRUDLinks",
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:UpdateItem",
        "dynamodb:DeleteItem"
      ],
      "Resource": "arn:aws:dynamodb:${region}:${account_id}:table/shorty-links"
    },
    {
      "Sid": "DynamoDBQueryLinksGSI",
      "Effect": "Allow",
      "Action": [
        "dynamodb:Query"
      ],
      "Resource": "arn:aws:dynamodb:${region}:${account_id}:table/shorty-links/index/owner_id-created_at-index"
    },
    {
      "Sid": "DynamoDBQueryClicksGSI",
      "Effect": "Allow",
      "Action": [
        "dynamodb:Query"
      ],
      "Resource": "arn:aws:dynamodb:${region}:${account_id}:table/shorty-clicks/index/code-date-index"
    },
    {
      "Sid": "DynamoDBUsersTable",
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:UpdateItem"
      ],
      "Resource": "arn:aws:dynamodb:${region}:${account_id}:table/shorty-users"
    },
    {
      "Sid": "SecretsManagerAccess",
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue"
      ],
      "Resource": [
        "arn:aws:secretsmanager:${region}:${account_id}:secret:shorty/ip-salt-*",
        "arn:aws:secretsmanager:${region}:${account_id}:secret:shorty/jwt-*"
      ]
    },
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "arn:aws:logs:${region}:${account_id}:log-group:/aws/lambda/shorty-api:*"
    },
    {
      "Sid": "XRayTracing",
      "Effect": "Allow",
      "Action": [
        "xray:PutTraceSegments",
        "xray:PutTelemetryRecords"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CloudWatchMetrics",
      "Effect": "Allow",
      "Action": [
        "cloudwatch:PutMetricData"
      ],
      "Resource": "*"
    },
    {
      "Sid": "VPCNetworkInterfaces",
      "Effect": "Allow",
      "Action": [
        "ec2:CreateNetworkInterface",
        "ec2:DescribeNetworkInterfaces",
        "ec2:DeleteNetworkInterface"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "ec2:Vpc": "arn:aws:ec2:${region}:${account_id}:vpc/${vpc_id}"
        }
      }
    }
  ]
}
```

**Notes:**
- No SQS access -- the API Lambda does not publish click events.
- No access to the `clicks` base table -- only the GSI `code-date-index` for stats queries.
- No `dynamodb:Scan` on any table.

---

## 4. Worker Lambda Role

**Role name:** `shorty-worker-lambda-role`

SQS FIFO consumer. Batch-writes click events to the `clicks` table and increments click counters on the `links` table.

### Permission Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "SQSConsumeClickEvents",
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes"
      ],
      "Resource": "arn:aws:sqs:${region}:${account_id}:shorty-clicks.fifo"
    },
    {
      "Sid": "DynamoDBBatchWriteClicks",
      "Effect": "Allow",
      "Action": [
        "dynamodb:BatchWriteItem"
      ],
      "Resource": "arn:aws:dynamodb:${region}:${account_id}:table/shorty-clicks"
    },
    {
      "Sid": "DynamoDBUpdateLinkClickCount",
      "Effect": "Allow",
      "Action": [
        "dynamodb:UpdateItem"
      ],
      "Resource": "arn:aws:dynamodb:${region}:${account_id}:table/shorty-links"
    },
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "arn:aws:logs:${region}:${account_id}:log-group:/aws/lambda/shorty-worker:*"
    },
    {
      "Sid": "XRayTracing",
      "Effect": "Allow",
      "Action": [
        "xray:PutTraceSegments",
        "xray:PutTelemetryRecords"
      ],
      "Resource": "*"
    },
    {
      "Sid": "CloudWatchMetrics",
      "Effect": "Allow",
      "Action": [
        "cloudwatch:PutMetricData"
      ],
      "Resource": "*"
    }
  ]
}
```

**Notes:**
- No Redis access -- the worker does not interact with the cache.
- No Secrets Manager access -- the IP hash is already computed by the redirect Lambda and passed in the SQS message payload.
- No `dynamodb:GetItem` on `links` -- the worker only increments `click_count` via `UpdateItem` with a `ConditionalExpression`.
- The worker does not run in VPC (no ElastiCache access needed), so no `ec2:*NetworkInterface*` permissions.

---

## 5. Terraform Implementation

### 5.1 Redirect Lambda Role

```hcl
# deploy/terraform/modules/lambda/iam.tf

data "aws_caller_identity" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  region     = var.region
}

# --- Trust Policy ---

data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

# --- Redirect Lambda ---

resource "aws_iam_role" "redirect_lambda" {
  name               = "shorty-redirect-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Project   = "shorty"
    Component = "redirect"
  }
}

data "aws_iam_policy_document" "redirect_lambda" {
  # DynamoDB: read-only on links table
  statement {
    sid       = "DynamoDBReadLinks"
    effect    = "Allow"
    actions   = ["dynamodb:GetItem"]
    resources = [var.dynamodb_links_table_arn]
  }

  # SQS: send click events
  statement {
    sid       = "SQSSendClickEvents"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [var.sqs_clicks_queue_arn]
  }

  # Secrets Manager: IP salt
  statement {
    sid       = "SecretsManagerIPSalt"
    effect    = "Allow"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = ["arn:aws:secretsmanager:${local.region}:${local.account_id}:secret:shorty/ip-salt-*"]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["arn:aws:logs:${local.region}:${local.account_id}:log-group:/aws/lambda/shorty-redirect:*"]
  }

  # X-Ray
  statement {
    sid    = "XRayTracing"
    effect = "Allow"
    actions = [
      "xray:PutTraceSegments",
      "xray:PutTelemetryRecords",
    ]
    resources = ["*"]
  }

  # CloudWatch Metrics
  statement {
    sid       = "CloudWatchMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
  }

  # VPC ENI management (required for Lambda in VPC)
  statement {
    sid    = "VPCNetworkInterfaces"
    effect = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "redirect_lambda" {
  name   = "shorty-redirect-lambda-policy"
  role   = aws_iam_role.redirect_lambda.id
  policy = data.aws_iam_policy_document.redirect_lambda.json
}
```

### 5.2 API Lambda Role

```hcl
resource "aws_iam_role" "api_lambda" {
  name               = "shorty-api-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Project   = "shorty"
    Component = "api"
  }
}

data "aws_iam_policy_document" "api_lambda" {
  # DynamoDB: full CRUD on links table
  statement {
    sid    = "DynamoDBCRUDLinks"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
    ]
    resources = [var.dynamodb_links_table_arn]
  }

  # DynamoDB: query links GSI (user dashboard)
  statement {
    sid       = "DynamoDBQueryLinksGSI"
    effect    = "Allow"
    actions   = ["dynamodb:Query"]
    resources = ["${var.dynamodb_links_table_arn}/index/owner_id-created_at-index"]
  }

  # DynamoDB: query clicks GSI (stats)
  statement {
    sid       = "DynamoDBQueryClicksGSI"
    effect    = "Allow"
    actions   = ["dynamodb:Query"]
    resources = ["${var.dynamodb_clicks_table_arn}/index/code-date-index"]
  }

  # DynamoDB: users table
  statement {
    sid    = "DynamoDBUsersTable"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
    ]
    resources = [var.dynamodb_users_table_arn]
  }

  # Secrets Manager: IP salt + JWT keys
  statement {
    sid     = "SecretsManagerAccess"
    effect  = "Allow"
    actions = ["secretsmanager:GetSecretValue"]
    resources = [
      "arn:aws:secretsmanager:${local.region}:${local.account_id}:secret:shorty/ip-salt-*",
      "arn:aws:secretsmanager:${local.region}:${local.account_id}:secret:shorty/jwt-*",
    ]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["arn:aws:logs:${local.region}:${local.account_id}:log-group:/aws/lambda/shorty-api:*"]
  }

  # X-Ray
  statement {
    sid    = "XRayTracing"
    effect = "Allow"
    actions = [
      "xray:PutTraceSegments",
      "xray:PutTelemetryRecords",
    ]
    resources = ["*"]
  }

  # CloudWatch Metrics
  statement {
    sid       = "CloudWatchMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
  }

  # VPC ENI management
  statement {
    sid    = "VPCNetworkInterfaces"
    effect = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "api_lambda" {
  name   = "shorty-api-lambda-policy"
  role   = aws_iam_role.api_lambda.id
  policy = data.aws_iam_policy_document.api_lambda.json
}
```

### 5.3 Worker Lambda Role

```hcl
resource "aws_iam_role" "worker_lambda" {
  name               = "shorty-worker-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json

  tags = {
    Project   = "shorty"
    Component = "worker"
  }
}

data "aws_iam_policy_document" "worker_lambda" {
  # SQS: consume click events
  statement {
    sid    = "SQSConsumeClickEvents"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = [var.sqs_clicks_queue_arn]
  }

  # DynamoDB: batch write to clicks table
  statement {
    sid       = "DynamoDBBatchWriteClicks"
    effect    = "Allow"
    actions   = ["dynamodb:BatchWriteItem"]
    resources = [var.dynamodb_clicks_table_arn]
  }

  # DynamoDB: update click_count on links table
  statement {
    sid       = "DynamoDBUpdateLinkClickCount"
    effect    = "Allow"
    actions   = ["dynamodb:UpdateItem"]
    resources = [var.dynamodb_links_table_arn]
  }

  # CloudWatch Logs
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["arn:aws:logs:${local.region}:${local.account_id}:log-group:/aws/lambda/shorty-worker:*"]
  }

  # X-Ray
  statement {
    sid    = "XRayTracing"
    effect = "Allow"
    actions = [
      "xray:PutTraceSegments",
      "xray:PutTelemetryRecords",
    ]
    resources = ["*"]
  }

  # CloudWatch Metrics
  statement {
    sid       = "CloudWatchMetrics"
    effect    = "Allow"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "worker_lambda" {
  name   = "shorty-worker-lambda-policy"
  role   = aws_iam_role.worker_lambda.id
  policy = data.aws_iam_policy_document.worker_lambda.json
}
```

### 5.4 Terraform Variables

```hcl
# deploy/terraform/modules/lambda/variables.tf

variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "dynamodb_links_table_arn" {
  description = "ARN of the shorty-links DynamoDB table"
  type        = string
}

variable "dynamodb_clicks_table_arn" {
  description = "ARN of the shorty-clicks DynamoDB table"
  type        = string
}

variable "dynamodb_users_table_arn" {
  description = "ARN of the shorty-users DynamoDB table"
  type        = string
}

variable "sqs_clicks_queue_arn" {
  description = "ARN of the shorty-clicks.fifo SQS queue"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID for Lambda network interface management"
  type        = string
}
```

---

## 6. Permission Matrix Summary

| Lambda | DynamoDB `links` | DynamoDB `clicks` | DynamoDB `users` | SQS | Secrets Manager | CloudWatch | X-Ray | VPC |
|--------|-----------------|-------------------|-------------------|-----|-----------------|------------|-------|-----|
| **redirect** | GetItem | -- | -- | SendMessage | GetSecretValue (ip-salt) | Logs + Metrics | Yes | Yes |
| **api** | GetItem, PutItem, UpdateItem, DeleteItem | Query (GSI only) | GetItem, PutItem, UpdateItem | -- | GetSecretValue (ip-salt, jwt) | Logs + Metrics | Yes | Yes |
| **worker** | UpdateItem | BatchWriteItem | -- | ReceiveMessage, DeleteMessage, GetQueueAttributes | -- | Logs + Metrics | Yes | No |

---

## 7. Security Audit Checklist

- [ ] No IAM policy uses `Action: "*"` or `Resource: "*"` (except X-Ray and CloudWatch Metrics, which require it)
- [ ] No `dynamodb:Scan` is granted to any Lambda
- [ ] Redirect Lambda has no write access to DynamoDB (read-only)
- [ ] Worker Lambda has no read access to links (only UpdateItem for click_count)
- [ ] API Lambda has no SQS access
- [ ] Worker Lambda has no Secrets Manager access
- [ ] All Secrets Manager ARNs use the `-*` suffix for the random secret suffix
- [ ] VPC ENI permissions are scoped to the project VPC where possible
- [ ] Trust policies only allow `lambda.amazonaws.com` as principal
- [ ] No inline credentials or secrets in Terraform -- all via Secrets Manager or SSM Parameter Store
