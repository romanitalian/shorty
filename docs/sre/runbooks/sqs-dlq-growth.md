# Runbook: SQS Dead Letter Queue Growth

**Alert:** `SQSDLQNotEmpty` / `SQSDLQDepthCritical` / `SQSQueueBacklog`
**Severity:** SEV3 (DLQ non-empty) / SEV2 (DLQ > 100 messages or queue backlog)
**SLO Impact:** Click event processing is best-effort, so DLQ growth does not directly impact redirect availability. However, sustained DLQ growth indicates systematic worker failure that may affect analytics accuracy and could signal broader infrastructure issues.

---

## Symptoms

- `SQSDLQNotEmpty` alert: messages visible in `shorty-clicks-dlq.fifo`.
- `SQSDLQDepthCritical` alert: DLQ depth > 100 messages.
- `SQSQueueBacklog` alert: oldest message age > 60 seconds in the main queue.
- Click analytics data missing or delayed.
- Worker Lambda error rate elevated.

---

## Diagnosis

### 1. Inspect DLQ messages

```bash
# Check DLQ depth
aws sqs get-queue-attributes \
  --queue-url https://sqs.{region}.amazonaws.com/{account}/shorty-clicks-dlq.fifo \
  --attribute-names ApproximateNumberOfMessagesVisible ApproximateNumberOfMessagesNotVisible

# Read sample messages from DLQ (does NOT delete them)
aws sqs receive-message \
  --queue-url https://sqs.{region}.amazonaws.com/{account}/shorty-clicks-dlq.fifo \
  --max-number-of-messages 10 \
  --visibility-timeout 0 \
  --attribute-names All \
  --message-attribute-names All
```

Look for patterns in the failed messages:
- **Malformed payload:** Serialization bug in the redirect Lambda.
- **Same message group ID:** All failures from one partition key.
- **Consistent error attribute:** `ApproximateReceiveCount` shows how many times delivery was attempted.

### 2. Check main queue health

```bash
# Main queue depth and oldest message
aws sqs get-queue-attributes \
  --queue-url https://sqs.{region}.amazonaws.com/{account}/shorty-clicks.fifo \
  --attribute-names ApproximateNumberOfMessagesVisible ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed
```

### 3. Check worker Lambda errors

```bash
# Worker Lambda error count (last 30 min)
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Errors \
  --dimensions Name=FunctionName,Value=shorty-worker \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum

# Worker invocation count
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Invocations \
  --dimensions Name=FunctionName,Value=shorty-worker \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

### 4. Analyze worker logs for error patterns

```sql
-- CloudWatch Insights: worker errors
filter @message like /ERROR/ and @logGroup like /shorty-worker/
| fields @timestamp, @message
| sort @timestamp desc
| limit 50
```

```sql
-- CloudWatch Insights: worker batch processing time
filter @type = "REPORT" and @logGroup like /shorty-worker/
| stats avg(@duration) as avg_ms, max(@duration) as max_ms, count(*) as invocations by bin(5m)
```

### 5. Check if DynamoDB clicks table is the bottleneck

```bash
# Throttled writes on clicks table
aws cloudwatch get-metric-statistics \
  --namespace AWS/DynamoDB \
  --metric-name ThrottledRequests \
  --dimensions Name=TableName,Value=clicks Name=Operation,Value=PutItem \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

### 6. Check if worker Lambda is throttled or disabled

```bash
# Check if worker concurrency was set to 0 (disabled during a previous incident)
aws lambda get-function-concurrency --function-name shorty-worker

# Check for throttling
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Throttles \
  --dimensions Name=FunctionName,Value=shorty-worker \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

---

## Mitigation

### Step 1: Fix the root cause based on diagnosis

| Root Cause | Action |
|------------|--------|
| DynamoDB `clicks` table throttling | Increase capacity -- see [dynamodb-throttling.md](dynamodb-throttling.md) |
| Malformed messages (serialization bug) | Fix the redirect Lambda, redeploy |
| Worker Lambda timeout | Increase timeout or reduce batch size |
| Worker Lambda disabled (concurrency = 0) | Re-enable: `aws lambda put-function-concurrency --function-name shorty-worker --reserved-concurrent-executions 100` |
| Worker code bug (recent deployment) | Rollback: `aws lambda update-alias --function-name shorty-worker --name live --function-version {previous}` |

### Step 2: Reduce batch size if timeouts

```bash
# Check current event source mapping
aws lambda list-event-source-mappings \
  --function-name shorty-worker \
  --query 'EventSourceMappings[0].{UUID: UUID, BatchSize: BatchSize, MaxBatchWindow: MaximumBatchingWindowInSeconds}'

# Reduce batch size
aws lambda update-event-source-mapping \
  --uuid {mapping_uuid} \
  --batch-size 5
```

### Step 3: Replay DLQ messages (after root cause is fixed)

```bash
# Start message move task (replays all DLQ messages back to the main queue)
aws sqs start-message-move-task \
  --source-arn arn:aws:sqs:{region}:{account}:shorty-clicks-dlq.fifo \
  --destination-arn arn:aws:sqs:{region}:{account}:shorty-clicks.fifo

# Monitor the move task
aws sqs list-message-move-tasks \
  --source-arn arn:aws:sqs:{region}:{account}:shorty-clicks-dlq.fifo
```

Important: Only replay after the root cause is fixed. Replaying into a broken pipeline will just re-fill the DLQ.

### Step 4: If messages are poison (unprocessable)

```bash
# Purge specific poison messages by receiving and deleting them
aws sqs receive-message \
  --queue-url https://sqs.{region}.amazonaws.com/{account}/shorty-clicks-dlq.fifo \
  --max-number-of-messages 1

# Delete a specific message using its ReceiptHandle
aws sqs delete-message \
  --queue-url https://sqs.{region}.amazonaws.com/{account}/shorty-clicks-dlq.fifo \
  --receipt-handle {receipt_handle}

# Or purge the entire DLQ (CAUTION: loses all messages)
aws sqs purge-queue \
  --queue-url https://sqs.{region}.amazonaws.com/{account}/shorty-clicks-dlq.fifo
```

---

## Prevention

1. **Message validation:** The worker Lambda should validate message schema before processing. Malformed messages should be logged and discarded rather than retried.
2. **DLQ redrive policy:** The FIFO queue `maxReceiveCount` should be set to 3 (retry 3 times before DLQ). Verify:
   ```bash
   aws sqs get-queue-attributes \
     --queue-url https://sqs.{region}.amazonaws.com/{account}/shorty-clicks.fifo \
     --attribute-names RedrivePolicy
   ```
3. **Worker timeout budget:** Worker Lambda timeout should be at least 3x the expected batch processing time. Monitor `shorty_worker_batch_duration_seconds`.
4. **DynamoDB capacity:** Ensure the `clicks` table has sufficient write capacity for peak click volume. Auto-scaling should be enabled.
5. **Alerting:** The `SQSDLQNotEmpty` alert fires on any DLQ message, enabling early detection before systematic failure.

---

## Escalation

| Condition | Action |
|-----------|--------|
| DLQ depth > 1000 messages | Engineering Lead -- systematic failure, may need code fix |
| Poison messages from serialization bug | Engineering team -- fix redirect Lambda event publisher |
| DynamoDB throttling causing DLQ growth | Follow [dynamodb-throttling.md](dynamodb-throttling.md) |
| Worker Lambda repeatedly failing after rollback | Engineering Lead -- root cause investigation required |
| DLQ messages contain PII or sensitive data | Security team -- review data handling |
