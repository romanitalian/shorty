# deploy/terraform/modules/cloudfront/main.tf

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

  has_certificate = var.acm_certificate_arn != ""
  has_aliases     = length(var.domain_aliases) > 0
}

# =============================================================================
# Response Headers Policy (Security Headers)
# =============================================================================

resource "aws_cloudfront_response_headers_policy" "security" {
  name = "${var.project}-${var.environment}-security-headers"

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

# =============================================================================
# Cache Policy: No Cache (redirects + API)
# =============================================================================

resource "aws_cloudfront_cache_policy" "no_cache" {
  name        = "${var.project}-${var.environment}-no-cache"
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

# =============================================================================
# Origin Request Policy
# =============================================================================

data "aws_cloudfront_origin_request_policy" "all_viewer_except_host" {
  name = "Managed-AllViewerExceptHostHeader"
}

# =============================================================================
# CloudFront Distribution
# =============================================================================

resource "aws_cloudfront_distribution" "this" {
  enabled             = true
  is_ipv6_enabled     = true
  http_version        = "http2and3"
  price_class         = var.price_class
  aliases             = local.has_aliases ? var.domain_aliases : null
  web_acl_id          = var.web_acl_arn != "" ? var.web_acl_arn : null
  default_root_object = ""

  # --- API Gateway Origin ---
  origin {
    domain_name = var.api_gateway_endpoint
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
      value = var.origin_verify_secret
    }
  }

  # --- Default Behavior: Redirect Endpoint ---
  default_cache_behavior {
    allowed_methods            = ["GET", "HEAD", "OPTIONS"]
    cached_methods             = ["GET", "HEAD"]
    target_origin_id           = "api-gateway"
    viewer_protocol_policy     = "redirect-to-https"
    compress                   = true
    cache_policy_id            = aws_cloudfront_cache_policy.no_cache.id
    origin_request_policy_id   = data.aws_cloudfront_origin_request_policy.all_viewer_except_host.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id
  }

  # --- API Behavior ---
  ordered_cache_behavior {
    path_pattern               = "/api/*"
    allowed_methods            = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods             = ["GET", "HEAD"]
    target_origin_id           = "api-gateway"
    viewer_protocol_policy     = "redirect-to-https"
    compress                   = true
    cache_policy_id            = aws_cloudfront_cache_policy.no_cache.id
    origin_request_policy_id   = data.aws_cloudfront_origin_request_policy.all_viewer_except_host.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id
  }

  # --- Password-protected links ---
  ordered_cache_behavior {
    path_pattern               = "/p/*"
    allowed_methods            = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods             = ["GET", "HEAD"]
    target_origin_id           = "api-gateway"
    viewer_protocol_policy     = "redirect-to-https"
    compress                   = true
    cache_policy_id            = aws_cloudfront_cache_policy.no_cache.id
    origin_request_policy_id   = data.aws_cloudfront_origin_request_policy.all_viewer_except_host.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id
  }

  # --- Auth flows ---
  ordered_cache_behavior {
    path_pattern               = "/auth/*"
    allowed_methods            = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods             = ["GET", "HEAD"]
    target_origin_id           = "api-gateway"
    viewer_protocol_policy     = "redirect-to-https"
    compress                   = true
    cache_policy_id            = aws_cloudfront_cache_policy.no_cache.id
    origin_request_policy_id   = data.aws_cloudfront_origin_request_policy.all_viewer_except_host.id
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
    acm_certificate_arn            = local.has_certificate ? var.acm_certificate_arn : null
    ssl_support_method             = local.has_certificate ? "sni-only" : null
    minimum_protocol_version       = local.has_certificate ? "TLSv1.2_2021" : "TLSv1"
    cloudfront_default_certificate = !local.has_certificate
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  # --- Logging ---
  dynamic "logging_config" {
    for_each = var.enable_logging ? [1] : []
    content {
      include_cookies = false
      bucket          = var.log_bucket_domain_name
      prefix          = "cloudfront/"
    }
  }

  tags = local.common_tags
}
