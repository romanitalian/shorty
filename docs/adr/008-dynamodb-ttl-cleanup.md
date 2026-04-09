# ADR-008: DynamoDB TTL for Automatic Link and Click Cleanup

## Status

Accepted

## Context

Shorty has two categories of data that require automatic cleanup:

1. **Expired links**: links with a time-based TTL (`expires_at` attribute) should
   become inaccessible after their expiration and eventually be deleted.
2. **Click events**: analytics data has a 90-day retention policy. Click records
   older than 90 days should be automatically purged.

Without automatic cleanup, the database grows unboundedly. Anonymous users can create
links with up to 24-hour TTL (requirements-init.md Section 12), generating significant
ephemeral data.

## Decision

We use **DynamoDB native TTL** on both the `links` and `clicks` tables for automatic
data expiration and cleanup.

### Links table

- The `expires_at` attribute is a Unix timestamp (epoch seconds).
- When set, DynamoDB automatically deletes the item within ~48 hours after the TTL
  expires. The item is still readable after `expires_at` passes but before DynamoDB
  physically deletes it.
- The redirect Lambda checks `expires_at` **in application code** before serving a
  redirect, returning HTTP 410 (Gone) for expired links:

```go
func (s *RedirectService) Resolve(ctx context.Context, code string) (*Link, error) {
    link, err := s.getFromCacheOrDB(ctx, code)
    if err != nil {
        return nil, err
    }

    // Application-level expiry check (DynamoDB TTL deletion is eventual)
    if link.ExpiresAt != nil && time.Now().Unix() > *link.ExpiresAt {
        // Cache a tombstone to prevent repeated DB lookups
        _ = s.cache.SetTombstone(ctx, "link:"+code, 60*time.Second)
        return nil, ErrLinkExpired
    }

    return link, nil
}
```

- Links without `expires_at` (set to `nil` or `0`) are never auto-deleted by TTL.
  These are managed links that persist until the owner deletes them.

### Clicks table

- The `created_at` attribute doubles as the TTL attribute, set to `now + 90 days`
  at write time.
- The worker Lambda calculates the TTL when recording click events:

```go
clickTTL := time.Now().Add(90 * 24 * time.Hour).Unix()
```

- 90-day retention aligns with the free tier stats history depth (30 days displayed,
  90 days stored for aggregation smoothing). Pro users see up to 1 year; their clicks
  have a 365-day TTL.

### DynamoDB TTL configuration (Terraform)

```hcl
resource "aws_dynamodb_table" "links" {
  name     = "shorty-links"
  # ...
  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }
}

resource "aws_dynamodb_table" "clicks" {
  name     = "shorty-clicks"
  # ...
  ttl {
    attribute_name = "created_at"
    enabled        = true
  }
}
```

## Consequences

**Positive:**
- Zero operational overhead: no scheduled Lambda, no cron job, no manual cleanup.
- No read/write capacity consumed for deletion -- DynamoDB TTL deletes are free
  (do not consume WCUs).
- Expired items are automatically removed from GSIs as well, keeping index sizes
  proportional to active data.
- Application-level expiry check provides immediate effect; DynamoDB TTL handles
  eventual physical cleanup.

**Negative:**
- DynamoDB TTL deletion is **eventually consistent** -- items may persist up to
  48 hours after the TTL timestamp. The application must always check expiry in
  code, not rely on item absence.
- TTL deletion cannot be triggered on demand. If a link's `expires_at` is extended
  (link renewal), the TTL attribute must be updated atomically.
- TTL deletes show up in DynamoDB Streams as `REMOVE` events. If we add Streams
  processing later, we must filter TTL deletions from user-initiated deletions
  (identifiable by the `userIdentity.principalId` field = `dynamodb.amazonaws.com`).

## Alternatives Considered

**Scheduled Lambda cleanup (CloudWatch Events cron):**
Rejected. Requires a Lambda function to scan the table periodically and delete expired
items. This consumes read and write capacity, adds operational complexity (monitoring
the cleanup Lambda itself), and introduces a cleanup lag equal to the cron interval.
DynamoDB TTL is simpler and free.

**S3 lifecycle policies (archive old click data to S3):**
Deferred to post-MVP. Archiving click data to S3 before TTL deletion would preserve
historical data for enterprise users with unlimited stats history. This requires
a DynamoDB Streams consumer that writes to S3 on TTL deletion events. Not needed
for MVP where the maximum stats depth is 1 year.

**Application-level soft delete (mark as expired, never physically delete):**
Rejected. Soft-deleted records still consume storage and read capacity (scans/queries
return them). DynamoDB charges for storage, and click event volume makes unbounded
retention expensive.

**Shorter/longer click retention:**
90 days balances analytics utility against storage cost. At 10K RPS, 90 days of
click data is approximately 77 billion records. With sparse click attributes
(~200 bytes each), this is ~15 TB. DynamoDB TTL ensures this cap is enforced
automatically.
