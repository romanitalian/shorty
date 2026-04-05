# RFC-0001: Core URL Shortener Architecture

| Field | Value |
|---|---|
| **Status** | Accepted |
| **Author** | @romanitalian |
| **Created** | 2026-04-05 |
| **Updated** | 2026-04-05 |
| **Issue** | — (bootstrapped from requirements-init.md) |

---

## Summary

Shorty is a high-performance URL shortener built on AWS serverless infrastructure.
This RFC establishes the core architecture decisions that all other work builds upon.

---

## Motivation

URL shorteners are a well-understood problem space, but building one that is:
- Reliably fast (p99 redirect < 100 ms globally)
- Abuse-resistant (quotas, rate limiting, bot protection)
- Operationally simple (serverless, no servers to manage)
- Privacy-respecting (no raw IP storage)

...requires deliberate architectural choices documented here.

---

## Detailed Design

### Request Flow

```
Visitor → CloudFront → WAF → API Gateway v2 → Lambda (redirect)
                                                    ├── Redis GET (cache hit → 302)
                                                    ├── DynamoDB GetItem (cache miss)
                                                    ├── goroutine: SQS SendMessage (click event)
                                                    └── HTTP 302
```

### Three Lambda Functions

| Function | Trigger | Responsibility |
|---|---|---|
| `redirect` | API GW GET /{code} | Hot path. Cache-first. Never blocks on click recording. |
| `api` | API GW /api/v1/* | CRUD for links, stats, user profile. JWT-protected. |
| `worker` | SQS FIFO | Batches click events into DynamoDB clicks table. |

### Code Generation

Short codes are Base62 (0-9a-zA-Z), 7 characters by default (3.5 trillion combinations).
Collision handling: PutItem with `attribute_not_exists(PK)` condition; retry with +1 length
after 3 consecutive collisions.

### Expiry Mechanisms

Two independent mechanisms can deactivate a link:
1. **Time-based**: DynamoDB TTL on `expires_at` attribute. Automatic, no Lambda required.
2. **Click-based**: `max_clicks` attribute. Enforced via `ConditionalExpression` on UpdateItem.

### Rate Limiting

Redis sliding window (Lua script, atomic). Three independent limit scopes:
- Per-IP redirect (200 req/min)
- Per-IP link creation (5/hour anonymous, 50/day free)
- Per-user quota (50 links/day free, 500 total)

Rate limit check occurs **before** any DynamoDB operation in every handler.

---

## Alternatives Considered

| Alternative | Why rejected |
|---|---|
| Aurora Serverless (PostgreSQL) | Cold start + connection overhead incompatible with p99 < 100ms target |
| Self-managed Redis on EC2 | Operational burden contradicts serverless philosophy |
| Pre-generating short codes | Wasted storage; collision probability acceptable with retry |
| CloudFront caching redirects (301) | Prevents click counting; deferred to post-MVP feature flag |

---

## Drawbacks

- DynamoDB single-table design has a learning curve for contributors unfamiliar with the pattern.
- Lambda in VPC adds ~300ms cold start; mitigated by provisioned concurrency on redirect Lambda.
- Stats are computed on read (no pre-aggregation) — expensive for links with > 100K clicks. Acceptable for MVP; pre-aggregation via DynamoDB Streams is the v2 solution.

---

## Open Questions

- [x] SnapStart for Go? → Not available for Go/ARM64. Provisioned concurrency instead. (Resolved)
- [x] 301 vs 302? → 302 default. 301 as opt-in per-link (post-MVP). (Resolved)

---

## Implementation Plan

All implementation tracked in GitHub Issues under Milestones Sprint 0–6.
See `deploy/scripts/github-setup.sh` for the full issue list.

---

## References

- `requirements-init.md` — full functional and non-functional requirements
- `docs/adr/` — individual architectural decision records
- `docs/api/openapi.yaml` — API contract (source of truth)
