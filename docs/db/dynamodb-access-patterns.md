# DynamoDB Access Patterns

Complete access pattern matrix for all Shorty operations. Every pattern maps to a specific table, key condition, index, and DynamoDB operation. **No operation requires a full table Scan.**

---

## Table: `links`

| # | Operation | DDB Op | PK | SK / SK Condition | Index | Projection | Condition Expression | RCU/WCU | Notes |
|---|-----------|--------|-----|-------------------|-------|------------|---------------------|---------|-------|
| L1 | Create link (collision-safe) | PutItem | `LINK#{code}` | `META` | Table | N/A | `attribute_not_exists(PK)` | 1 WCU (item < 1 KB) | On `ConditionalCheckFailedException` -- retry with new code (max 3 retries, then extend code length by 1 char) |
| L2 | Get link by code (redirect) | GetItem | `LINK#{code}` | `META` | Table | `original_url, password_hash, expires_at, max_clicks, click_count, is_active, utm_source, utm_medium, utm_campaign` | -- | 0.5 RCU (eventually consistent, item ~300 bytes < 4 KB) | Hot path. Redis cache checked first (`link:{code}`, TTL 300s). Cache miss falls through to DynamoDB. |
| L3 | Get link detail (API) | GetItem | `LINK#{code}` | `META` | Table | ALL | -- | 0.5 RCU (eventually consistent) | Returns all attributes for the link detail API endpoint. |
| L4 | Update link | UpdateItem | `LINK#{code}` | `META` | Table | N/A | `attribute_exists(PK) AND owner_id = :caller_id` | 1 WCU | SET expressions for mutable fields: `title`, `original_url`, `expires_at`, `max_clicks`, `password_hash`, `utm_*`, `updated_at`. Owner check prevents unauthorized edits. |
| L5 | Delete link (soft-delete) | UpdateItem | `LINK#{code}` | `META` | Table | N/A | `attribute_exists(PK) AND owner_id = :caller_id` | 1 WCU | `SET is_active = :false, updated_at = :now`. Idempotent -- calling on already-deactivated link succeeds. |
| L6 | Delete link (hard delete) | DeleteItem | `LINK#{code}` | `META` | Table | N/A | `attribute_exists(PK) AND owner_id = :caller_id` | 1 WCU | Used only for anonymous links (TTL-expired cleanup) or admin operations. |
| L7 | List user links (paginated) | Query | `owner_id = :uid` | `created_at` (ScanIndexForward=false for newest-first) | `owner_id-created_at-index` | ALL | FilterExpression: `is_active = :active` (optional) | Variable: 0.5 RCU per 4 KB read (eventually consistent) | Paginated via `ExclusiveStartKey` / `Limit`. Page size: 20 items. Max ~6 KB per page = 1 RCU. |
| L8 | Increment click_count (atomic) | UpdateItem | `LINK#{code}` | `META` | Table | N/A | `is_active = :true AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)` | 1 WCU | `SET click_count = click_count + :one, updated_at = :now`. On `ConditionalCheckFailedException` -- link is inactive or click limit reached, return HTTP 410. |

### Hot Partition Analysis -- `links`

- **PK `LINK#{code}`**: Codes are randomly generated Base62 (7-8 chars). Distribution is inherently uniform across partitions. **Low risk.**
- **GSI `owner_id`**: A single power user with thousands of links could create a hot GSI partition. **Mitigated** by `total_link_quota` cap per plan (free: 500, pro: 10,000). Enterprise users with higher limits should be monitored.

---

## Table: `clicks`

| # | Operation | DDB Op | PK | SK / SK Condition | Index | Projection | Condition Expression | RCU/WCU | Notes |
|---|-----------|--------|-----|-------------------|-------|------------|---------------------|---------|-------|
| C1 | Record click event (single) | PutItem | `LINK#{code}` | `CLICK#{unix_ts}#{uuid}` | Table | N/A | -- | 1 WCU (item ~240 bytes < 1 KB) | Used when SQS delivers a single message. UUID in SK guarantees uniqueness. |
| C2 | Batch write clicks | BatchWriteItem | `LINK#{code}` | `CLICK#{unix_ts}#{uuid}` | Table | N/A | -- | 1 WCU per item (max 25 items per batch = 25 WCU per batch call) | SQS worker processes up to 25 click events per batch. Partial failures returned in `UnprocessedItems` -- must retry with exponential backoff. **BatchWriteItem does not support ConditionExpression.** |
| C3 | Get stats aggregate (time range) | Query | `PK = :link_pk` | `created_at BETWEEN :start AND :end` | `code-date-index` | `country, device_type, referer_domain, ip_hash` (INCLUDE projection) | -- | ~0.5 RCU per 4 KB (eventually consistent). 90-day query for a link with 10K clicks: ~240 bytes x 10K = 2.4 MB = ~300 RCU | Client-side aggregation. 90-day TTL on click items naturally bounds the result set. |
| C4 | Get stats by date (timeline) | Query | `PK = :link_pk` | `created_at BETWEEN :start AND :end` | `code-date-index` | `created_at` | -- | Lower than C3 (fewer projected bytes) | Application groups by day/week/month for time-series chart. |
| C5 | Get geo breakdown | Query | `PK = :link_pk` | `created_at BETWEEN :start AND :end` | `code-date-index` | `country` | -- | Same index as C3 | Application aggregates `country` counts client-side. |
| C6 | Get referrer stats | Query | `PK = :link_pk` | `created_at BETWEEN :start AND :end` | `code-date-index` | `referer_domain` | FilterExpression: `attribute_exists(referer_domain)` | Same index as C3, filter reduces returned items | Filters out clicks with no referrer. |
| C7 | Get click timeline (raw, via table) | Query | `PK = LINK#{code}` | `begins_with(SK, "CLICK#")` | Table | ALL | -- | 0.5 RCU per 4 KB | Alternative to GSI query; useful when `user_agent_hash` is needed. SK prefix enables `begins_with` without GSI. |
| C8 | Get clicks for specific date | Query | `PK = LINK#{code}` | `begins_with(SK, "CLICK#1712300")` | Table | ALL | -- | 0.5 RCU per 4 KB | The ISO-date prefix in SK enables begins_with for date filtering without GSI. |

### Hot Partition Analysis -- `clicks`

- **PK `LINK#{code}`**: A viral link could receive millions of clicks, creating a hot partition. **Mitigated** by:
  1. Write path is through SQS FIFO worker with controlled concurrency (not direct from redirect Lambda).
  2. DynamoDB adaptive capacity automatically isolates hot items.
  3. 90-day TTL ensures old data is automatically cleaned up.
  4. For extreme cases (> 1,000 WCU/s on a single partition), consider sharding: `LINK#{code}#SHARD#{n}` with scatter-gather reads.

---

## Table: `users`

| # | Operation | DDB Op | PK | SK / SK Condition | Index | Projection | Condition Expression | RCU/WCU | Notes |
|---|-----------|--------|-----|-------------------|-------|------------|---------------------|---------|-------|
| U1 | Get user profile | GetItem | `USER#{cognito_sub}` | `PROFILE` | Table | ALL | -- | 0.5 RCU (eventually consistent, item ~150 bytes) | Called on authenticated API requests to check plan + quotas. |
| U2 | Create user (first login) | PutItem | `USER#{cognito_sub}` | `PROFILE` | Table | N/A | `attribute_not_exists(PK)` | 1 WCU | Idempotent creation on first Cognito login. Sets default quotas based on plan. |
| U3 | Update user quota counter (daily) | UpdateItem | `USER#{cognito_sub}` | `PROFILE` | Table | N/A | `links_created_today < daily_link_quota AND last_reset_date = :today` | 1 WCU | `SET links_created_today = links_created_today + :one, total_active_links = total_active_links + :one`. On `ConditionalCheckFailedException` -- daily quota exceeded (return 429) OR date mismatch (reset counter first, then retry). |
| U4 | Reset daily counter (lazy) | UpdateItem | `USER#{cognito_sub}` | `PROFILE` | Table | N/A | `last_reset_date <> :today` | 1 WCU | `SET links_created_today = :zero, last_reset_date = :today`. No-op if already reset today (condition fails silently). This is a lazy reset -- triggered when the user's next link creation detects a date change. |
| U5 | Check daily quota | GetItem | `USER#{cognito_sub}` | `PROFILE` | Table | `links_created_today, daily_link_quota, total_active_links, total_link_quota, last_reset_date` | -- | 0.5 RCU | Read-only check. Used for dashboard quota display. Actual enforcement is via U3 condition expression. |
| U6 | Decrement active links (on delete) | UpdateItem | `USER#{cognito_sub}` | `PROFILE` | Table | N/A | `attribute_exists(PK)` | 1 WCU | `SET total_active_links = total_active_links - :one`. Keeps the running count accurate when links are deleted. |

### Hot Partition Analysis -- `users`

- **PK `USER#{cognito_sub}`**: UUIDs are uniformly distributed. **Low risk.** User table has very low throughput.

---

## Cross-Table Operations

| # | Operation | Tables Involved | Pattern | Notes |
|---|-----------|----------------|---------|-------|
| X1 | Create link + decrement quota | `links` + `users` | TransactWriteItems | Atomic: PutItem on `links` (with collision check) + UpdateItem on `users` (increment `links_created_today` + `total_active_links` with quota check). Prevents quota bypass from concurrent requests. 2 WCU per transaction (2x cost). |
| X2 | Full redirect flow | `links` (via cache) + `clicks` (async) | GetItem + async SQS publish | Cache-first read on `links`, then async SQS message for click recording. No cross-table transaction needed -- click recording is eventually consistent by design. |
| X3 | Delete link + update user counter | `links` + `users` | TransactWriteItems | Atomic: UpdateItem on `links` (set `is_active = false`) + UpdateItem on `users` (decrement `total_active_links`). |

---

## Scan Verification

**Result: NO access pattern requires a full table Scan.** All operations use:
- GetItem (O(1) by PK+SK)
- Query (bounded by partition key + sort key condition)
- PutItem / UpdateItem / DeleteItem (by PK+SK)
- BatchWriteItem (individual PutItem operations)
- TransactWriteItems (individual Put/Update operations)

The only potential exception is admin user management (listing all users), which is documented as post-MVP and would require either a GSI or an acceptable Scan on the small `users` table.

---

## Capacity Unit Summary

### Per-Operation Cost

| Operation | Type | CU per call | Expected RPS (steady state) | Total CU/s |
|-----------|------|-------------|----------------------------|------------|
| Redirect lookup (cache miss) | Read | 0.5 RCU | ~2,000 (20% cache miss of 10K RPS) | 1,000 RCU |
| Link creation | Write | 2 WCU (TransactWriteItems with user quota) | ~50 | 100 WCU |
| Click count increment | Write | 1 WCU | ~400 (batched via worker) | 400 WCU |
| Click event write | Write | 25 WCU per batch | ~400 batches/s | 10,000 WCU |
| Stats query (90-day) | Read | ~300 RCU | ~5 | 1,500 RCU |
| User profile read | Read | 0.5 RCU | ~50 | 25 RCU |
| Quota update | Write | 1 WCU | ~50 | 50 WCU |

### Table-Level Totals (Steady State, 10K RPS redirects)

| Table | Read CU/s | Write CU/s | Recommended Mode |
|-------|-----------|------------|-----------------|
| `links` | ~1,025 RCU | ~550 WCU | On-demand (bursty traffic from marketing campaigns) |
| `clicks` | ~1,500 RCU | ~10,000 WCU | Provisioned with auto-scaling (predictable write volume) |
| `users` | ~25 RCU | ~100 WCU | On-demand (very low volume) |
