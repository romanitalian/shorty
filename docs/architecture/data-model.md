# Shorty Data Model

## Overview

Shorty uses a **multi-table** DynamoDB design with three tables (`links`, `clicks`, `users`) plus ElastiCache Redis for caching and rate limiting. This document specifies every attribute, index, access pattern, and Redis data structure.

### Design Rationale

We use separate tables (not single-table) for the following reasons:

1. **Independent scaling** -- `clicks` has 10-100x higher write throughput than `links`. Separate tables allow independent WCU/RCU provisioning.
2. **Different TTL policies** -- `clicks` rows expire after 90 days; `links` rows expire per-link (or never). DynamoDB TTL is per-table.
3. **Blast radius isolation** -- a hot partition in `clicks` cannot throttle `links` reads on the redirect hot path.
4. **Simpler IAM** -- the `worker` Lambda gets write access to `clicks` only, never to `links` PutItem.

Trade-offs: cross-table joins are impossible (acceptable -- no join needed in any access pattern), and provisioned capacity must be managed per table.

---

## Table: `links`

Primary store for all shortened link metadata.

### Key Schema

| Key | Attribute | Type | Example |
|-----|-----------|------|---------|
| Partition Key | `PK` | String | `LINK#abc1234` |
| Sort Key | `SK` | String | `META` |

### Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `PK` | S | Yes | `LINK#{code}` |
| `SK` | S | Yes | Always `META` |
| `owner_id` | S | Yes | `USER#{cognito_sub}` or `ANON#{ip_hash}` |
| `original_url` | S | Yes | Target URL (max 2,048 chars) |
| `code` | S | Yes | Short code (Base62, 7-8 chars) |
| `title` | S | No | Human-readable label |
| `password_hash` | S | No | bcrypt hash of link password |
| `expires_at` | N | No | Unix timestamp; DynamoDB TTL attribute |
| `max_clicks` | N | No | Click limit (link deactivates when reached) |
| `click_count` | N | Yes | Atomic counter, default 0 |
| `is_active` | BOOL | Yes | Soft-delete / deactivation flag |
| `utm_source` | S | No | UTM source tag |
| `utm_medium` | S | No | UTM medium tag |
| `utm_campaign` | S | No | UTM campaign tag |
| `created_at` | N | Yes | Unix timestamp |
| `updated_at` | N | Yes | Unix timestamp |

### GSI: `owner_id-created_at-index`

Powers the user dashboard "list my links" query.

| Property | Value |
|----------|-------|
| Partition Key | `owner_id` (S) |
| Sort Key | `created_at` (N) |
| Projection | **ALL** |
| Justification | Dashboard needs full link details (title, URL, click_count, is_active, expires_at) for display. KEYS_ONLY or INCLUDE would require a follow-up GetItem for each link, defeating the purpose. |

### Item Size Estimate

| Field | Typical Size |
|-------|-------------|
| PK | 14 bytes (`LINK#` + 7 char code) |
| SK | 4 bytes (`META`) |
| owner_id | 45 bytes (`USER#` + UUID) |
| original_url | 100 bytes (avg) |
| code | 7 bytes |
| title | 30 bytes (avg, when present) |
| password_hash | 60 bytes (when present) |
| Numeric fields (5x) | 40 bytes |
| is_active | 1 byte |
| UTM fields | 60 bytes (when present) |
| **Total (typical)** | **~300 bytes** |
| **Total (max, with all optional)** | **~2,500 bytes** |

---

## Table: `clicks`

Stores individual click events. High write volume, 90-day TTL.

### Key Schema

| Key | Attribute | Type | Example |
|-----|-----------|------|---------|
| Partition Key | `PK` | String | `LINK#abc1234` |
| Sort Key | `SK` | String | `CLICK#1712300000#550e8400-e29b-41d4-a716-446655440000` |

The SK format `CLICK#{timestamp}#{uuid}` ensures:
- Chronological ordering within a partition (range queries by time)
- Uniqueness (UUID suffix prevents collisions for same-millisecond clicks)

### Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `PK` | S | Yes | `LINK#{code}` |
| `SK` | S | Yes | `CLICK#{unix_ts}#{uuid}` |
| `ip_hash` | S | Yes | SHA-256(IP + secret_salt) -- never raw IP |
| `country` | S | Yes | ISO 3166-1 alpha-2 (e.g., `US`, `DE`) or `XX` if unknown |
| `device_type` | S | Yes | `desktop`, `mobile`, `tablet`, or `bot` |
| `referer_domain` | S | No | Extracted domain from Referer header |
| `user_agent_hash` | S | Yes | SHA-256 of raw User-Agent string |
| `created_at` | N | Yes | Unix timestamp; **DynamoDB TTL attribute** (90 days) |

### GSI: `code-date-index`

Powers daily/weekly/monthly aggregation for stats endpoints.

| Property | Value |
|----------|-------|
| Partition Key | `PK` (S) -- reuses the table PK |
| Sort Key | `created_at` (N) |
| Projection | **INCLUDE** [`country`, `device_type`, `referer_domain`, `ip_hash`] |
| Justification | Stats queries need these dimensions for aggregation but not `user_agent_hash` or the full SK. INCLUDE saves ~40 bytes/item vs ALL while covering all stats query needs. |

### Item Size Estimate

| Field | Typical Size |
|-------|-------------|
| PK | 14 bytes |
| SK | 55 bytes (`CLICK#` + 10-digit ts + `#` + 36-char UUID) |
| ip_hash | 64 bytes (SHA-256 hex) |
| country | 2 bytes |
| device_type | 7 bytes (avg) |
| referer_domain | 25 bytes (avg, when present) |
| user_agent_hash | 64 bytes |
| created_at | 8 bytes |
| **Total (typical)** | **~240 bytes** |

---

## Table: `users`

Stores user profiles and quota configuration. Low volume.

### Key Schema

| Key | Attribute | Type | Example |
|-----|-----------|------|---------|
| Partition Key | `PK` | String | `USER#a1b2c3d4-e5f6-7890-abcd-ef1234567890` |
| Sort Key | `SK` | String | `PROFILE` |

### Attributes

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `PK` | S | Yes | `USER#{cognito_sub}` |
| `SK` | S | Yes | Always `PROFILE` |
| `email` | S | Yes | User email |
| `display_name` | S | No | Display name |
| `plan` | S | Yes | `free`, `pro`, or `enterprise` |
| `daily_link_quota` | N | Yes | Default: 50 (free) |
| `total_link_quota` | N | Yes | Default: 500 (free) |
| `links_created_today` | N | Yes | Daily counter, reset at midnight UTC |
| `total_active_links` | N | Yes | Running count of active links |
| `last_reset_date` | S | Yes | `YYYY-MM-DD` date of last daily counter reset |
| `created_at` | N | Yes | Unix timestamp |

### GSI: None

User lookups are always by `PK` (cognito_sub is known from the JWT). No GSI needed.

### Item Size Estimate

| Field | Typical Size |
|-------|-------------|
| PK | 42 bytes |
| SK | 7 bytes |
| email | 30 bytes (avg) |
| plan | 4 bytes |
| Numeric fields (5x) | 40 bytes |
| last_reset_date | 10 bytes |
| **Total (typical)** | **~150 bytes** |

---

## Access Patterns Matrix

### Table: `links`

| # | Operation | Table | Key Condition | Filter / Condition Expression | Index | Projection |
|---|-----------|-------|---------------|-------------------------------|-------|------------|
| L1 | **Create link** | links | PK=`LINK#{code}`, SK=`META` | `attribute_not_exists(PK)` (collision check) | Table | N/A (PutItem) |
| L2 | **Get link (redirect)** | links | PK=`LINK#{code}`, SK=`META` | -- | Table | `original_url, password_hash, expires_at, max_clicks, click_count, is_active, utm_source, utm_medium, utm_campaign` |
| L3 | **Get link detail** | links | PK=`LINK#{code}`, SK=`META` | -- | Table | ALL |
| L4 | **Update link** | links | PK=`LINK#{code}`, SK=`META` | `attribute_exists(PK) AND owner_id = :caller_id` | Table | N/A (UpdateItem) |
| L5 | **Delete link** (soft) | links | PK=`LINK#{code}`, SK=`META` | `attribute_exists(PK) AND owner_id = :caller_id` | Table | N/A (UpdateItem: `is_active=false`) |
| L6 | **List user links** | links | `owner_id = :uid` | `is_active = :active` (optional filter) | `owner_id-created_at-index` | ALL |
| L7 | **Increment click_count** | links | PK=`LINK#{code}`, SK=`META` | `is_active = :true AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)` | Table | N/A (UpdateItem: `SET click_count = click_count + :one`) |

#### Key DynamoDB Expressions

**L1 -- Create (collision-safe PutItem):**
```
PutItem
  TableName: links
  Item: { PK: "LINK#abc1234", SK: "META", ... }
  ConditionExpression: "attribute_not_exists(PK)"
```
On `ConditionalCheckFailedException` -- retry with a new code (up to 3 retries, then extend code length by 1).

**L7 -- Click count increment (enforces max_clicks):**
```
UpdateItem
  TableName: links
  Key: { PK: "LINK#abc1234", SK: "META" }
  UpdateExpression: "SET click_count = click_count + :one, updated_at = :now"
  ConditionExpression: "is_active = :true AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)"
  ExpressionAttributeValues: { ":one": 1, ":true": true, ":now": 1712300000 }
```
On `ConditionalCheckFailedException` -- the link is either inactive or has reached its click limit. Return HTTP 410.

### Table: `clicks`

| # | Operation | Table | Key Condition | Filter | Index | Projection |
|---|-----------|-------|---------------|--------|-------|------------|
| C1 | **Record click** (batch) | clicks | PK=`LINK#{code}`, SK=`CLICK#{ts}#{uuid}` | -- | Table | N/A (BatchWriteItem) |
| C2 | **Get stats (time range)** | clicks | PK=`LINK#{code}`, `created_at BETWEEN :start AND :end` | -- | `code-date-index` | `country, device_type, referer_domain, ip_hash` |
| C3 | **Get timeline** | clicks | PK=`LINK#{code}`, `created_at BETWEEN :start AND :end` | -- | `code-date-index` | `created_at` (count by day) |
| C4 | **Get geo stats** | clicks | PK=`LINK#{code}`, `created_at BETWEEN :start AND :end` | -- | `code-date-index` | `country` |
| C5 | **Get referrer stats** | clicks | PK=`LINK#{code}`, `created_at BETWEEN :start AND :end` | `attribute_exists(referer_domain)` | `code-date-index` | `referer_domain` |

### Table: `users`

| # | Operation | Table | Key Condition | Filter / Condition | Index | Projection |
|---|-----------|-------|---------------|-------------------|-------|------------|
| U1 | **Get user profile** | users | PK=`USER#{sub}`, SK=`PROFILE` | -- | Table | ALL |
| U2 | **Create user** (first login) | users | PK=`USER#{sub}`, SK=`PROFILE` | `attribute_not_exists(PK)` | Table | N/A (PutItem) |
| U3 | **Increment daily counter** | users | PK=`USER#{sub}`, SK=`PROFILE` | `links_created_today < daily_link_quota` | Table | N/A (UpdateItem: `SET links_created_today = links_created_today + :one`) |
| U4 | **Reset daily counter** | users | PK=`USER#{sub}`, SK=`PROFILE` | `last_reset_date <> :today` | Table | N/A (UpdateItem: `SET links_created_today = :zero, last_reset_date = :today`) |

---

## Redis Data Structures

### Cache Keys

| Key Pattern | Type | TTL | Description |
|-------------|------|-----|-------------|
| `link:{code}` | STRING (JSON) | 300s (5 min) | Cached link record for redirect. Contains: `original_url`, `password_hash` (presence flag only), `expires_at`, `max_clicks`, `click_count`, `is_active`, `utm_*`. |
| `link:{code}:miss` | STRING (`1`) | 60s | Negative cache -- prevents repeated DynamoDB lookups for non-existent codes. |

### Rate Limiter Keys

| Key Pattern | Type | TTL | Description |
|-------------|------|-----|-------------|
| `rl:redir:{ip_hash}` | SORTED SET | 60s | Sliding window (1 min) for redirect rate limit. Members are request timestamps (score = timestamp). Limit: 200/min. |
| `rl:create:{ip_hash}` | SORTED SET | 3600s | Sliding window (1 hour) for anonymous link creation. Limit: 5/hour. |
| `rl:pwd:{code}:{ip_hash}` | SORTED SET | 900s | Sliding window (15 min) for password attempts. Limit: 5/15min. |

### Rate Limiter Lua Script

All rate limit checks use a single atomic Lua script that:
1. `ZREMRANGEBYSCORE` -- remove entries outside the window
2. `ZCARD` -- count remaining entries
3. If under limit: `ZADD` the current timestamp, `EXPIRE` the key
4. Return: `[allowed (0/1), remaining, reset_timestamp]`

This guarantees atomicity -- no race conditions between check and increment.

---

## Capacity Estimates

### Steady State (10,000 RPS redirects)

| Table | Reads | Writes | Notes |
|-------|-------|--------|-------|
| `links` | ~2,000 RCU (assuming 80% cache hit rate) | ~10 WCU (link creation) | Eventually consistent reads, 300-byte items |
| `clicks` | ~50 RCU (stats queries) | ~10,000 WCU (via BatchWriteItem, 25 items/batch = 400 batches/s) | Worker Lambda batches, 240-byte items |
| `users` | ~10 RCU | ~10 WCU | Very low volume |

### DynamoDB Pricing Mode

- `links`: **On-demand** -- traffic is bursty (marketing campaigns), on-demand handles spikes without pre-provisioning.
- `clicks`: **Provisioned with auto-scaling** -- write volume is predictable (proportional to redirect traffic), provisioned is cheaper at sustained load.
- `users`: **On-demand** -- very low volume, on-demand avoids paying for idle capacity.
