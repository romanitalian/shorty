# CloudFront Configuration

> AWS Specialist specification for DevOps implementation.
> Reference: ADR-009, requirements-init.md Section 7.3.

---

## 1. Distribution Overview

| Setting | Value |
|---|---|
| Origin | API Gateway v2 HTTP API endpoint |
| Price class | `PriceClass_100` (NA + EU) for MVP; `PriceClass_All` for global launch |
| HTTP version | HTTP/2 enabled |
| IPv6 | Enabled |
| Custom domain | `s.shorty.io` (short links), `api.shorty.io` (API) |
| ACM certificate | us-east-1 (required for CloudFront), covering `*.shorty.io` |
| TLS policy | `TLSv1.2_2021` (TLS 1.2+ only) |
| Compression | Enabled (gzip + brotli for JSON, HTML, CSS, JS) |
| Origin Shield | Disabled (single-region MVP) |
| Web ACL | `shorty-waf` (see `waf-config.md`) |

---

## 2. Origin Configuration

### API Gateway Origin

```
Origin domain:    {api-id}.execute-api.{region}.amazonaws.com
Origin path:      (empty)
Protocol:         HTTPS only
Minimum TLS:      TLSv1.2
Origin timeout:   30s (must exceed Lambda timeout of 10s)
Origin keepalive: 5s
```

### Custom Origin Headers

| Header | Value | Purpose |
|---|---|---|
| `X-Origin-Verify` | Secret value stored in Secrets Manager | Prevent direct API Gateway access bypassing CloudFront+WAF |

API Gateway must validate this header and reject requests without it.

### Origin Failover

Not configured for MVP (single-region). For production multi-region:

```
Primary:   API Gateway us-east-1
Secondary: API Gateway eu-west-1
Failover:  on 5xx or timeout from primary
```

---

## 3. Cache Behaviors (order matters -- most specific first)

| Priority | Path Pattern | Cache Policy | Origin Request Policy | Notes |
|---|---|---|---|---|
| 1 | `/api/*` | `CachingDisabled` | `AllViewerExceptHostHeader` | API endpoints -- never cache |
| 2 | `/p/*` | `CachingDisabled` | `AllViewerExceptHostHeader` | Password-protected links -- never cache |
| 3 | `/auth/*` | `CachingDisabled` | `AllViewerExceptHostHeader` | Auth flows -- never cache |
| 4 | `/static/*.js` | `CachingOptimized` (TTL 86400s) | `CORS-S3Origin` | Static JS assets |
| 5 | `/static/*.css` | `CachingOptimized` (TTL 86400s) | `CORS-S3Origin` | Static CSS assets |
| 6 | `/static/*.png` | `CachingOptimized` (TTL 86400s) | `CORS-S3Origin` | Static images |
| 7 | `/*` (default) | Custom redirect policy | `AllViewerExceptHostHeader` | Redirect endpoint |

### Custom Cache Policy for Redirect (`/*` default behavior)

**Do not cache redirects at CloudFront.** Every redirect must reach Lambda to count clicks.

```
TTL:     min=0, default=0, max=0
Reason:  302 redirects must increment click counters in DynamoDB
```

**Exception (post-MVP):** For links with analytics disabled or permanent (301) redirects, the Lambda can set `Cache-Control: public, max-age=86400` to allow CloudFront caching. CloudFront honors origin `Cache-Control` headers when `max=0` is not enforced.

### Cache Key Policy

- Forward to origin: `Accept-Encoding` header only
- Do NOT forward: `Cookie`, `Authorization` (these bust cache and are not needed for redirects)
- Query strings: forward all (short codes are path-based, but future features may use query params)

---

## 4. Custom Error Responses

| HTTP Status | Response Page | Error Caching TTL |
|---|---|---|
| 403 Forbidden | `/static/403.html` | 60s |
| 404 Not Found | `/static/404.html` | 60s |
| 410 Gone | `/static/410.html` | 300s (expired links) |
| 429 Too Many Requests | `/static/429.html` | 10s |
| 500 Internal Server Error | `/static/500.html` | 10s |
| 502 Bad Gateway | `/static/502.html` | 10s |
| 503 Service Unavailable | `/static/503.html` | 10s |
| 504 Gateway Timeout | `/static/504.html` | 10s |

Error pages are served from an S3 origin bucket (`shorty-static-{env}`).

---

## 5. TLS Configuration

- **Minimum protocol version:** TLS 1.2 (`TLSv1.2_2021` security policy)
- **ACM certificate:** Wildcard `*.shorty.io` in us-east-1
- **SSL support method:** SNI (Server Name Indication) -- no dedicated IP
- **HSTS:** Enforced via `Strict-Transport-Security` response header (added by CloudFront response headers policy)

### Response Headers Policy

```
Strict-Transport-Security: max-age=31536000; includeSubDomains; preload
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'
```

---

## 6. Geo-Restriction

Disabled by default. Can be enabled via WAF geo-blocking rules (see `waf-config.md`) for:

- Compliance requirements (e.g., OFAC-sanctioned countries)
- Attack mitigation (temporarily block countries generating attack traffic)

CloudFront geo-restriction (whitelist/blacklist) is an alternative but WAF-level geo rules are preferred because they integrate with WAF logging and metrics.

---

## 7. Real-Time Logs

Enable CloudFront real-time logs for operational visibility:

```
Kinesis Data Stream: shorty-cloudfront-logs
Fields:
  - timestamp
  - c-ip (client IP)
  - sc-status (HTTP status)
  - cs-uri-stem (request path)
  - time-taken (total request time)
  - x-edge-result-type (Hit/Miss/Error)
  - cs-user-agent
  - x-edge-response-result-type
Sampling rate: 10% (production), 100% (dev/staging)
```

Standard logs (S3): enabled for all environments, delivered to `s3://shorty-logs-{env}/cloudfront/`.

---

## 8. Lambda@Edge vs CloudFront Functions

### Decision: CloudFront Functions for simple tasks

| Task | Technology | Stage | Justification |
|---|---|---|---|
| Security headers | CloudFront Response Headers Policy | N/A | Native feature, no code needed |
| URL normalization (trailing slash removal) | CloudFront Functions | Viewer Request | Sub-ms execution, JS-only is sufficient |
| Cookie-to-Authorization header extraction | Lambda@Edge | Origin Request | Needs network access to validate, more complex logic |

**Cookie-to-header extraction** is needed because API Gateway v2 JWT authorizer reads from the `Authorization` header, but Shorty stores JWTs in httpOnly cookies (ADR-010). A Lambda@Edge function at the Origin Request stage extracts the access token from the cookie and sets the `Authorization: Bearer {token}` header.

```javascript
// Lambda@Edge: Origin Request - extract JWT from cookie
exports.handler = async (event) => {
  const request = event.Records[0].cf.request;
  const cookies = request.headers.cookie;

  if (cookies) {
    const cookieStr = cookies.map(c => c.value).join('; ');
    const match = cookieStr.match(/access_token=([^;]+)/);
    if (match) {
      request.headers['authorization'] = [{
        key: 'Authorization',
        value: `Bearer ${match[1]}`
      }];
    }
  }

  return request;
};
```

---

## 9. Terraform Example

```hcl
# --- ACM Certificate (must be in us-east-1 for CloudFront) ---
resource "aws_acm_certificate" "shorty" {
  provider          = aws.us_east_1
  domain_name       = "shorty.io"
  subject_alternative_names = ["*.shorty.io"]
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

# --- CloudFront Response Headers Policy ---
resource "aws_cloudfront_response_headers_policy" "security" {
  name = "shorty-security-headers"

  security_headers_config {
    strict_transport_security {
      access_control_max_age_sec = 31536000
      include_subdomains         = true
      preload                    = true
      override                   = true
    }
    content_type_options {
      override = true
    }
    frame_options {
      frame_option = "DENY"
      override     = true
    }
    xss_protection {
      mode_block = true
      protection = true
      override   = true
    }
    referrer_policy {
      referrer_policy = "strict-origin-when-cross-origin"
      override        = true
    }
  }
}

# --- CloudFront Cache Policy: No Cache (redirects, API) ---
resource "aws_cloudfront_cache_policy" "no_cache" {
  name        = "shorty-no-cache"
  min_ttl     = 0
  default_ttl = 0
  max_ttl     = 0

  parameters_in_cache_key_and_forwarded_to_origin {
    headers_config {
      header_behavior = "none"
    }
    cookies_config {
      cookie_behavior = "none"
    }
    query_strings_config {
      query_string_behavior = "all"
    }
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true
  }
}

# --- CloudFront Distribution ---
resource "aws_cloudfront_distribution" "shorty" {
  enabled             = true
  is_ipv6_enabled     = true
  http_version        = "http2and3"
  price_class         = "PriceClass_100"
  aliases             = ["s.shorty.io", "api.shorty.io"]
  web_acl_id          = aws_wafv2_web_acl.shorty.arn
  default_root_object = ""

  # --- API Gateway Origin ---
  origin {
    domain_name = replace(aws_apigatewayv2_api.shorty.api_endpoint, "https://", "")
    origin_id   = "api-gateway"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
      origin_read_timeout    = 30
      origin_keepalive_timeout = 5
    }

    custom_header {
      name  = "X-Origin-Verify"
      value = data.aws_secretsmanager_secret_version.origin_verify.secret_string
    }
  }

  # --- S3 Origin for Static Error Pages ---
  origin {
    domain_name = aws_s3_bucket.static.bucket_regional_domain_name
    origin_id   = "s3-static"

    s3_origin_config {
      origin_access_identity = aws_cloudfront_origin_access_identity.static.cloudfront_access_identity_path
    }
  }

  # --- Default Behavior: Redirect Endpoint ---
  default_cache_behavior {
    allowed_methods          = ["GET", "HEAD", "OPTIONS"]
    cached_methods           = ["GET", "HEAD"]
    target_origin_id         = "api-gateway"
    viewer_protocol_policy   = "redirect-to-https"
    compress                 = true
    cache_policy_id          = aws_cloudfront_cache_policy.no_cache.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id

    lambda_function_association {
      event_type   = "origin-request"
      lambda_arn   = aws_lambda_function.cookie_to_auth.qualified_arn
      include_body = false
    }
  }

  # --- API Behavior ---
  ordered_cache_behavior {
    path_pattern             = "/api/*"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    target_origin_id         = "api-gateway"
    viewer_protocol_policy   = "redirect-to-https"
    compress                 = true
    cache_policy_id          = aws_cloudfront_cache_policy.no_cache.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id

    lambda_function_association {
      event_type   = "origin-request"
      lambda_arn   = aws_lambda_function.cookie_to_auth.qualified_arn
      include_body = true
    }
  }

  # --- Password-protected links ---
  ordered_cache_behavior {
    path_pattern             = "/p/*"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    target_origin_id         = "api-gateway"
    viewer_protocol_policy   = "redirect-to-https"
    compress                 = true
    cache_policy_id          = aws_cloudfront_cache_policy.no_cache.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id
  }

  # --- Auth flows ---
  ordered_cache_behavior {
    path_pattern             = "/auth/*"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    target_origin_id         = "api-gateway"
    viewer_protocol_policy   = "redirect-to-https"
    compress                 = true
    cache_policy_id          = aws_cloudfront_cache_policy.no_cache.id
  }

  # --- Static assets ---
  ordered_cache_behavior {
    path_pattern             = "/static/*"
    allowed_methods          = ["GET", "HEAD", "OPTIONS"]
    cached_methods           = ["GET", "HEAD"]
    target_origin_id         = "s3-static"
    viewer_protocol_policy   = "redirect-to-https"
    compress                 = true

    forwarded_values {
      query_string = false
      cookies {
        forward = "none"
      }
    }

    min_ttl     = 86400
    default_ttl = 86400
    max_ttl     = 604800
  }

  # --- Custom Error Responses ---
  custom_error_response {
    error_code            = 403
    response_code         = 403
    response_page_path    = "/static/403.html"
    error_caching_min_ttl = 60
  }

  custom_error_response {
    error_code            = 404
    response_code         = 404
    response_page_path    = "/static/404.html"
    error_caching_min_ttl = 60
  }

  custom_error_response {
    error_code            = 500
    response_code         = 500
    response_page_path    = "/static/500.html"
    error_caching_min_ttl = 10
  }

  custom_error_response {
    error_code            = 502
    response_code         = 502
    response_page_path    = "/static/502.html"
    error_caching_min_ttl = 10
  }

  custom_error_response {
    error_code            = 503
    response_code         = 503
    response_page_path    = "/static/503.html"
    error_caching_min_ttl = 10
  }

  custom_error_response {
    error_code            = 504
    response_code         = 504
    response_page_path    = "/static/504.html"
    error_caching_min_ttl = 10
  }

  # --- TLS ---
  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate.shorty.arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  # --- Logging ---
  logging_config {
    include_cookies = false
    bucket          = aws_s3_bucket.logs.bucket_domain_name
    prefix          = "cloudfront/"
  }

  tags = {
    Project     = "shorty"
    Environment = var.environment
  }
}
```

---

## 10. Key Operational Notes

1. **ACM certificate must be in us-east-1** regardless of the API Gateway region. This is a CloudFront requirement.
2. **Origin `X-Origin-Verify` header** must be validated by API Gateway to prevent direct access that bypasses CloudFront+WAF.
3. **CloudFront adds 1-5ms latency** for origin fetches. Since redirects are not cached, every redirect is an origin fetch. This is offset by TLS termination at the edge.
4. **AWS Shield Standard** is included free with CloudFront and provides baseline L3/L4 DDoS protection.
5. **Cache invalidation** is not needed for redirects (TTL=0). For static assets, use versioned filenames (e.g., `app.abc123.js`) instead of cache invalidation.
