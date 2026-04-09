# Shorty MVP (v1.0) Scope Definition

## MoSCoW Prioritization

---

## Must Have (MVP cannot ship without these)

| ID | Feature | Rationale |
|---|---|---|
| US-001 | Anonymous link creation (POST /api/v1/shorten) | Core value proposition -- users must be able to shorten URLs without registering |
| US-002 | Authenticated link creation (POST /api/v1/links) | Full-featured link creation is the primary product function |
| US-010 | Redirect (GET /{code}) with HTTP 302 | The other half of the core value -- short links must resolve |
| US-011 | Time-based TTL enforcement | Links must expire on schedule; critical for anonymous link cleanup |
| US-012 | Click-based TTL enforcement | Differentiating feature; enforced via DynamoDB conditional writes |
| US-014 | Async click event recording (SQS + worker) | Enables statistics; must be async to not block redirects |
| US-015 | 404 for nonexistent links | Basic error handling |
| US-020 | List my links (paginated, filtered) | Minimum viable dashboard |
| US-021 | View link details | Users need to see their link's config and status |
| US-030 | Aggregate statistics (total/unique clicks) | Core dashboard value -- users want to know how their links perform |
| US-040 | Email/password registration and login | Baseline auth; required before any user-specific features work |
| US-041 | Google SSO | Lowest-friction onboarding; majority of target users have Google accounts |
| US-043 | JWT token refresh | Sessions must not expire unexpectedly during active use |
| US-050 | Rate limit anonymous redirects (200/min/IP) | Service protection; without this, a single client can exhaust resources |
| US-051 | Rate limit anonymous creation (5/hour/IP) | Storage abuse prevention is non-negotiable for anonymous access |
| US-052 | Enforce user quotas (daily + total) | Fair usage enforcement for the free tier |
| US-060 | Structured JSON logging with trace IDs | Operational baseline; cannot debug production without it |
| US-071 | CSRF protection on password forms | Security baseline |
| US-080 | Terraform IaC for all AWS resources | Reproducible deployments; no manual console changes allowed |
| US-081 | CI/CD pipeline (GitHub Actions) | Automated quality gates; enables the spec-first BDD workflow |
| US-082 | Local development environment (Docker Compose) | Developers must be able to work without AWS accounts |

---

## Should Have (important, but workaroundable for launch)

| ID | Feature | Rationale |
|---|---|---|
| US-003 | Custom alias | High user demand, but auto-generated codes work fine for MVP launch |
| US-013 | Password-protected links | Differentiator, but can be deferred briefly; redirect works without it |
| US-022 | Update a link (PATCH) | Users can deactivate/delete and recreate as a workaround |
| US-023 | Delete a link (DELETE) | Users can deactivate as workaround; TTL will eventually clean up |
| US-024 | Quota usage display (GET /api/v1/me) | Users can infer quota from 429 errors, but explicit display is much better UX |
| US-031 | Click timeline (time-series) | Aggregate stats cover the basics; timeline adds depth |
| US-032 | Geographic breakdown | Valuable insight but not blocking for launch |
| US-033 | Referrer sources | Same as geo -- valuable but not blocking |
| US-053 | Rate limit password attempts | Security hardening; WAF provides baseline protection |
| US-061 | Distributed tracing (OpenTelemetry) | Structured logs provide fallback; traces make debugging faster |
| US-070 | URL validation + Safe Browsing | Critical for abuse prevention, but basic URL validation can ship first; Safe Browsing integration can follow shortly |
| US-072 | Security headers (CSP, X-Frame-Options) | Defense in depth; CloudFront can add basic headers as interim |

---

## Could Have (nice for v1, not critical)

| ID | Feature | Rationale |
|---|---|---|
| US-004 | Auto-append UTM parameters | Marketing convenience; users can add UTM to the original URL manually |
| US-062 | Grafana dashboards (5 pre-built) | Operators can query Prometheus/Loki directly; dashboards save time |

---

## Won't Have (explicitly out of scope for v1.0)

| ID | Feature | Rationale |
|---|---|---|
| US-042 | GitHub SSO | Google + email/password covers the majority; GitHub adds complexity for a small audience segment |
| US-090 | Pro/Enterprise plans + billing (Stripe) | v1.0 is free-tier only; billing integration is a major effort |
| US-091 | Bulk import (CSV) | Power-user feature; not needed for initial launch |
| US-092 | QR code generation | Nice-to-have; can be added as a lightweight post-MVP feature |
| US-093 | Developer API with API keys | Requires API key management, throttling per key, and documentation |
| US-094 | Custom domain (white-label) | Enterprise feature; requires Route53, ACM, CloudFront multi-domain |
| US-095 | A/B redirect testing | Advanced feature; adds complexity to redirect hot path |
| US-096 | Webhooks on events | Requires reliable delivery, retry logic, and subscription management |

---

## MVP Boundary Summary

### What ships in v1.0

- Full link lifecycle: create (anonymous + authenticated), redirect, expire, deactivate, delete
- Free-tier quotas enforced (50 links/day, 500 total, 30-day stats)
- Anonymous access with strict limits (5 links/day, 24h TTL)
- Email/password + Google SSO authentication
- User dashboard: list, detail, aggregate stats
- Rate limiting at all tiers (IP-based + user quotas)
- Async click recording with basic statistics
- Full observability (structured logs, metrics)
- Terraform-managed infrastructure
- CI/CD with spec-validate, BDD, integration, E2E gates
- Local dev environment with hot reload

### What does NOT ship in v1.0

- No paid plans or billing
- No GitHub SSO
- No bulk operations
- No QR codes
- No API keys for programmatic access
- No custom domains
- No A/B testing
- No webhooks
- No advanced analytics (device type breakdown as a separate endpoint)

### Rationale for boundary

The MVP delivers the complete core loop: **create a short link, redirect visitors, track clicks, manage links**. Authentication ensures user data is secure. Rate limiting and quotas prevent abuse. The free tier validates product-market fit before investing in billing and premium features. All post-MVP features are additive -- they enhance the product without changing its core architecture.
