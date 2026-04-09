# deploy/terraform/environments/prod/main.tf
#
# Production environment root configuration.
# Full-capacity settings: provisioned DynamoDB (clicks), on-demand (links/users),
# multi-AZ Redis, WAF Bot Control, canary deployments.

terraform {
  required_version = ">= 1.7.0"

  backend "s3" {
    bucket         = "shorty-terraform-state-prod"
    key            = "prod/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "shorty-terraform-locks-prod"
    encrypt        = true
  }
}

module "shorty" {
  source = "../../"

  project     = "shorty"
  environment = "prod"
  region      = "us-east-1"

  # VPC / Networking
  vpc_id                   = var.vpc_id
  private_subnet_ids       = var.private_subnet_ids
  lambda_security_group_id = var.lambda_security_group_id

  # Lambda artifacts
  artifacts_bucket = "shorty-prod-artifacts"

  # DynamoDB:
  #   links/users  = on-demand (unpredictable traffic patterns, low write volume)
  #   clicks       = provisioned + auto-scaling (high write volume, predictable from redirect traffic)
  dynamodb_billing_mode        = "PAY_PER_REQUEST"
  dynamodb_clicks_billing_mode = "PROVISIONED"
  enable_dynamodb_autoscaling  = true

  # ElastiCache: multi-AZ for HA, r7g.large for production throughput
  redis_node_type          = "cache.r7g.large"
  redis_num_cache_clusters = 2
  redis_auth_token         = var.redis_auth_token

  # WAF: full Bot Control enabled in prod
  enable_bot_control = true

  # CloudFront: all edge locations for global coverage
  cloudfront_domain_aliases     = var.cloudfront_domain_aliases
  origin_verify_secret          = var.origin_verify_secret
  acm_certificate_arn_us_east_1 = var.acm_certificate_arn_us_east_1

  # Custom domain
  enable_custom_domain = var.enable_custom_domain
  domain_name          = var.domain_name
  acm_certificate_arn  = var.acm_certificate_arn
  hosted_zone_id       = var.hosted_zone_id

  # Cognito
  cognito_domain_prefix  = var.cognito_domain_prefix
  cognito_callback_urls  = var.cognito_callback_urls
  cognito_logout_urls    = var.cognito_logout_urls
  enable_google_idp      = true
  google_client_id       = var.google_client_id
  google_client_secret   = var.google_client_secret

  # CORS: production origins only
  cors_allow_origins = var.cors_allow_origins

  # Monitoring
  alert_email = var.alert_email
}

# =============================================================================
# Canary Deployment - Lambda Alias Routing
# =============================================================================
#
# During deploys, the deploy.sh script updates these weights via AWS CLI:
#   aws lambda update-alias --function-name <fn> --name live \
#     --routing-config '{"AdditionalVersionWeights":{"<new_ver>":0.1}}'
#
# After health check passes (10 min, error rate < 0.1%), it promotes to 100%.
# This Terraform resource captures the steady-state (100% on current version).

# =============================================================================
# SLO-Aligned CloudWatch Alarms (prod-specific)
# =============================================================================
#
# These alarms supplement the monitoring module's defaults with SLO burn-rate
# alerting as defined in docs/sre/slo.md.

resource "aws_sns_topic" "pagerduty_critical" {
  name = "shorty-prod-pagerduty-critical"

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
  }
}

resource "aws_sns_topic" "slack_warnings" {
  name = "shorty-prod-slack-warnings"

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
  }
}

resource "aws_sns_topic_subscription" "pagerduty_email" {
  topic_arn = aws_sns_topic.pagerduty_critical.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

resource "aws_sns_topic_subscription" "slack_email" {
  topic_arn = aws_sns_topic.slack_warnings.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# --- Burn Rate Critical: 14.4x (2% of monthly budget in 1 hour) ---
resource "aws_cloudwatch_metric_alarm" "redirect_burn_rate_critical" {
  alarm_name          = "shorty-prod-redirect-burn-rate-critical"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 3
  threshold           = 0

  alarm_description = "Redirect error budget burn rate CRITICAL (14.4x). 2% of monthly budget consumed in 1 hour. Page on-call immediately."

  metric_query {
    id          = "error_rate"
    expression  = "IF(total > 0, errors / total, 0)"
    label       = "Error Rate"
    return_data = true
  }

  metric_query {
    id = "errors"
    metric {
      metric_name = "5xx"
      namespace   = "AWS/ApiGateway"
      period      = 60
      stat        = "Sum"
      dimensions = {
        ApiId = module.shorty.api_gateway_id
      }
    }
  }

  metric_query {
    id = "total"
    metric {
      metric_name = "Count"
      namespace   = "AWS/ApiGateway"
      period      = 60
      stat        = "Sum"
      dimensions = {
        ApiId = module.shorty.api_gateway_id
      }
    }
  }

  # 14.4x burn rate = 1.44% error rate for 99.9% SLO
  threshold = 0.0144

  alarm_actions = [aws_sns_topic.pagerduty_critical.arn]
  ok_actions    = [aws_sns_topic.pagerduty_critical.arn]

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
    slo         = "availability"
    severity    = "critical"
  }
}

# --- Burn Rate Warning: 6x (5% of monthly budget in 6 hours) ---
resource "aws_cloudwatch_metric_alarm" "redirect_burn_rate_warning" {
  alarm_name          = "shorty-prod-redirect-burn-rate-warning"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 6
  datapoints_to_alarm = 4
  threshold           = 0

  alarm_description = "Redirect error budget burn rate WARNING (6x). 5% of monthly budget consumed in 6 hours. Investigate."

  metric_query {
    id          = "error_rate"
    expression  = "IF(total > 0, errors / total, 0)"
    label       = "Error Rate"
    return_data = true
  }

  metric_query {
    id = "errors"
    metric {
      metric_name = "5xx"
      namespace   = "AWS/ApiGateway"
      period      = 300
      stat        = "Sum"
      dimensions = {
        ApiId = module.shorty.api_gateway_id
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
        ApiId = module.shorty.api_gateway_id
      }
    }
  }

  # 6x burn rate = 0.6% error rate
  threshold = 0.006

  alarm_actions = [aws_sns_topic.slack_warnings.arn]
  ok_actions    = [aws_sns_topic.slack_warnings.arn]

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
    slo         = "availability"
    severity    = "warning"
  }
}

# --- Redirect Latency p99 > 100ms (SLO target) ---
resource "aws_cloudwatch_metric_alarm" "redirect_latency_p99" {
  alarm_name          = "shorty-prod-redirect-latency-p99"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 300
  extended_statistic  = "p99"
  threshold           = 100

  alarm_description = "Redirect Lambda p99 latency exceeds 100ms SLO target."

  dimensions = {
    FunctionName = "shorty-prod-redirect"
  }

  alarm_actions = [aws_sns_topic.slack_warnings.arn]
  ok_actions    = [aws_sns_topic.slack_warnings.arn]

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
    slo         = "latency"
    severity    = "warning"
  }
}

# --- API Latency p99 > 300ms (SLO target) ---
resource "aws_cloudwatch_metric_alarm" "api_latency_p99" {
  alarm_name          = "shorty-prod-api-latency-p99"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  datapoints_to_alarm = 2
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 300
  extended_statistic  = "p99"
  threshold           = 300

  alarm_description = "API Lambda p99 latency exceeds 300ms SLO target."

  dimensions = {
    FunctionName = "shorty-prod-api"
  }

  alarm_actions = [aws_sns_topic.slack_warnings.arn]
  ok_actions    = [aws_sns_topic.slack_warnings.arn]

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
    slo         = "latency"
    severity    = "warning"
  }
}

# --- SQS DLQ depth (any message = systematic failure) ---
resource "aws_cloudwatch_metric_alarm" "dlq_depth" {
  alarm_name          = "shorty-prod-dlq-messages"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 0

  alarm_description = "Messages detected in clicks DLQ. Indicates systematic processing failure."

  dimensions = {
    QueueName = "shorty-clicks-dlq.fifo"
  }

  alarm_actions = [aws_sns_topic.pagerduty_critical.arn]

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
    severity    = "critical"
  }
}

# --- SQS message age (backlog indicator) ---
resource "aws_cloudwatch_metric_alarm" "sqs_age" {
  alarm_name          = "shorty-prod-sqs-message-age"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "ApproximateAgeOfOldestMessage"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 60

  alarm_description = "SQS click events backing up. Oldest message > 60s."

  dimensions = {
    QueueName = "shorty-clicks.fifo"
  }

  alarm_actions = [aws_sns_topic.slack_warnings.arn]

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
    severity    = "warning"
  }
}

# --- Redis CPU utilization ---
resource "aws_cloudwatch_metric_alarm" "redis_cpu" {
  alarm_name          = "shorty-prod-redis-cpu-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "EngineCPUUtilization"
  namespace           = "AWS/ElastiCache"
  period              = 300
  statistic           = "Average"
  threshold           = 80

  alarm_description = "ElastiCache Redis CPU utilization > 80% sustained for 15 minutes."

  dimensions = {
    ReplicationGroupId = "shorty-prod"
  }

  alarm_actions = [aws_sns_topic.slack_warnings.arn]

  tags = {
    project     = "shorty"
    environment = "prod"
    managed_by  = "terraform"
    severity    = "warning"
  }
}

# =============================================================================
# Variables
# =============================================================================

variable "vpc_id" {
  description = "VPC ID for prod environment"
  type        = string
}

variable "private_subnet_ids" {
  description = "Private subnet IDs (multi-AZ)"
  type        = list(string)
}

variable "lambda_security_group_id" {
  description = "Security group ID for Lambda functions"
  type        = string
}

variable "redis_auth_token" {
  description = "Redis AUTH token for production (set via TF_VAR_redis_auth_token)"
  type        = string
  sensitive   = true
}

variable "origin_verify_secret" {
  description = "Secret for X-Origin-Verify header (set via TF_VAR_origin_verify_secret)"
  type        = string
  sensitive   = true
}

variable "cloudfront_domain_aliases" {
  description = "CloudFront alternate domain names"
  type        = list(string)
  default     = []
}

variable "acm_certificate_arn_us_east_1" {
  description = "ACM certificate ARN in us-east-1 (for CloudFront)"
  type        = string
  default     = ""
}

variable "enable_custom_domain" {
  description = "Enable custom domain for API Gateway"
  type        = bool
  default     = false
}

variable "domain_name" {
  description = "Custom domain name"
  type        = string
  default     = ""
}

variable "acm_certificate_arn" {
  description = "ACM certificate ARN (same region as API GW)"
  type        = string
  default     = ""
}

variable "hosted_zone_id" {
  description = "Route 53 hosted zone ID"
  type        = string
  default     = ""
}

variable "cognito_domain_prefix" {
  description = "Cognito hosted UI domain prefix"
  type        = string
  default     = "shorty"
}

variable "cognito_callback_urls" {
  description = "OAuth callback URLs"
  type        = list(string)
}

variable "cognito_logout_urls" {
  description = "OAuth logout URLs"
  type        = list(string)
}

variable "google_client_id" {
  description = "Google OAuth client ID (set via TF_VAR_google_client_id)"
  type        = string
  sensitive   = true
}

variable "google_client_secret" {
  description = "Google OAuth client secret (set via TF_VAR_google_client_secret)"
  type        = string
  sensitive   = true
}

variable "cors_allow_origins" {
  description = "Allowed CORS origins for production"
  type        = list(string)
}

variable "alert_email" {
  description = "Email for CloudWatch alarm notifications"
  type        = string
}

# =============================================================================
# Outputs
# =============================================================================

output "api_endpoint" {
  description = "API Gateway endpoint URL"
  value       = module.shorty.api_endpoint
}

output "cloudfront_domain" {
  description = "CloudFront distribution domain"
  value       = module.shorty.cloudfront_domain
}

output "cognito_user_pool_id" {
  description = "Cognito User Pool ID"
  value       = module.shorty.cognito_user_pool_id
}

output "cognito_client_id" {
  description = "Cognito App Client ID"
  value       = module.shorty.cognito_client_id
}

output "redis_address" {
  description = "ElastiCache Redis endpoint"
  value       = module.shorty.redis_address
}

output "sns_alerts_topic_arn" {
  description = "SNS alerts topic ARN"
  value       = module.shorty.sns_alerts_topic_arn
}

output "pagerduty_topic_arn" {
  description = "SNS topic ARN for PagerDuty-critical alerts"
  value       = aws_sns_topic.pagerduty_critical.arn
}
