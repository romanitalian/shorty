# deploy/terraform/modules/api_gateway/main.tf

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

locals {
  common_tags = merge({
    project     = var.project
    environment = var.environment
    managed_by  = "terraform"
  }, var.tags)

  api_name = "${var.project}-${var.environment}-api"
}

# =============================================================================
# HTTP API
# =============================================================================

resource "aws_apigatewayv2_api" "this" {
  name          = local.api_name
  protocol_type = "HTTP"
  description   = "Shorty URL shortener HTTP API (${var.environment})"

  cors_configuration {
    allow_origins     = var.cors_allow_origins
    allow_methods     = ["GET", "POST", "PATCH", "DELETE", "OPTIONS"]
    allow_headers     = ["Authorization", "Content-Type", "X-CSRF-Token"]
    expose_headers    = ["X-Request-Id", "X-RateLimit-Remaining"]
    max_age           = var.cors_max_age
    allow_credentials = true
  }

  tags = local.common_tags
}

# =============================================================================
# Lambda Integrations
# =============================================================================

resource "aws_apigatewayv2_integration" "redirect" {
  api_id                 = aws_apigatewayv2_api.this.id
  integration_type       = "AWS_PROXY"
  integration_uri        = var.redirect_lambda_invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_integration" "api" {
  api_id                 = aws_apigatewayv2_api.this.id
  integration_type       = "AWS_PROXY"
  integration_uri        = var.api_lambda_invoke_arn
  payload_format_version = "2.0"
}

# =============================================================================
# JWT Authorizer (Cognito)
# =============================================================================

resource "aws_apigatewayv2_authorizer" "cognito" {
  api_id           = aws_apigatewayv2_api.this.id
  authorizer_type  = "JWT"
  identity_sources = ["$request.header.Authorization"]
  name             = "cognito-jwt"

  jwt_configuration {
    audience = [var.cognito_client_id]
    issuer   = "https://cognito-idp.${var.region}.amazonaws.com/${var.cognito_user_pool_id}"
  }
}

# =============================================================================
# Public Routes (no authorization)
# =============================================================================

resource "aws_apigatewayv2_route" "redirect" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "GET /{code}"
  target    = "integrations/${aws_apigatewayv2_integration.redirect.id}"
}

resource "aws_apigatewayv2_route" "password_form" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "POST /p/{code}"
  target    = "integrations/${aws_apigatewayv2_integration.redirect.id}"
}

resource "aws_apigatewayv2_route" "guest_shorten" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "POST /api/v1/shorten"
  target    = "integrations/${aws_apigatewayv2_integration.api.id}"
}

# =============================================================================
# Authenticated Routes (JWT authorizer required)
# =============================================================================

resource "aws_apigatewayv2_route" "create_link" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "POST /api/v1/links"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "list_links" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "GET /api/v1/links"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "get_link" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "GET /api/v1/links/{code}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "update_link" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "PATCH /api/v1/links/{code}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "delete_link" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "DELETE /api/v1/links/{code}"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "link_stats" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "GET /api/v1/links/{code}/stats"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "link_stats_timeline" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "GET /api/v1/links/{code}/stats/timeline"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "link_stats_geo" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "GET /api/v1/links/{code}/stats/geo"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "link_stats_referrers" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "GET /api/v1/links/{code}/stats/referrers"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

resource "aws_apigatewayv2_route" "user_profile" {
  api_id             = aws_apigatewayv2_api.this.id
  route_key          = "GET /api/v1/me"
  target             = "integrations/${aws_apigatewayv2_integration.api.id}"
  authorization_type = "JWT"
  authorizer_id      = aws_apigatewayv2_authorizer.cognito.id
  authorization_scopes = ["openid"]
}

# =============================================================================
# Stage with Access Logging and Throttling
# =============================================================================

resource "aws_cloudwatch_log_group" "access_logs" {
  name              = "/aws/apigateway/${local.api_name}"
  retention_in_days = var.access_log_retention_days

  tags = local.common_tags
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.this.id
  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.access_logs.arn
    format = jsonencode({
      requestId          = "$context.requestId"
      ip                 = "$context.identity.sourceIp"
      caller             = "$context.identity.caller"
      user               = "$context.identity.user"
      requestTime        = "$context.requestTime"
      httpMethod         = "$context.httpMethod"
      routeKey           = "$context.routeKey"
      status             = "$context.status"
      protocol           = "$context.protocol"
      responseLength     = "$context.responseLength"
      integrationLatency = "$context.integrationLatency"
      errorMessage       = "$context.error.message"
      authorizerError    = "$context.authorizer.error"
    })
  }

  default_route_settings {
    throttling_burst_limit = var.default_throttle_burst_limit
    throttling_rate_limit  = var.default_throttle_rate_limit
  }

  # Per-route throttling
  route_settings {
    route_key              = "GET /{code}"
    throttling_burst_limit = 5000
    throttling_rate_limit  = 2000
  }

  route_settings {
    route_key              = "POST /api/v1/shorten"
    throttling_burst_limit = 100
    throttling_rate_limit  = 50
  }

  route_settings {
    route_key              = "POST /api/v1/links"
    throttling_burst_limit = 500
    throttling_rate_limit  = 200
  }

  route_settings {
    route_key              = "GET /api/v1/links"
    throttling_burst_limit = 500
    throttling_rate_limit  = 200
  }

  route_settings {
    route_key              = "GET /api/v1/links/{code}/stats"
    throttling_burst_limit = 200
    throttling_rate_limit  = 100
  }

  tags = local.common_tags
}

# =============================================================================
# Lambda Permissions for API Gateway
# =============================================================================

resource "aws_lambda_permission" "redirect" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = var.redirect_function_name
  qualifier     = "live"
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.this.execution_arn}/*/*"
}

resource "aws_lambda_permission" "api" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = var.api_function_name
  qualifier     = "live"
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.this.execution_arn}/*/*"
}

# =============================================================================
# Custom Domain (optional)
# =============================================================================

resource "aws_apigatewayv2_domain_name" "this" {
  count       = var.enable_custom_domain ? 1 : 0
  domain_name = var.domain_name

  domain_name_configuration {
    certificate_arn = var.acm_certificate_arn
    endpoint_type   = "REGIONAL"
    security_policy = "TLS_1_2"
  }

  tags = local.common_tags
}

resource "aws_apigatewayv2_api_mapping" "this" {
  count       = var.enable_custom_domain ? 1 : 0
  api_id      = aws_apigatewayv2_api.this.id
  domain_name = aws_apigatewayv2_domain_name.this[0].id
  stage       = aws_apigatewayv2_stage.default.id
}

resource "aws_route53_record" "api" {
  count   = var.enable_custom_domain && var.hosted_zone_id != "" ? 1 : 0
  zone_id = var.hosted_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_apigatewayv2_domain_name.this[0].domain_name_configuration[0].target_domain_name
    zone_id                = aws_apigatewayv2_domain_name.this[0].domain_name_configuration[0].hosted_zone_id
    evaluate_target_health = false
  }
}
