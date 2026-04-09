# Runbook: DynamoDB Throttling

**Alert:** `DynamoDBThrottling` / `DynamoDBThrottlingSustained`
**Severity:** SEV2 (sustained) / SEV3 (intermittent)
**SLO Impact:** Throttled reads on `links` table degrade redirect latency (p99 target < 100ms). Throttled writes on `clicks` table cause SQS DLQ growth.

---

## Symptoms

- `DynamoDBThrottling` or `DynamoDBThrottlingSustained` alert firing.
- CloudWatch `ThrottledRequests` > 0 on `links` or `clicks` table.
- `ThrottlingException` errors in Lambda logs.
- Redirect latency spike (DynamoDB fallback path is slower than Redis cache).
- Elevated 5xx error rate if SDK retries are exhausted.
- SQS DLQ growth if the `clicks` table is throttled (worker write failures).

---

## Diagnosis

### 1. Identify which table and operation is throttled

```bash
# Check throttled requests per table (last 30 min)
for table in links clicks users; do
  echo "=== $table ==="
  aws cloudwatch get-metric-statistics \
    --namespace AWS/DynamoDB \
    --metric-name ThrottledRequests \
    --dimensions Name=TableName,Value=$table \
    --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
    --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
    --period 60 --statistics Sum
done
```

### 2. Check consumed vs provisioned capacity

```bash
# Current table settings
aws dynamodb describe-table --table-name links \
  --query '{BillingMode: BillingModeSummary.BillingMode, ProvisionedThroughput: ProvisionedThroughput, GSIs: GlobalSecondaryIndexes[*].{Name: IndexName, Throughput: ProvisionedThroughput}}'

# Consumed read capacity (last 30 min)
aws cloudwatch get-metric-statistics \
  --namespace AWS/DynamoDB \
  --metric-name ConsumedReadCapacityUnits \
  --dimensions Name=TableName,Value=links \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum

# Consumed write capacity (last 30 min)
aws cloudwatch get-metric-statistics \
  --namespace AWS/DynamoDB \
  --metric-name ConsumedWriteCapacityUnits \
  --dimensions Name=TableName,Value=links \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

### 3. Detect hot partitions with Contributor Insights

```bash
# Enable Contributor Insights (if not already enabled)
aws dynamodb update-contributor-insights \
  --table-name links \
  --contributor-insights-action ENABLE

# Check the most accessed partition keys (top hot keys)
aws dynamodb describe-contributor-insights \
  --table-name links

# View hot partition key data in CloudWatch
# Metric: DynamoDBContributorInsights
# Dimension: TableName=links
# Contributor: PKValue
```

Use CloudWatch console to view "Contributor Insights" tab on the table. Look for a single `LINK#{code}` PK consuming a disproportionate share of throughput.

### 4. Check GSI throttling

```bash
# GSI throttling (owner_id-created_at-index) can back-pressure the base table
aws cloudwatch get-metric-statistics \
  --namespace AWS/DynamoDB \
  --metric-name ThrottledRequests \
  --dimensions Name=TableName,Value=links Name=GlobalSecondaryIndexName,Value=owner_id-created_at-index \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

### 5. CloudWatch Insights -- throttle events in Lambda logs

```sql
filter @message like /ThrottlingException/ or @message like /ProvisionedThroughputExceededException/
| fields @timestamp, @message, @logStream
| sort @timestamp desc
| limit 50
```

---

## Mitigation

### Step 1: Increase provisioned capacity (if using provisioned mode)

```bash
# Increase read and write capacity on the links table
aws dynamodb update-table \
  --table-name links \
  --provisioned-throughput ReadCapacityUnits=10000,WriteCapacityUnits=2000

# If GSI is also throttled, update its capacity
aws dynamodb update-table \
  --table-name links \
  --global-secondary-index-updates '[{
    "Update": {
      "IndexName": "owner_id-created_at-index",
      "ProvisionedThroughput": {
        "ReadCapacityUnits": 5000,
        "WriteCapacityUnits": 1000
      }
    }
  }]'
```

### Step 2: Switch to on-demand mode (if burst is unpredictable)

```bash
aws dynamodb update-table \
  --table-name links \
  --billing-mode PAY_PER_REQUEST
```

Note: Switching from provisioned to on-demand takes effect immediately. Switching back to provisioned requires a 24-hour cooldown.

### Step 3: Hot partition -- block abusive traffic

If a single `LINK#{code}` is generating disproportionate traffic:

1. Check if the short code is being abused (bot traffic, DDoS).
2. Block via WAF rate-based rule or CloudFront origin request policy.
3. Consider adding the code to a Redis-based blocklist.

### Step 4: Clicks table throttled -- reduce worker batch size

If the `clicks` table is throttled due to write volume:

```bash
# Temporarily reduce worker Lambda batch size
aws lambda update-function-configuration \
  --function-name shorty-worker \
  --environment "Variables={BATCH_SIZE=5,SQS_QUEUE_URL=$SQS_QUEUE_URL}"
```

---

## Prevention

1. **DynamoDB Auto Scaling:** Enable auto scaling on both tables with target utilization of 70%. Verify scaling policies are active:
   ```bash
   aws application-autoscaling describe-scalable-targets \
     --service-namespace dynamodb --resource-ids "table/links"
   ```
2. **Contributor Insights:** Keep enabled on both `links` and `clicks` tables for ongoing hot partition visibility.
3. **Partition key review:** The `LINK#{code}` partition key distributes well across codes. If a single code goes viral, Redis cache should absorb the reads. Verify cache TTL is sufficient.
4. **GSI capacity:** Ensure GSI provisioned capacity is at least 50% of the base table capacity.
5. **On-demand mode consideration:** For unpredictable traffic patterns, on-demand billing avoids throttling entirely at the cost of higher per-request pricing.

---

## Escalation

| Condition | Action |
|-----------|--------|
| Throttling persists after capacity increase | Check for account-level DynamoDB limits; open AWS Support case |
| Hot partition identified (single key > 3000 RCU/s) | Engineering review of access pattern; consider caching or sharding |
| GSI back-pressure causing base table throttling | Engineering Lead -- may need GSI redesign |
| Sustained throttling causing SLO burn > 6x | Follow [high-error-rate.md](high-error-rate.md) escalation path |
