# ADR-009: CloudFront + WAF Defense-in-Depth

## Status

Accepted

## Context

Shorty is a public-facing URL shortener. The redirect endpoint (`GET /{code}`) is
unauthenticated and high-traffic, making it an attractive target for:

- **DDoS attacks** (L3/L4 and L7 HTTP floods).
- **Bot scraping** (enumerating short codes to discover original URLs).
- **Abuse** (using the shortener as a redirect proxy for phishing/malware).
- **Credential stuffing** (against the password-protected link endpoint `/p/{code}`).

The application-level rate limiter (ADR-006) handles fine-grained per-IP and per-user
limits, but it runs inside Lambda. Malicious traffic that reaches Lambda still incurs
compute cost, even if the rate limiter returns 429.

## Decision

We deploy **CloudFront** as the edge layer and **AWS WAF** as the first line of
defense, filtering traffic before it reaches API Gateway and Lambda.

### CloudFront configuration

- **Origin:** API Gateway v2 HTTP API endpoint.
- **Cache behavior:** `CachingDisabled` for all paths (redirects must not be cached
  at the CDN -- it would bypass click counting). Post-MVP: optional 301 caching
  for links with analytics disabled.
- **TLS:** TLS 1.2+ only, `TLSv1.2_2021` security policy.
- **Custom domain:** `s.example.com` with ACM certificate.
- **Compression:** enabled for API JSON responses.
- **Origin Shield:** disabled (single-region deployment for MVP).

### WAF rules (ordered by priority)

| Priority | Rule | Action | Purpose |
|---|---|---|---|
| 1 | `AWSManagedRulesCommonRuleSet` | Block | OWASP Top 10 baseline |
| 2 | `AWSManagedRulesBotControlRuleSet` | Block/CAPTCHA | Bot detection and mitigation |
| 3 | Rate-based: 1,000 req/5min per IP | Block | Broad DDoS mitigation |
| 4 | Rate-based: 10 req/5min on `/p/{code}` | Block | Password brute-force protection |
| 5 | `AWSManagedRulesKnownBadInputsRuleSet` | Block | Log4j, path traversal, etc. |
| 6 | Geo-restriction (optional) | Block | Block traffic from specific countries |
| 7 | Custom: CAPTCHA on `/api/v1/shorten` | CAPTCHA | Anti-abuse for guest link creation |

### WAF Bot Control

AWS WAF Bot Control classifies requests into:
- **Verified bots** (Googlebot, Bingbot): allowed through to redirect (good for SEO).
- **Unverified bots**: challenged with CAPTCHA on creation endpoints, allowed on
  redirect with rate limiting.
- **Targeted bot categories** (scrapers, crawlers): blocked.

### Cost-aware design

WAF pricing is per-rule per-month plus per-request. To keep costs manageable:
- Use AWS Managed Rules (included in base WAF pricing) instead of Marketplace rules.
- Limit custom rules to the essentials listed above.
- Bot Control "common" level ($10/month), not "targeted" ($10/month + $1/million req).

### Terraform configuration

```hcl
resource "aws_wafv2_web_acl" "shorty" {
  name  = "shorty-waf"
  scope = "CLOUDFRONT"

  default_action {
    allow {}
  }

  rule {
    name     = "rate-limit-global"
    priority = 3

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 1000
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-rate-limit"
    }
  }

  # Additional rules omitted for brevity
}
```

## Consequences

**Positive:**
- Malicious traffic is blocked at the edge, before it reaches Lambda. This
  eliminates compute costs for blocked requests.
- AWS Shield Standard (included free with CloudFront) provides baseline L3/L4
  DDoS protection.
- WAF metrics and sampled requests provide visibility into attack patterns via
  CloudWatch.
- Layered defense: WAF (coarse, edge) + Redis rate limiter (fine-grained, app-level)
  provides defense-in-depth.

**Negative:**
- CloudFront + WAF add ~$15-30/month baseline cost (before traffic-based charges).
- CloudFront adds 1-5 ms latency for cache-miss requests (origin fetch). Since we
  don't cache redirects, every request is an origin fetch. The latency is offset
  by TLS termination at the edge rather than at API Gateway.
- WAF false positives: the `CommonRuleSet` may occasionally block legitimate
  requests with unusual URL patterns. Requires monitoring and rule exclusion
  tuning.
- WAF rate-based rules have a minimum granularity of 100 requests per 5 minutes,
  which is coarser than the application-level Redis rate limiter (200/min).

## Alternatives Considered

**API Gateway throttling only (no CloudFront/WAF):**
Rejected. API Gateway throttling is account-wide and does not provide per-IP
blocking, bot detection, or geo-filtering. It also doesn't protect against L3/L4
DDoS (only AWS Shield + CloudFront do).

**Cloudflare instead of CloudFront + WAF:**
Rejected. Cloudflare offers excellent DDoS protection but adds a non-AWS dependency.
Since all infrastructure is on AWS and managed via Terraform, staying within the
AWS ecosystem simplifies operations and IAM management.

**AWS Shield Advanced ($3,000/month):**
Rejected for MVP. Shield Standard (free with CloudFront) provides sufficient baseline
protection. Shield Advanced adds DDoS cost protection and 24/7 DDoS Response Team
access, which is warranted at higher traffic volumes but not for initial launch.

**No edge layer (direct API Gateway exposure):**
Rejected. API Gateway is publicly accessible by default. Without CloudFront + WAF,
there is no bot detection, no geo-filtering, and no L3/L4 DDoS mitigation. Every
request, including attack traffic, would invoke Lambda and incur cost.
