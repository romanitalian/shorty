# ADR-001: DynamoDB Single-Table Design

## Status

Accepted

## Context

Shorty needs a data store for links, click events, and user profiles. DynamoDB is the
chosen database (serverless, single-digit-ms latency, pay-per-request pricing aligns
with Lambda). The question is whether to use a single table with composite keys or
separate tables per entity.

The redirect hot path has a hard p99 < 100 ms target at 10,000 RPS (see RFC-0001).
Every millisecond of DynamoDB overhead matters. The primary access patterns are:

1. **Redirect lookup**: Get link by code (`PK=LINK#{code}, SK=META`).
2. **Click recording**: Write click event (`PK=LINK#{code}, SK=CLICK#{ts}#{uuid}`).
3. **Dashboard list**: All links for a user, sorted by creation date.
4. **Stats aggregation**: All clicks for a link within a time range.
5. **User profile**: Get user by Cognito sub (`PK=USER#{sub}, SK=PROFILE`).

## Decision

We use a **two-physical-table design** with single-table patterns within each table:

- **`links` table** -- stores link metadata (`SK=META`) with a GSI
  `owner_id-created_at-index` for the dashboard query.
- **`clicks` table** -- stores click events (`PK=LINK#{code}`, `SK=CLICK#{ts}#{uuid}`)
  with a GSI `code-date-index` for daily aggregation. This table has a 90-day TTL.
- **`users` table** -- stores user profiles (`PK=USER#{sub}`, `SK=PROFILE`).

Key design choices:

1. **Composite primary key (`PK` + `SK`)** enables related data co-location. All data
   for a single link lives under `LINK#{code}`, making range queries efficient.

2. **Prefix-based key patterns** (`LINK#`, `CLICK#`, `USER#`) provide namespace isolation
   and make debugging straightforward when scanning tables in the console.

3. **Clicks in a separate table** because click events have fundamentally different
   characteristics: high write volume, 90-day TTL, and no transactional relationship
   with link metadata. Separating them avoids hot partition interference with the
   redirect read path and allows independent capacity provisioning.

4. **Atomic counter for `click_count`** on the links table uses `UpdateExpression`
   `SET click_count = click_count + :inc` with a `ConditionExpression` to enforce
   `max_clicks`:

   ```
   ConditionExpression: "click_count < :max OR attribute_not_exists(max_clicks)"
   ```

5. **GSI `owner_id-created_at-index`** on `links` powers the dashboard. Projects only
   `code`, `title`, `original_url`, `click_count`, `is_active`, `created_at` to
   minimize read capacity.

## Consequences

**Positive:**
- Single GetItem for redirect lookup -- one read capacity unit, sub-5ms latency.
- Click events scale independently from link metadata.
- DynamoDB TTL handles cleanup automatically (no cron Lambda needed for clicks).
- GSI provides efficient dashboard queries without table scans.

**Negative:**
- Three tables to manage in Terraform instead of one.
- Cross-table consistency is eventual (click count on links table vs actual click
  records in clicks table may briefly diverge).
- Contributors unfamiliar with DynamoDB key design patterns face a learning curve.

## Alternatives Considered

**Pure single-table design (one table for everything):**
Rejected. Click events at 10K RPS would create hot partitions on `LINK#{code}` keys
for popular links, degrading redirect read latency. Separate tables allow independent
throughput scaling.

**Aurora Serverless (PostgreSQL):**
Rejected. Connection pooling overhead and cold start latency (~300-500ms for proxy
initialization) are incompatible with the p99 < 100ms redirect target.
VPC-bound Lambda adds additional cold start penalty. See RFC-0001.

**Multi-table with no composite keys (one attribute PK):**
Rejected. Composite keys enable range queries on click events (`SK begins_with CLICK#`)
and future entity co-location without additional GSIs.
