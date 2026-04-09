#!/usr/bin/env bash
# LocalStack initialization script
# Runs automatically on LocalStack startup via /etc/localstack/init/ready.d/
# Creates DynamoDB tables, SQS queues, and Secrets Manager secrets.
set -euo pipefail

echo "=== Shorty LocalStack Init: starting ==="

REGION="us-east-1"

# ---------------------------------------------------------------------------
# DynamoDB Tables
# ---------------------------------------------------------------------------

echo "Creating DynamoDB table: shorty-links"
awslocal dynamodb create-table \
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
    '[{
      "IndexName": "owner_id-created_at-index",
      "KeySchema": [
        {"AttributeName": "owner_id", "KeyType": "HASH"},
        {"AttributeName": "created_at", "KeyType": "RANGE"}
      ],
      "Projection": {"ProjectionType": "ALL"}
    }]' \
  --billing-mode PAY_PER_REQUEST \
  --region "$REGION"

# Enable TTL on expires_at
awslocal dynamodb update-time-to-live \
  --table-name shorty-links \
  --time-to-live-specification Enabled=true,AttributeName=expires_at \
  --region "$REGION"

echo "Creating DynamoDB table: shorty-clicks"
awslocal dynamodb create-table \
  --table-name shorty-clicks \
  --attribute-definitions \
    AttributeName=PK,AttributeType=S \
    AttributeName=SK,AttributeType=S \
    AttributeName=created_at,AttributeType=N \
  --key-schema \
    AttributeName=PK,KeyType=HASH \
    AttributeName=SK,KeyType=RANGE \
  --global-secondary-indexes \
    '[{
      "IndexName": "code-date-index",
      "KeySchema": [
        {"AttributeName": "PK", "KeyType": "HASH"},
        {"AttributeName": "created_at", "KeyType": "RANGE"}
      ],
      "Projection": {
        "ProjectionType": "INCLUDE",
        "NonKeyAttributes": ["country", "device_type", "referer_domain", "ip_hash"]
      }
    }]' \
  --billing-mode PAY_PER_REQUEST \
  --region "$REGION"

# Enable TTL on created_at (90-day retention)
awslocal dynamodb update-time-to-live \
  --table-name shorty-clicks \
  --time-to-live-specification Enabled=true,AttributeName=created_at \
  --region "$REGION"

echo "Creating DynamoDB table: shorty-users"
awslocal dynamodb create-table \
  --table-name shorty-users \
  --attribute-definitions \
    AttributeName=PK,AttributeType=S \
    AttributeName=SK,AttributeType=S \
  --key-schema \
    AttributeName=PK,KeyType=HASH \
    AttributeName=SK,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST \
  --region "$REGION"

# ---------------------------------------------------------------------------
# SQS FIFO Queue
# ---------------------------------------------------------------------------

echo "Creating SQS FIFO queue: click-events.fifo"
awslocal sqs create-queue \
  --queue-name click-events.fifo \
  --attributes '{
    "FifoQueue": "true",
    "ContentBasedDeduplication": "true",
    "VisibilityTimeout": "60",
    "MessageRetentionPeriod": "86400"
  }' \
  --region "$REGION"

# Dead letter queue
echo "Creating SQS FIFO DLQ: click-events-dlq.fifo"
awslocal sqs create-queue \
  --queue-name click-events-dlq.fifo \
  --attributes '{
    "FifoQueue": "true",
    "MessageRetentionPeriod": "1209600"
  }' \
  --region "$REGION"

# ---------------------------------------------------------------------------
# Secrets Manager
# ---------------------------------------------------------------------------

echo "Creating Secrets Manager secret: shorty/ip-salt"
awslocal secretsmanager create-secret \
  --name shorty/ip-salt \
  --secret-string "local-dev-salt-change-in-prod" \
  --region "$REGION"

echo "Creating Secrets Manager secret: shorty/redis-auth"
awslocal secretsmanager create-secret \
  --name shorty/redis-auth \
  --secret-string "" \
  --region "$REGION"

# ---------------------------------------------------------------------------
# Verification
# ---------------------------------------------------------------------------

echo ""
echo "=== Verification ==="
echo "DynamoDB tables:"
awslocal dynamodb list-tables --region "$REGION"

echo "SQS queues:"
awslocal sqs list-queues --region "$REGION"

echo "Secrets:"
awslocal secretsmanager list-secrets --region "$REGION" --query 'SecretList[].Name'

echo ""
echo "=== Shorty LocalStack Init: complete ==="
