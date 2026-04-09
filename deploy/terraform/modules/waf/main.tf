# deploy/terraform/modules/waf/main.tf
#
# WAF Web ACL with CLOUDFRONT scope. Must be deployed in us-east-1.

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
}

# =============================================================================
# IP Set for Manual Blocklist
# =============================================================================

resource "aws_wafv2_ip_set" "blocked_ips_v4" {
  name               = "${var.project}-blocked-ips"
  scope              = "CLOUDFRONT"
  ip_address_version = "IPV4"
  addresses          = []

  tags = local.common_tags
}

resource "aws_wafv2_ip_set" "blocked_ips_v6" {
  name               = "${var.project}-blocked-ips-v6"
  scope              = "CLOUDFRONT"
  ip_address_version = "IPV6"
  addresses          = []

  tags = local.common_tags
}

# =============================================================================
# Regex Pattern Set for Suspicious User-Agents
# =============================================================================

resource "aws_wafv2_regex_pattern_set" "suspicious_ua" {
  name  = "${var.project}-suspicious-ua"
  scope = "CLOUDFRONT"

  regular_expression {
    regex_string = "(?i)(nikto|sqlmap|nmap|masscan|zgrab)"
  }
  regular_expression {
    regex_string = "(?i)(scrapy|httpclient|python-requests)"
  }
  regular_expression {
    regex_string = "^$"
  }

  tags = local.common_tags
}

# =============================================================================
# Web ACL
# =============================================================================

resource "aws_wafv2_web_acl" "this" {
  name  = "${var.project}-waf"
  scope = "CLOUDFRONT"

  default_action {
    allow {}
  }

  # --- Priority 1: IP Blocklist (IPv4) ---
  rule {
    name     = "${var.project}-ip-blocklist"
    priority = 1

    action {
      block {}
    }

    statement {
      or_statement {
        statement {
          ip_set_reference_statement {
            arn = aws_wafv2_ip_set.blocked_ips_v4.arn
          }
        }
        statement {
          ip_set_reference_statement {
            arn = aws_wafv2_ip_set.blocked_ips_v6.arn
          }
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-ip-blocklist"
    }
  }

  # --- Priority 2: AWS Managed Rules - Common Rule Set ---
  rule {
    name     = "aws-common-rules"
    priority = 2

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesCommonRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-common-rules"
    }
  }

  # --- Priority 3: AWS Managed Rules - Known Bad Inputs ---
  rule {
    name     = "aws-known-bad-inputs"
    priority = 3

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesKnownBadInputsRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-known-bad-inputs"
    }
  }

  # --- Priority 4: AWS Managed Rules - Bot Control (optional) ---
  dynamic "rule" {
    for_each = var.enable_bot_control ? [1] : []
    content {
      name     = "aws-bot-control"
      priority = 4

      override_action {
        dynamic "count" {
          for_each = var.bot_control_action == "count" ? [1] : []
          content {}
        }
        dynamic "none" {
          for_each = var.bot_control_action != "count" ? [1] : []
          content {}
        }
      }

      statement {
        managed_rule_group_statement {
          name        = "AWSManagedRulesBotControlRuleSet"
          vendor_name = "AWS"

          managed_rule_group_configs {
            aws_managed_rules_bot_control_rule_set {
              inspection_level = "COMMON"
            }
          }
        }
      }

      visibility_config {
        sampled_requests_enabled   = true
        cloudwatch_metrics_enabled = true
        metric_name                = "${var.project}-bot-control"
      }
    }
  }

  # --- Priority 5: AWS Managed Rules - IP Reputation List ---
  rule {
    name     = "aws-ip-reputation"
    priority = 5

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAmazonIpReputationList"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-ip-reputation"
    }
  }

  # --- Priority 6: Rate-based rule - Global ---
  rule {
    name     = "${var.project}-rate-limit-global"
    priority = 6

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = var.rate_limit_global
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-rate-limit-global"
    }
  }

  # --- Priority 7: Rate-based rule - Password endpoint ---
  rule {
    name     = "${var.project}-rate-limit-password"
    priority = 7

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = var.rate_limit_password
        aggregate_key_type = "IP"

        scope_down_statement {
          byte_match_statement {
            search_string         = "/p/"
            positional_constraint = "STARTS_WITH"
            field_to_match {
              uri_path {}
            }
            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-rate-limit-password"
    }
  }

  # --- Priority 8: Rate-based rule - Link creation ---
  rule {
    name     = "${var.project}-rate-limit-create"
    priority = 8

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = var.rate_limit_create
        aggregate_key_type = "IP"

        scope_down_statement {
          and_statement {
            statement {
              byte_match_statement {
                search_string         = "/api/v1/shorten"
                positional_constraint = "EXACTLY"
                field_to_match {
                  uri_path {}
                }
                text_transformation {
                  priority = 0
                  type     = "LOWERCASE"
                }
              }
            }
            statement {
              byte_match_statement {
                search_string         = "POST"
                positional_constraint = "EXACTLY"
                field_to_match {
                  method {}
                }
                text_transformation {
                  priority = 0
                  type     = "NONE"
                }
              }
            }
          }
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-rate-limit-create"
    }
  }

  # --- Priority 9: CAPTCHA on link creation (optional) ---
  dynamic "rule" {
    for_each = var.enable_captcha_on_create ? [1] : []
    content {
      name     = "${var.project}-captcha-create"
      priority = 9

      action {
        captcha {}
      }

      statement {
        and_statement {
          statement {
            byte_match_statement {
              search_string         = "/api/v1/shorten"
              positional_constraint = "EXACTLY"
              field_to_match {
                uri_path {}
              }
              text_transformation {
                priority = 0
                type     = "LOWERCASE"
              }
            }
          }
          statement {
            byte_match_statement {
              search_string         = "POST"
              positional_constraint = "EXACTLY"
              field_to_match {
                method {}
              }
              text_transformation {
                priority = 0
                type     = "NONE"
              }
            }
          }
        }
      }

      captcha_config {
        immunity_time_property {
          immunity_time = var.captcha_immunity_time
        }
      }

      visibility_config {
        sampled_requests_enabled   = true
        cloudwatch_metrics_enabled = true
        metric_name                = "${var.project}-captcha-create"
      }
    }
  }

  # --- Priority 10: Block suspicious User-Agents ---
  rule {
    name     = "${var.project}-block-suspicious-ua"
    priority = 10

    action {
      block {}
    }

    statement {
      regex_pattern_set_reference_statement {
        arn = aws_wafv2_regex_pattern_set.suspicious_ua.arn
        field_to_match {
          single_header {
            name = "user-agent"
          }
        }
        text_transformation {
          priority = 0
          type     = "NONE"
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project}-suspicious-ua"
    }
  }

  # --- Priority 11: Geo-blocking (optional) ---
  dynamic "rule" {
    for_each = length(var.blocked_country_codes) > 0 ? [1] : []
    content {
      name     = "${var.project}-geo-block"
      priority = 11

      action {
        block {}
      }

      statement {
        geo_match_statement {
          country_codes = var.blocked_country_codes
        }
      }

      visibility_config {
        sampled_requests_enabled   = true
        cloudwatch_metrics_enabled = true
        metric_name                = "${var.project}-geo-block"
      }
    }
  }

  visibility_config {
    sampled_requests_enabled   = true
    cloudwatch_metrics_enabled = true
    metric_name                = "${var.project}-waf"
  }

  tags = local.common_tags
}

# =============================================================================
# WAF Logging
# =============================================================================

resource "aws_cloudwatch_log_group" "waf" {
  name              = "aws-waf-logs-${var.project}"
  retention_in_days = var.waf_log_retention_days

  tags = local.common_tags
}

resource "aws_wafv2_web_acl_logging_configuration" "this" {
  log_destination_configs = [aws_cloudwatch_log_group.waf.arn]
  resource_arn            = aws_wafv2_web_acl.this.arn

  logging_filter {
    default_behavior = "DROP"

    filter {
      behavior    = "KEEP"
      requirement = "MEETS_ANY"

      condition {
        action_condition {
          action = "BLOCK"
        }
      }
      condition {
        action_condition {
          action = "CAPTCHA"
        }
      }
      condition {
        action_condition {
          action = "COUNT"
        }
      }
    }
  }

  redacted_fields {
    single_header {
      name = "authorization"
    }
  }

  redacted_fields {
    single_header {
      name = "cookie"
    }
  }
}

# =============================================================================
# CloudWatch Alarms
# =============================================================================

resource "aws_cloudwatch_metric_alarm" "high_block_rate" {
  count               = var.sns_topic_arn != "" ? 1 : 0
  alarm_name          = "${var.project}-waf-high-block-rate"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "BlockedRequests"
  namespace           = "AWS/WAFV2"
  period              = 300
  statistic           = "Sum"
  threshold           = 5000
  alarm_description   = "WAF blocking > 5000 requests in 5 minutes"
  alarm_actions       = [var.sns_topic_arn]

  dimensions = {
    WebACL = aws_wafv2_web_acl.this.name
    Region = "us-east-1"
    Rule   = "ALL"
  }

  tags = local.common_tags
}
