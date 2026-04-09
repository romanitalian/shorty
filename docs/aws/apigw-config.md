# API Gateway v2 (HTTP API) Configuration

Shorty uses AWS API Gateway v2 (HTTP API), not REST API (v1). HTTP API is cheaper ($1.00/million vs $3.50/million), faster (lower latency), and supports JWT authorizers natively. It lacks some REST API features (usage plans, API keys, request validation) that Shorty does not need.

---

## Route Configuration

### Public Routes (no authorization)

| Method | Route | Lambda Target | Description |
|--------|-------|---------------|-------------|
| `GET` | `/{code}` | `shorty-redirect:live` | Redirect hot path |
| `POST` | `/p/{code}` | `shorty-redirect:live` | Password-protected link form submission |
| `POST` | `/api/v1/shorten` | `shorty-api:live` | Guest link creation (IP rate-limited) |

### Authenticated Routes (JWT authorizer required)

| Method | Route | Lambda Target | Description |
|--------|-------|---------------|-------------|
| `POST` | `/api/v1/links` | `shorty-api:live` | Create link |
| `GET` | `/api/v1/links` | `shorty-api:live` | List user's links (paginated) |
| `GET` | `/api/v1/links/{code}` | `shorty-api:live` | Link details |
| `PATCH` | `/api/v1/links/{code}` | `shorty-api:live` | Update link |
| `DELETE` | `/api/v1/links/{code}` | `shorty-api:live` | Delete link (soft) |
| `GET` | `/api/v1/links/{code}/stats` | `shorty-api:live` | Aggregate statistics |
| `GET` | `/api/v1/links/{code}/stats/timeline` | `shorty-api:live` | Click time series |
| `GET` | `/api/v1/links/{code}/stats/geo` | `shorty-api:live` | Geography breakdown |
| `GET` | `/api/v1/links/{code}/stats/referrers` | `shorty-api:live` | Referrer sources |
| `GET` | `/api/v1/me` | `shorty-api:live` | User profile + quota |

All authenticated routes target the Lambda alias `live` (not `$LATEST`) to support canary deployments and provisioned concurrency.

---

## Integration Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Integration type | `AWS_PROXY` | Lambda proxy integration -- API Gateway passes the full request to Lambda and returns the Lambda response as-is. No request/response mapping templates needed. |
| Payload format version | **2.0** | v2.0 payload is smaller and simpler than v1.0. Includes `rawPath`, `rawQueryString`, `requestContext.http.method`. The Go handler uses `events.APIGatewayV2HTTPRequest`. |
| Connection type | `INTERNET` | Lambda is invoked via the AWS control plane, not via VPC link. Lambda itself runs in VPC for ElastiCache access, but the API GW -> Lambda invocation path does not traverse the VPC. |

### Payload Format Version 2.0

Key differences from 1.0 that affect handler code:

- Path parameters: `event.PathParameters["code"]` (same)
- HTTP method: `event.RequestContext.HTTP.Method` (not `event.HTTPMethod`)
- Source IP: `event.RequestContext.HTTP.SourceIP`
- Headers: lowercase keys, comma-delimited multi-value (not separate `MultiValueHeaders`)
- Cookies: `event.Cookies` (dedicated field)

### Terraform Example

```hcl
resource "aws_apigatewayv2_api" "shorty" {
  name          = "shorty-api"
  protocol_type = "HTTP"
  description   = "Shorty URL shortener HTTP API"

  cors_configuration {
    allow_origins = var.cors_allow_origins
    allow_methods = ["GET", "POST", "PATCH", "DELETE", "OPTIONS"]
    allow_headers = ["Authorization", "Content-Type", "X-CSRF-Token"]
    max_age       = 3600
  }
}

resource "aws_apigatewayv2_integration" "redirect" {
  api_id                 = aws_apigatewayv2_api.shorty.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_alias.redirect_live.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "redirect" {
  api_id    = aws_apigatewayv2_api.shorty.id
  route_key = "GET /{code}"
  target    = "integrations/${aws_apigatewayv2_integration.redirect.id}"
}

resource "aws_apigatewayv2_route" "password_form" {
  api_id    = aws_apigatewayv2_api.shorty.id
  route_key = "POST /p/{code}"
  target    = "integrations/${aws_apigatewayv2_integration.redirect.id}"
}

resource "aws_apigatewayv2_integration" "api" {
  api_id                 = aws_apigatewayv2_api.shorty.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_alias.api_live.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "api_create_link" {
  api_id             = aws_apigatewayv2_api.shorty.id
  route_key          = "POST /api/v1/links"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
}

# ... repeat for each authenticated route
```

---

## Stage Configuration

| Stage | Auto-deploy | Description |
|-------|-------------|-------------|
| `$default` | Yes | Production. All traffic routes here unless overridden. Auto-deploy means changes to routes/integrations are deployed immediately. |
| `dev` | Yes | Development environment. Separate stage variables point to dev Lambda aliases and DynamoDB tables. |

API Gateway v2 HTTP APIs use a simplified stage model compared to REST APIs. The `$default` stage is mandatory and serves as the production stage.

### Stage Variables

| Variable | `$default` (prod) | `dev` |
|----------|-------------------|-------|
| `lambda_alias` | `live` | `dev` |
| `log_level` | `info` | `debug` |
| `cors_origin` | `https://shorty.io` | `http://localhost:3000` |

### Terraform Example

```hcl
resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.shorty.id
  name        = "$default"
  auto_deploy = true

  stage_variables = {
    lambda_alias = "live"
    log_level    = "info"
  }

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.apigw_access.arn
    format = jsonencode({
      requestId      = "$context.requestId"
      ip             = "$context.identity.sourceIp"
      caller         = "$context.identity.caller"
      user           = "$context.identity.user"
      requestTime    = "$context.requestTime"
      httpMethod     = "$context.httpMethod"
      resourcePath   = "$context.resourcePath"
      routeKey       = "$context.routeKey"
      status         = "$context.status"
      protocol       = "$context.protocol"
      responseLength = "$context.responseLength"
      integrationLatency = "$context.integrationLatency"
      errorMessage   = "$context.error.message"
      authorizerError = "$context.authorizer.error"
    })
  }

  default_route_settings {
    throttling_burst_limit = 5000
    throttling_rate_limit  = 2000
  }
}
```

---

## Throttling

### Default Limits

| Setting | Value | Rationale |
|---------|-------|-----------|
| Account-level burst | 5,000 req/s | AWS default for HTTP API. Request increase if needed. |
| Account-level rate | 10,000 req/s | AWS default. |
| Stage-level burst | 5,000 req/s | Matches account limit. |
| Stage-level rate | 2,000 req/s | Steady-state limit. Provides headroom below the Lambda reserved concurrency of 1,250 total. |

### Per-Route Throttling

| Route | Burst | Rate | Rationale |
|-------|-------|------|-----------|
| `GET /{code}` | 5,000 | 2,000 | Main traffic path. Matches stage default. |
| `POST /api/v1/shorten` | 100 | 50 | Guest endpoint -- aggressive throttling to prevent abuse. Further limited by WAF and Redis rate limiter. |
| `POST /api/v1/links` | 500 | 200 | Authenticated creation. |
| `GET /api/v1/links` | 500 | 200 | Dashboard queries. |
| `GET /api/v1/links/{code}/stats/*` | 200 | 100 | Stats queries are expensive (DynamoDB scans). |
| `PATCH /api/v1/links/{code}` | 200 | 100 | Updates. |
| `DELETE /api/v1/links/{code}` | 200 | 100 | Deletes. |

### Terraform Example (per-route)

```hcl
resource "aws_apigatewayv2_route" "guest_shorten" {
  api_id    = aws_apigatewayv2_api.shorty.id
  route_key = "POST /api/v1/shorten"
  target    = "integrations/${aws_apigatewayv2_integration.api.id}"

  # Note: per-route throttling in HTTP API is configured via stage route settings
}

# Per-route throttling is set in the stage, not the route itself
resource "aws_apigatewayv2_stage" "default" {
  # ... (other settings)

  route_settings {
    route_key              = "POST /api/v1/shorten"
    throttling_burst_limit = 100
    throttling_rate_limit  = 50
  }

  route_settings {
    route_key              = "GET /{code}"
    throttling_burst_limit = 5000
    throttling_rate_limit  = 2000
  }

  route_settings {
    route_key              = "POST /api/v1/links"
    throttling_burst_limit = 500
    throttling_rate_limit  = 200
  }
}
```

---

## CORS Configuration

CORS is configured at the API Gateway level, not in Lambda code. This avoids duplicating CORS headers in every Lambda handler and ensures OPTIONS preflight requests are handled without invoking Lambda.

| Setting | Production | Development |
|---------|-----------|-------------|
| `AllowOrigins` | `https://shorty.io` | `http://localhost:3000`, `http://localhost:8080` |
| `AllowMethods` | `GET, POST, PATCH, DELETE, OPTIONS` | Same |
| `AllowHeaders` | `Authorization, Content-Type, X-CSRF-Token` | Same |
| `ExposeHeaders` | `X-Request-Id, X-RateLimit-Remaining` | Same |
| `MaxAge` | `3600` (1 hour) | `0` (no cache during dev) |
| `AllowCredentials` | `true` | `true` |

**Important:** When `AllowCredentials` is `true`, `AllowOrigins` cannot be `*`. Each origin must be explicitly listed.

---

## JWT Authorizer (Cognito Integration)

API Gateway v2 has native JWT authorizer support -- no need for a custom Lambda authorizer.

### Configuration

| Setting | Value |
|---------|-------|
| Authorizer type | `JWT` |
| Identity source | `$request.header.Authorization` |
| JWT issuer | `https://cognito-idp.{region}.amazonaws.com/{user_pool_id}` |
| JWT audience | Cognito App Client ID |

### Authorization Scopes

| Route | Required Scopes | Description |
|-------|----------------|-------------|
| `POST /api/v1/links` | `openid` | Any authenticated user can create links |
| `GET /api/v1/links` | `openid` | List own links only (owner_id filter in Lambda) |
| `GET /api/v1/links/{code}` | `openid` | View own link details (ownership check in Lambda) |
| `PATCH /api/v1/links/{code}` | `openid` | Update own link (ownership check in Lambda) |
| `DELETE /api/v1/links/{code}` | `openid` | Delete own link (ownership check in Lambda) |
| `GET /api/v1/links/{code}/stats/*` | `openid` | View own link stats (ownership check in Lambda) |
| `GET /api/v1/me` | `openid` | View own profile |

**Note:** API Gateway JWT authorizer validates the token signature and expiration but does not enforce resource-level ownership. The Lambda handler must verify that the authenticated user (`sub` from JWT claims) matches the `owner_id` of the requested resource.

### Terraform Example

```hcl
resource "aws_apigatewayv2_authorizer" "cognito" {
  api_id           = aws_apigatewayv2_api.shorty.id
  authorizer_type  = "JWT"
  identity_sources = ["$request.header.Authorization"]
  name             = "cognito-jwt"

  jwt_configuration {
    audience = [aws_cognito_user_pool_client.shorty.id]
    issuer   = "https://cognito-idp.${var.region}.amazonaws.com/${aws_cognito_user_pool.shorty.id}"
  }
}
```

---

## Custom Domain

| Setting | Value |
|---------|-------|
| Domain name | `api.shorty.io` |
| Certificate | ACM certificate in the **same region** as the API Gateway (regional endpoint) |
| Endpoint type | `REGIONAL` |
| Base path mapping | `/` (maps to `$default` stage) |
| DNS | CNAME or Route 53 alias to the API Gateway domain name |

If CloudFront is placed in front of API Gateway, the custom domain is configured on CloudFront instead, and the API Gateway uses its default `*.execute-api.{region}.amazonaws.com` domain. In this architecture:

- `shorty.io` -> CloudFront -> API Gateway (redirect routes)
- `api.shorty.io` -> CloudFront -> API Gateway (API routes)

The ACM certificate for CloudFront must be in `us-east-1` regardless of the API Gateway region.

### Terraform Example

```hcl
resource "aws_apigatewayv2_domain_name" "api" {
  domain_name = "api.shorty.io"

  domain_name_configuration {
    certificate_arn = aws_acm_certificate.api.arn
    endpoint_type   = "REGIONAL"
    security_policy = "TLS_1_2"
  }
}

resource "aws_apigatewayv2_api_mapping" "api" {
  api_id      = aws_apigatewayv2_api.shorty.id
  domain_name = aws_apigatewayv2_domain_name.api.id
  stage       = aws_apigatewayv2_stage.default.id
}

resource "aws_route53_record" "api" {
  zone_id = var.hosted_zone_id
  name    = "api.shorty.io"
  type    = "A"

  alias {
    name                   = aws_apigatewayv2_domain_name.api.domain_name_configuration[0].target_domain_name
    zone_id                = aws_apigatewayv2_domain_name.api.domain_name_configuration[0].hosted_zone_id
    evaluate_target_health = false
  }
}
```

---

## Access Logging

Access logs are written to CloudWatch Logs in JSON format for structured querying via CloudWatch Insights.

### Log Format

```json
{
  "requestId": "$context.requestId",
  "ip": "$context.identity.sourceIp",
  "caller": "$context.identity.caller",
  "user": "$context.identity.user",
  "requestTime": "$context.requestTime",
  "httpMethod": "$context.httpMethod",
  "routeKey": "$context.routeKey",
  "status": "$context.status",
  "protocol": "$context.protocol",
  "responseLength": "$context.responseLength",
  "integrationLatency": "$context.integrationLatency",
  "errorMessage": "$context.error.message",
  "authorizerError": "$context.authorizer.error"
}
```

### Log Group Configuration

| Setting | Value |
|---------|-------|
| Log group name | `/aws/apigateway/shorty-api` |
| Retention | 30 days (dev), 90 days (prod) |
| Encryption | AWS managed key (default) |

### Useful CloudWatch Insights Queries

**P99 latency by route:**
```
fields @timestamp, routeKey, integrationLatency
| stats percentile(integrationLatency, 99) as p99 by routeKey
| sort p99 desc
```

**Error rate by route:**
```
fields routeKey, status
| filter status >= 500
| stats count(*) as errors by routeKey
| sort errors desc
```

**Top IPs by request volume:**
```
fields ip
| stats count(*) as requests by ip
| sort requests desc
| limit 20
```

---

## Request/Response Transformations

No request or response transformations are used. With `AWS_PROXY` integration and payload format version 2.0, the full HTTP request is passed to Lambda and the Lambda response is returned directly. This keeps the architecture simple and avoids API Gateway-specific template languages.

If response transformation becomes necessary (e.g., adding standard headers), use Lambda response headers rather than API Gateway transformations. This keeps the logic in Go code where it can be unit-tested.

---

## API Gateway Limits and Quotas

| Limit | Value | Notes |
|-------|-------|-------|
| Payload size | 10 MB | Hard limit, cannot be increased. Document in OpenAPI spec. |
| Route timeout | 29 s | Hard limit. All Lambda timeouts must be < 29 s. |
| Routes per API | 300 | Soft limit. Shorty uses ~15 routes, well within limit. |
| Stages per API | 10 | Soft limit. Shorty uses 2 (default + dev). |
| Authorizers per API | 10 | Soft limit. Shorty uses 1 (Cognito JWT). |
| Integrations per API | 300 | Soft limit. Shorty uses 2 (redirect + api Lambda). |

---

## Monitoring and Alarms

### Key Metrics

| Metric | Namespace | Alarm Threshold | Action |
|--------|-----------|----------------|--------|
| `5XXError` | `AWS/ApiGateway` | > 1% of requests over 5 min | SNS alert |
| `4XXError` | `AWS/ApiGateway` | > 10% of requests over 5 min | SNS alert (may indicate attack) |
| `Latency` (p99) | `AWS/ApiGateway` | > 200 ms over 5 min | SNS alert |
| `Count` | `AWS/ApiGateway` | > 8,000 req/s over 1 min | SNS warning (approaching throttle limit) |
| `IntegrationLatency` (p99) | `AWS/ApiGateway` | > 150 ms over 5 min | SNS alert (Lambda is slow) |

### Terraform Example

```hcl
resource "aws_cloudwatch_metric_alarm" "apigw_5xx" {
  alarm_name          = "shorty-apigw-5xx-rate"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  threshold           = 1

  metric_query {
    id          = "error_rate"
    expression  = "errors / total * 100"
    label       = "5XX Error Rate %"
    return_data = true
  }

  metric_query {
    id = "errors"
    metric {
      metric_name = "5XXError"
      namespace   = "AWS/ApiGateway"
      period      = 300
      stat        = "Sum"
      dimensions = {
        ApiId = aws_apigatewayv2_api.shorty.id
      }
    }
  }

  metric_query {
    id = "total"
    metric {
      metric_name = "Count"
      namespace   = "AWS/ApiGateway"
      period      = 300
      stat        = "Sum"
      dimensions = {
        ApiId = aws_apigatewayv2_api.shorty.id
      }
    }
  }

  alarm_actions = [aws_sns_topic.alerts.arn]
}
```
