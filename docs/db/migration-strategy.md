# Migration Strategy -- Shorty URL Shortener

## Overview

This document covers schema evolution, data migration, version upgrades, disaster recovery, and rollback procedures for both DynamoDB and ElastiCache Redis.

DynamoDB is schemaless -- there is no `ALTER TABLE`. Schema changes fall into categories with very different risk profiles, from zero-effort (adding attributes) to full table rebuilds (changing key structure).

---

## 1. DynamoDB Schema Changes

### 1.1 Adding a New Attribute

**Risk: None. No migration needed.**

DynamoDB does not enforce a schema beyond the key attributes. New code simply writes the new attribute; old items do not have it.

**Application requirement:** all read paths must handle the attribute being absent (nil/zero-value). Use Go struct tags with `omitempty` and set defaults in the repository layer.

```go
type Link struct {
    // ... existing fields ...
    NewField string `dynamodbav:"new_field,omitempty"`
}

func (l *Link) GetNewField() string {
    if l.NewField == "" {
        return "default_value"
    }
    return l.NewField
}
```

**Backfill (if needed):** run a one-time scan-and-update script to populate existing items. See Section 3.

### 1.2 Removing an Attribute

**Risk: Low. No migration needed.**

Stop writing the attribute in application code. Existing items retain the old attribute until overwritten or TTL-expired. This is harmless -- DynamoDB does not charge for attributes that are never read.

If the attribute consumes significant storage (e.g., a large text field on millions of items), run a backfill script to remove it with `REMOVE attribute_name` in UpdateExpression.

### 1.3 Adding a GSI

**Risk: Low-Medium. Online operation, but impacts throughput during backfill.**

DynamoDB creates new GSIs online -- the base table remains fully available for reads and writes. However:

1. **Backfill consumes read capacity** on the base table (DynamoDB scans all items to populate the GSI).
2. **Backfill consumes write capacity** on the new GSI.
3. The GSI is not queryable until backfill completes (status: `CREATING` -> `ACTIVE`).

**Procedure:**

```bash
# 1. Add GSI via Terraform
terraform plan -target=module.dynamodb
terraform apply -target=module.dynamodb

# 2. Monitor backfill progress
aws dynamodb describe-table --table-name shorty-links \
  --query "Table.GlobalSecondaryIndexes[?IndexName=='new-index'].IndexStatus"

# 3. Monitor with CloudWatch
# Metric: OnlineIndexPercentageProgress
# Metric: ConsumedReadCapacityUnits (expect spike during backfill)
```

**Capacity impact mitigation:**
- For provisioned tables: temporarily increase RCU by 50% before adding the GSI, reduce after backfill completes.
- For on-demand tables: DynamoDB auto-scales, but monitor for throttling.
- Schedule GSI additions during low-traffic windows.

**Limits:** maximum 20 GSIs per table. Current usage: `links` has 1, `clicks` has 1, `users` has 0.

### 1.4 Removing a GSI

**Risk: Low. Online operation.**

```bash
# Remove via Terraform (or AWS CLI)
# Table remains available. GSI deletion is immediate.
# Note: 24-hour cooldown before you can create a new GSI with the same name.
```

### 1.5 Changing Key Structure (PK/SK)

**Risk: High. Requires blue/green table swap.**

This is the most complex migration and should be avoided if possible. Examples: changing `LINK#{code}` to a different partition key format, or splitting a single-table design into multiple tables.

**Blue/Green Table Swap Procedure:**

```
Phase 1: Prepare (no downtime)
  [1] Create new table ("shorty-links-v2") with new key schema
  [2] Enable DynamoDB Streams on the OLD table (NEW_AND_OLD_IMAGES)
  [3] Deploy a Stream processor Lambda that:
      - Reads change events from the old table
      - Transforms and writes to the new table
  [4] Run backfill script: scan old table -> batch-write to new table
      (Stream processor handles any writes during backfill)
  [5] Validate: compare item counts, sample data checks

Phase 2: Switch (brief downtime or feature-flag)
  [6] Option A (zero-downtime): feature flag switches reads to new table
      - Deploy with flag OFF (still reading old table)
      - Turn flag ON (reads from new table)
      - Verify correctness
      - Deploy again with flag removed (always reads new table)
  [6] Option B (maintenance window): brief API downtime
      - Stop all writes
      - Wait for stream processor to drain
      - Switch table name in config
      - Resume writes

Phase 3: Cleanup (after validation period)
  [7] Keep old table for 30 days as rollback safety net
  [8] Remove Stream processor Lambda
  [9] Delete old table
```

**Backfill script pattern:**

```go
// deploy/scripts/migrate/main.go

func backfill(ctx context.Context, oldTable, newTable string) error {
    paginator := dynamodb.NewScanPaginator(client, &dynamodb.ScanInput{
        TableName: &oldTable,
    })

    for paginator.HasMorePages() {
        page, err := paginator.NextPage(ctx)
        if err != nil {
            return fmt.Errorf("scan page: %w", err)
        }

        // Transform items to new schema
        var writeRequests []types.WriteRequest
        for _, item := range page.Items {
            newItem := transformItem(item) // apply schema changes
            writeRequests = append(writeRequests, types.WriteRequest{
                PutRequest: &types.PutRequest{Item: newItem},
            })
        }

        // BatchWriteItem (max 25 items per batch)
        for i := 0; i < len(writeRequests); i += 25 {
            end := min(i+25, len(writeRequests))
            _, err := client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
                RequestItems: map[string][]types.WriteRequest{
                    newTable: writeRequests[i:end],
                },
            })
            if err != nil {
                return fmt.Errorf("batch write: %w", err)
            }
        }
    }
    return nil
}
```

### 1.6 Changing Capacity Mode

**Risk: Low. Single API call.**

```bash
aws dynamodb update-table \
  --table-name shorty-links \
  --billing-mode PAY_PER_REQUEST
```

Constraints:
- 24-hour cooldown between capacity mode changes.
- Table remains available during the switch.
- Manage via Terraform to keep state consistent.

---

## 2. Attribute Backfill

When a new attribute needs to be populated on existing items (e.g., adding `utm_campaign` to links created before the feature existed).

### 2.1 Backfill Script

```go
// deploy/scripts/migrate/backfill.go

func backfillAttribute(ctx context.Context, table, attrName string, defaultValue types.AttributeValue) error {
    paginator := dynamodb.NewScanPaginator(client, &dynamodb.ScanInput{
        TableName:        &table,
        FilterExpression: aws.String("attribute_not_exists(#attr)"),
        ExpressionAttributeNames: map[string]string{
            "#attr": attrName,
        },
    })

    for paginator.HasMorePages() {
        page, err := paginator.NextPage(ctx)
        if err != nil {
            return err
        }

        for _, item := range page.Items {
            _, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
                TableName: &table,
                Key: map[string]types.AttributeValue{
                    "PK": item["PK"],
                    "SK": item["SK"],
                },
                UpdateExpression: aws.String("SET #attr = :val"),
                ConditionExpression: aws.String("attribute_not_exists(#attr)"),
                ExpressionAttributeNames: map[string]string{
                    "#attr": attrName,
                },
                ExpressionAttributeValues: map[string]types.AttributeValue{
                    ":val": defaultValue,
                },
            })
            if err != nil {
                var condErr *types.ConditionalCheckFailedException
                if errors.As(err, &condErr) {
                    continue // already backfilled (race with application writes)
                }
                return err
            }
        }
    }
    return nil
}
```

### 2.2 Backfill Best Practices

1. **Use conditional expressions** -- `attribute_not_exists` prevents overwriting values set by application code during the backfill.
2. **Rate limit the scan** -- add a `time.Sleep` between pages to avoid consuming all table capacity. Or use a dedicated provisioned capacity allocation.
3. **Idempotent** -- the script can be run multiple times safely.
4. **Progress tracking** -- log the `LastEvaluatedKey` so the script can resume if interrupted.
5. **Test on dev first** -- run against LocalStack before production.

---

## 3. Redis Version Upgrades

### 3.1 ElastiCache In-Place Upgrade

ElastiCache supports in-place Redis engine upgrades (e.g., 7.0 -> 7.1). Downgrades are not supported.

**Procedure:**

```bash
# 1. Check available versions
aws elasticache describe-cache-engine-versions --engine redis

# 2. Upgrade via Terraform
# Change engine_version in the replication group resource
# terraform apply

# 3. Or via CLI (for emergency upgrades outside Terraform)
aws elasticache modify-replication-group \
  --replication-group-id shorty-redis \
  --engine-version 7.1 \
  --apply-immediately
```

**Behavior during upgrade:**
- With Multi-AZ enabled: failover to replica during primary upgrade, then upgrade replica. Brief (~30s) connection loss.
- Without Multi-AZ: full downtime during upgrade (1-5 minutes depending on dataset size).

**Pre-upgrade checklist:**
1. Verify Lua scripts are compatible with the new Redis version.
2. Check for deprecated commands in application code.
3. Take a manual snapshot before upgrade.
4. Test the upgrade in staging first.
5. Schedule during low-traffic window.

### 3.2 Major Version Upgrades (e.g., 6.x -> 7.x)

Major version upgrades may introduce breaking changes. Follow the same in-place upgrade process but with additional validation:

1. Review the [Redis release notes](https://redis.io/docs/about/releases/) for breaking changes.
2. Test all Lua scripts against the new version in a local Docker container.
3. Verify `maxmemory-policy` behavior hasn't changed.
4. Test connection pooling behavior (some versions changed connection lifecycle).

---

## 4. Data Export and Disaster Recovery

### 4.1 DynamoDB Backup Strategies

**Point-in-Time Recovery (PITR):**
```hcl
resource "aws_dynamodb_table" "links" {
  # ...
  point_in_time_recovery {
    enabled = true
  }
}
```
- Continuous backups with 35-day retention.
- Restore to any second within the retention window.
- Restore creates a NEW table (does not overwrite the existing one).
- Restore time: minutes to hours depending on table size.

**On-Demand Backups:**
```bash
# Create manual backup before risky operations
aws dynamodb create-backup \
  --table-name shorty-links \
  --backup-name "pre-migration-$(date +%Y%m%d)"
```
- Retained until explicitly deleted.
- Zero impact on table performance.
- Use before any migration or schema change.

**Cross-Region Backup (DynamoDB Global Tables):**
- Configured via Terraform for production.
- Active-active replication with < 1 second lag.
- Provides RPO < 1 second for regional failures.

### 4.2 DynamoDB Data Export (for analytics or DR)

```bash
# Export to S3 (for large-scale analysis or cold storage)
aws dynamodb export-table-to-point-in-time \
  --table-arn arn:aws:dynamodb:us-east-1:123456789:table/shorty-links \
  --s3-bucket shorty-backups \
  --s3-prefix exports/ \
  --export-format DYNAMODB_JSON
```

- Export runs in the background, no impact on table throughput.
- Output format: DynamoDB JSON or Amazon Ion.
- Use for: DR cold storage, data warehouse import, compliance archives.

### 4.3 Redis Backup (ElastiCache Snapshots)

**Automatic snapshots:**
```hcl
resource "aws_elasticache_replication_group" "shorty" {
  # ...
  snapshot_retention_limit = 7    # keep 7 daily snapshots
  snapshot_window          = "03:00-04:00"  # UTC, low-traffic window
}
```

**Manual snapshot (before upgrades/migrations):**
```bash
aws elasticache create-snapshot \
  --replication-group-id shorty-redis \
  --snapshot-name "pre-upgrade-$(date +%Y%m%d)"
```

**Restore from snapshot:**
```bash
aws elasticache create-replication-group \
  --replication-group-id shorty-redis-restored \
  --snapshot-name "pre-upgrade-20260405" \
  --replication-group-description "Restored from snapshot"
```

**Important:** Redis data is ephemeral cache + rate limiter state. Losing Redis data is recoverable:
- Cache repopulates on demand from DynamoDB (temporary latency increase).
- Rate limiter state resets (brief window where rate limits are not enforced -- acceptable for the seconds it takes to restore).

---

## 5. Rollback Procedures

### 5.1 Rolling Back a New Attribute

No action needed. The attribute is ignored by old code. If the attribute causes issues in new code, revert the application deployment. Old items without the attribute continue to work.

### 5.2 Rolling Back a GSI

```bash
# Delete the GSI (online, immediate)
aws dynamodb update-table \
  --table-name shorty-links \
  --global-secondary-index-updates '[{"Delete":{"IndexName":"problematic-index"}}]'
```

Application code must not reference the GSI after deletion. Deploy the rollback application version first.

### 5.3 Rolling Back a Blue/Green Table Swap

If the new table is faulty after the switch:

1. **If using feature flag:** turn the flag OFF (revert to old table). The old table is still receiving writes via DynamoDB Streams processor.
2. **If using config switch:** change the table name back in SSM Parameter Store / environment variable, redeploy Lambdas.
3. **Critical:** the old table must still be receiving writes via the Stream processor. This is why we keep the old table for 30 days.

### 5.4 Rolling Back a Redis Version Upgrade

ElastiCache does not support in-place downgrades. Rollback options:

1. **Restore from snapshot** taken before the upgrade (creates a new replication group).
2. **Accept the upgrade** and fix application compatibility issues.

This is why pre-upgrade testing in staging is critical.

### 5.5 Emergency Recovery Playbook

| Scenario | Action | RTO |
|----------|--------|-----|
| Redis node failure | ElastiCache automatic failover to replica | ~30s |
| Redis cluster loss | Restore from snapshot; cache rebuilds from DynamoDB | 5-10 min |
| DynamoDB table corrupted | PITR restore to new table, switch config | 15-60 min |
| DynamoDB region outage | Global table failover (if configured) | < 1 min |
| Bad migration deployed | Feature flag rollback or config switch | 1-5 min |
| Application bug writing bad data | Fix code, run backfill/cleanup script | Varies |

---

## 6. LocalStack Limitations vs Production Parity

### 6.1 Known Limitations

| Feature | LocalStack Behavior | Production Behavior | Impact |
|---------|-------------------|-------------------|--------|
| DynamoDB Streams | Supported (basic) | Full CDC with ordering guarantees | Stream-based migrations may behave differently |
| DynamoDB TTL | Supported but slower deletion | Items deleted within 48h of expiry | Tests may see expired items longer than expected |
| DynamoDB PITR | Not supported | Full 35-day continuous backup | Cannot test PITR restore locally |
| DynamoDB Global Tables | Not supported | Multi-region active-active | Cannot test failover locally |
| DynamoDB Auto-Scaling | Not supported | CloudWatch-based scaling | Capacity limits not enforced locally |
| ElastiCache | Not available (use plain Redis) | Managed Redis with failover | No failover, no snapshots, no encryption |
| DynamoDB GSI Backfill | Instant (small dataset) | Can take hours for large tables | Backfill timing not representative |
| DynamoDB Throttling | Not enforced | Provisioned capacity limits | Cannot test throttling behavior locally |

### 6.2 Mitigation Strategies

1. **Integration tests** (`make test-integration`) run against LocalStack for basic correctness. They verify access patterns, conditional expressions, and Lua scripts.

2. **E2E tests** (`make test-e2e`) run against a real dev AWS environment. They catch production-specific behaviors (throttling, TTL timing, auto-scaling).

3. **Migration scripts** must be tested in the dev AWS environment before production. LocalStack testing provides confidence in logic, but production testing validates capacity impact.

4. **Redis feature parity** is high between Docker Redis and ElastiCache. The main differences are: no AUTH token locally, no TLS, no cluster mode. Application code should handle both configurations via environment variables.

---

## 7. Migration Checklist Template

Use this checklist for any migration:

```
Pre-Migration:
[ ] Migration script tested against LocalStack
[ ] Migration script tested against dev AWS environment
[ ] DynamoDB backup created (on-demand backup)
[ ] Redis snapshot created (if applicable)
[ ] Rollback procedure documented and tested
[ ] Capacity impact estimated (additional RCU/WCU during migration)
[ ] Maintenance window communicated (if downtime required)
[ ] Monitoring dashboards open (DynamoDB throttling, Redis evictions)

Execution:
[ ] Execute migration in dev environment
[ ] Validate dev environment (access pattern tests, data integrity)
[ ] Execute migration in production
[ ] Monitor CloudWatch for 30 minutes post-migration

Post-Migration:
[ ] Verify application health (error rate, latency)
[ ] Verify data integrity (sample checks)
[ ] Scale down temporary capacity increases
[ ] Update documentation (data-model.md, this file)
[ ] Keep old resources for 30-day rollback window
[ ] Schedule cleanup of old resources
```
