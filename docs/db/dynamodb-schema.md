# DynamoDB Schema Validation

Exact CreateTable parameters for all three tables in both JSON (AWS CLI) and Terraform formats. Validated against the data model in `docs/architecture/data-model.md`.

---

## Table: `links`

### CreateTable (JSON)

```json
{
  "TableName": "shorty-links",
  "AttributeDefinitions": [
    { "AttributeName": "PK", "AttributeType": "S" },
    { "AttributeName": "SK", "AttributeType": "S" },
    { "AttributeName": "owner_id", "AttributeType": "S" },
    { "AttributeName": "created_at", "AttributeType": "N" }
  ],
  "KeySchema": [
    { "AttributeName": "PK", "KeyType": "HASH" },
    { "AttributeName": "SK", "KeyType": "RANGE" }
  ],
  "GlobalSecondaryIndexes": [
    {
      "IndexName": "owner_id-created_at-index",
      "KeySchema": [
        { "AttributeName": "owner_id", "KeyType": "HASH" },
        { "AttributeName": "created_at", "KeyType": "RANGE" }
      ],
      "Projection": { "ProjectionType": "ALL" }
    }
  ],
  "BillingMode": "PAY_PER_REQUEST",
  "SSESpecification": {
    "Enabled": true,
    "SSEType": "KMS"
  },
  "PointInTimeRecoverySpecification": {
    "PointInTimeRecoveryEnabled": true
  },
  "TimeToLiveSpecification": {
    "AttributeName": "expires_at",
    "Enabled": true
  },
  "Tags": [
    { "Key": "Project", "Value": "shorty" },
    { "Key": "Table", "Value": "links" },
    { "Key": "ManagedBy", "Value": "terraform" }
  ]
}
```

### Terraform

```hcl
resource "aws_dynamodb_table" "links" {
  name         = "${var.project}-links"
  billing_mode = var.environment == "prod" ? "PROVISIONED" : "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  # Key attributes
  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  # GSI attributes
  attribute {
    name = "owner_id"
    type = "S"
  }

  attribute {
    name = "created_at"
    type = "N"
  }

  # GSI: user dashboard list
  global_secondary_index {
    name            = "owner_id-created_at-index"
    hash_key        = "owner_id"
    range_key       = "created_at"
    projection_type = "ALL"

    # Only for PROVISIONED mode
    read_capacity  = var.environment == "prod" ? var.links_gsi_read_capacity : null
    write_capacity = var.environment == "prod" ? var.links_gsi_write_capacity : null
  }

  # TTL on expires_at (per-link expiration)
  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  # Encryption at rest with AWS-managed KMS key
  server_side_encryption {
    enabled     = true
    kms_key_arn = null  # AWS-managed key (aws/dynamodb)
  }

  # Point-in-time recovery
  point_in_time_recovery {
    enabled = true
  }

  # Provisioned capacity (prod only)
  dynamic "read_capacity" {
    for_each = var.environment == "prod" ? [1] : []
    content {
      # Set via auto-scaling, see appautoscaling below
    }
  }

  tags = {
    Project   = var.project
    Table     = "links"
    ManagedBy = "terraform"
  }
}

# Auto-scaling for prod (links table)
resource "aws_appautoscaling_target" "links_read" {
  count              = var.environment == "prod" ? 1 : 0
  max_capacity       = 2500
  min_capacity       = 500
  resource_id        = "table/${aws_dynamodb_table.links.name}"
  scalable_dimension = "dynamodb:table:ReadCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "links_read" {
  count              = var.environment == "prod" ? 1 : 0
  name               = "${var.project}-links-read-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.links_read[0].resource_id
  scalable_dimension = aws_appautoscaling_target.links_read[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.links_read[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_target" "links_write" {
  count              = var.environment == "prod" ? 1 : 0
  max_capacity       = 1200
  min_capacity       = 200
  resource_id        = "table/${aws_dynamodb_table.links.name}"
  scalable_dimension = "dynamodb:table:WriteCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "links_write" {
  count              = var.environment == "prod" ? 1 : 0
  name               = "${var.project}-links-write-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.links_write[0].resource_id
  scalable_dimension = aws_appautoscaling_target.links_write[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.links_write[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}
```

---

## Table: `clicks`

### CreateTable (JSON)

```json
{
  "TableName": "shorty-clicks",
  "AttributeDefinitions": [
    { "AttributeName": "PK", "AttributeType": "S" },
    { "AttributeName": "SK", "AttributeType": "S" },
    { "AttributeName": "created_at", "AttributeType": "N" }
  ],
  "KeySchema": [
    { "AttributeName": "PK", "KeyType": "HASH" },
    { "AttributeName": "SK", "KeyType": "RANGE" }
  ],
  "GlobalSecondaryIndexes": [
    {
      "IndexName": "code-date-index",
      "KeySchema": [
        { "AttributeName": "PK", "KeyType": "HASH" },
        { "AttributeName": "created_at", "KeyType": "RANGE" }
      ],
      "Projection": {
        "ProjectionType": "INCLUDE",
        "NonKeyAttributes": ["country", "device_type", "referer_domain", "ip_hash"]
      }
    }
  ],
  "BillingMode": "PROVISIONED",
  "ProvisionedThroughput": {
    "ReadCapacityUnits": 100,
    "WriteCapacityUnits": 10000
  },
  "SSESpecification": {
    "Enabled": true,
    "SSEType": "KMS"
  },
  "PointInTimeRecoverySpecification": {
    "PointInTimeRecoveryEnabled": true
  },
  "TimeToLiveSpecification": {
    "AttributeName": "created_at",
    "Enabled": true
  },
  "StreamSpecification": {
    "StreamEnabled": true,
    "StreamViewType": "NEW_IMAGE"
  },
  "Tags": [
    { "Key": "Project", "Value": "shorty" },
    { "Key": "Table", "Value": "clicks" },
    { "Key": "ManagedBy", "Value": "terraform" }
  ]
}
```

### Terraform

```hcl
resource "aws_dynamodb_table" "clicks" {
  name         = "${var.project}-clicks"
  billing_mode = "PROVISIONED"
  hash_key     = "PK"
  range_key    = "SK"

  read_capacity  = var.clicks_read_capacity   # default: 100
  write_capacity = var.clicks_write_capacity  # default: 10000

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  attribute {
    name = "created_at"
    type = "N"
  }

  # GSI: stats queries by date range
  global_secondary_index {
    name            = "code-date-index"
    hash_key        = "PK"
    range_key       = "created_at"
    projection_type = "INCLUDE"
    non_key_attributes = [
      "country",
      "device_type",
      "referer_domain",
      "ip_hash"
    ]

    read_capacity  = var.clicks_gsi_read_capacity   # default: 100
    write_capacity = var.clicks_gsi_write_capacity  # default: 10000
  }

  # TTL on created_at (90-day retention)
  # Application sets created_at = now() + 90 days for TTL, or uses
  # a separate `ttl` attribute. See note below.
  ttl {
    attribute_name = "created_at"
    enabled        = true
  }

  # Encryption at rest
  server_side_encryption {
    enabled = true
  }

  # Point-in-time recovery
  point_in_time_recovery {
    enabled = true
  }

  # DynamoDB Streams for analytics pipeline (future use)
  stream_enabled   = true
  stream_view_type = "NEW_IMAGE"

  tags = {
    Project   = var.project
    Table     = "clicks"
    ManagedBy = "terraform"
  }
}

# Auto-scaling for clicks (write-heavy)
resource "aws_appautoscaling_target" "clicks_write" {
  max_capacity       = 25000
  min_capacity       = 5000
  resource_id        = "table/${aws_dynamodb_table.clicks.name}"
  scalable_dimension = "dynamodb:table:WriteCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "clicks_write" {
  name               = "${var.project}-clicks-write-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.clicks_write.resource_id
  scalable_dimension = aws_appautoscaling_target.clicks_write.scalable_dimension
  service_namespace  = aws_appautoscaling_target.clicks_write.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}

resource "aws_appautoscaling_target" "clicks_read" {
  max_capacity       = 500
  min_capacity       = 50
  resource_id        = "table/${aws_dynamodb_table.clicks.name}"
  scalable_dimension = "dynamodb:table:ReadCapacityUnits"
  service_namespace  = "dynamodb"
}

resource "aws_appautoscaling_policy" "clicks_read" {
  name               = "${var.project}-clicks-read-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.clicks_read.resource_id
  scalable_dimension = aws_appautoscaling_target.clicks_read.scalable_dimension
  service_namespace  = aws_appautoscaling_target.clicks_read.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}
```

### TTL Note for `clicks` Table

The `created_at` attribute serves double duty: sort key for the GSI and TTL attribute. The application must set `created_at` to `now() + 90 days` (in Unix seconds) for DynamoDB TTL to delete items after 90 days. **Alternative**: add a dedicated `ttl` attribute set to `now() + 90*86400` and use that for TTL instead, keeping `created_at` as the actual event timestamp. The dedicated `ttl` attribute approach is cleaner:

```
created_at = 1712300000          (actual event time)
ttl        = 1712300000 + 7776000 (event time + 90 days)
```

**Recommendation**: Use a separate `ttl` attribute. Update the data model to add `ttl (N)` to the clicks table. The GSI sort key remains `created_at` (actual event time).

---

## Table: `users`

### CreateTable (JSON)

```json
{
  "TableName": "shorty-users",
  "AttributeDefinitions": [
    { "AttributeName": "PK", "AttributeType": "S" },
    { "AttributeName": "SK", "AttributeType": "S" }
  ],
  "KeySchema": [
    { "AttributeName": "PK", "KeyType": "HASH" },
    { "AttributeName": "SK", "KeyType": "RANGE" }
  ],
  "BillingMode": "PAY_PER_REQUEST",
  "SSESpecification": {
    "Enabled": true,
    "SSEType": "KMS"
  },
  "PointInTimeRecoverySpecification": {
    "PointInTimeRecoveryEnabled": true
  },
  "Tags": [
    { "Key": "Project", "Value": "shorty" },
    { "Key": "Table", "Value": "users" },
    { "Key": "ManagedBy", "Value": "terraform" }
  ]
}
```

### Terraform

```hcl
resource "aws_dynamodb_table" "users" {
  name         = "${var.project}-users"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  # No GSI needed for MVP
  # No TTL -- user records persist indefinitely

  server_side_encryption {
    enabled = true
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Project   = var.project
    Table     = "users"
    ManagedBy = "terraform"
  }
}
```

---

## Schema Summary

| Table | PK | SK | GSI | TTL Attribute | Billing | PITR | SSE | Streams |
|-------|-----|-----|-----|---------------|---------|------|-----|---------|
| `links` | `PK` (S) | `SK` (S) | `owner_id-created_at-index` (ALL) | `expires_at` | On-demand (dev), Provisioned (prod) | Yes | KMS | No |
| `clicks` | `PK` (S) | `SK` (S) | `code-date-index` (INCLUDE) | `ttl` (recommended) or `created_at` | Provisioned with auto-scaling | Yes | KMS | Yes (NEW_IMAGE) |
| `users` | `PK` (S) | `SK` (S) | None | None | On-demand | Yes | KMS | No |

---

## LocalStack Configuration

For local development, all tables use `PAY_PER_REQUEST` (LocalStack ignores capacity settings). The `deploy/scripts/seed/main.go` script creates all three tables with the same key schemas and GSIs as production.

```bash
# Create tables locally via AWS CLI + LocalStack
aws --endpoint-url=http://localhost:4566 dynamodb create-table \
  --table-name shorty-links \
  --attribute-definitions \
    AttributeName=PK,AttributeType=S \
    AttributeName=SK,AttributeType=S \
    AttributeName=owner_id,AttributeType=S \
    AttributeName=created_at,AttributeType=N \
  --key-schema \
    AttributeName=PK,KeyType=HASH \
    AttributeName=SK,KeyType=RANGE \
  --global-secondary-indexes \
    'IndexName=owner_id-created_at-index,KeySchema=[{AttributeName=owner_id,KeyType=HASH},{AttributeName=created_at,KeyType=RANGE}],Projection={ProjectionType=ALL}' \
  --billing-mode PAY_PER_REQUEST
```
