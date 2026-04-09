# deploy/terraform/main.tf
#
# Root module that wires all Shorty infrastructure modules together.
# Environment-specific values should be set via terraform.tfvars or
# the deploy/terraform/environments/{env}/terraform.tfvars files.

terraform {
  required_version = ">= 1.7.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      project     = var.project
      environment = var.environment
      managed_by  = "terraform"
    }
  }
}

# Provider for us-east-1 (required for CloudFront, WAF CLOUDFRONT scope, ACM)
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"

  default_tags {
    tags = {
      project     = var.project
      environment = var.environment
      managed_by  = "terraform"
    }
  }
}

# =============================================================================
# Variables
# =============================================================================

variable "project" {
  description = "Project name"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

# --- VPC (assumed to exist or managed separately) ---

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "private_subnet_ids" {
  description = "Private subnet IDs for Lambda and ElastiCache"
  type        = list(string)
}

variable "lambda_security_group_id" {
  description = "Security group ID for Lambda functions"
  type        = string
}

# --- Lambda artifacts ---

variable "artifacts_bucket" {
  description = "S3 bucket containing Lambda deployment packages"
  type        = string
}

# --- Domain configuration (optional) ---

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

variable "acm_certificate_arn_us_east_1" {
  description = "ACM certificate ARN in us-east-1 (for CloudFront)"
  type        = string
  default     = ""
}

variable "hosted_zone_id" {
  description = "Route 53 hosted zone ID"
  type        = string
  default     = ""
}

# --- CloudFront ---

variable "cloudfront_domain_aliases" {
  description = "CloudFront alternate domain names"
  type        = list(string)
  default     = []
}

variable "origin_verify_secret" {
  description = "Secret value for X-Origin-Verify header"
  type        = string
  sensitive   = true
  default     = "change-me-in-production"
}

# --- Cognito ---

variable "cognito_callback_urls" {
  description = "OAuth callback URLs"
  type        = list(string)
  default     = ["http://localhost:8080/auth/callback"]
}

variable "cognito_logout_urls" {
  description = "OAuth logout URLs"
  type        = list(string)
  default     = ["http://localhost:8080/"]
}

variable "cognito_domain_prefix" {
  description = "Cognito hosted UI domain prefix"
  type        = string
  default     = ""
}

variable "enable_google_idp" {
  description = "Enable Google OAuth identity provider"
  type        = bool
  default     = false
}

variable "google_client_id" {
  description = "Google OAuth client ID"
  type        = string
  default     = ""
  sensitive   = true
}

variable "google_client_secret" {
  description = "Google OAuth client secret"
  type        = string
  default     = ""
  sensitive   = true
}

# --- ElastiCache ---

variable "redis_node_type" {
  description = "ElastiCache Redis node type"
  type        = string
  default     = "cache.t4g.small"
}

variable "redis_num_cache_clusters" {
  description = "Number of Redis cache clusters"
  type        = number
  default     = 1
}

variable "redis_auth_token" {
  description = "Redis AUTH token"
  type        = string
  default     = ""
  sensitive   = true
}

# --- CORS ---

variable "cors_allow_origins" {
  description = "Allowed CORS origins"
  type        = list(string)
  default     = ["http://localhost:3000", "http://localhost:8080"]
}

# --- Monitoring ---

variable "alert_email" {
  description = "Email address for alarm notifications"
  type        = string
  default     = ""
}

# --- DynamoDB ---

variable "dynamodb_billing_mode" {
  description = "DynamoDB billing mode for links table"
  type        = string
  default     = "PAY_PER_REQUEST"
}

variable "dynamodb_clicks_billing_mode" {
  description = "DynamoDB billing mode for clicks table (defaults to dynamodb_billing_mode if empty)"
  type        = string
  default     = ""
}

variable "enable_dynamodb_autoscaling" {
  description = "Enable DynamoDB auto-scaling (for PROVISIONED mode)"
  type        = bool
  default     = false
}

# --- WAF ---

variable "enable_bot_control" {
  description = "Enable WAF Bot Control (additional cost)"
  type        = bool
  default     = false
}

# =============================================================================
# Locals
# =============================================================================

locals {
  is_prod          = var.environment == "prod"
  log_retention    = local.is_prod ? 90 : 14
  apigw_log_retention = local.is_prod ? 90 : 30
}

# =============================================================================
# DynamoDB
# =============================================================================

module "dynamodb" {
  source = "./modules/dynamodb"

  project     = var.project
  environment = var.environment

  links_billing_mode  = var.dynamodb_billing_mode
  clicks_billing_mode = var.dynamodb_clicks_billing_mode != "" ? var.dynamodb_clicks_billing_mode : var.dynamodb_billing_mode
  users_billing_mode  = "PAY_PER_REQUEST"

  enable_autoscaling          = var.enable_dynamodb_autoscaling
  deletion_protection_enabled = local.is_prod
}

# =============================================================================
# SQS
# =============================================================================

module "sqs" {
  source = "./modules/sqs"

  project     = var.project
  environment = var.environment

  queue_name                 = "clicks"
  visibility_timeout_seconds = 90
  sns_topic_arn              = module.monitoring.sns_topic_arn
}

# =============================================================================
# ElastiCache
# =============================================================================

module "elasticache" {
  source = "./modules/elasticache"

  project     = var.project
  environment = var.environment

  node_type          = var.redis_node_type
  num_cache_clusters = var.redis_num_cache_clusters
  vpc_id             = var.vpc_id
  subnet_ids         = var.private_subnet_ids

  lambda_security_group_id   = var.lambda_security_group_id
  automatic_failover_enabled = local.is_prod && var.redis_num_cache_clusters >= 2
  multi_az_enabled           = local.is_prod && var.redis_num_cache_clusters >= 2
  auth_token                 = var.redis_auth_token
  snapshot_retention_limit   = local.is_prod ? 7 : 0
}

# =============================================================================
# Lambda
# =============================================================================

module "lambda" {
  source = "./modules/lambda"

  project     = var.project
  environment = var.environment
  region      = var.region

  artifacts_bucket = var.artifacts_bucket

  subnet_ids         = var.private_subnet_ids
  security_group_ids = [var.lambda_security_group_id]

  # DynamoDB ARNs for IAM policies
  dynamodb_links_table_arn  = module.dynamodb.links_table_arn
  dynamodb_clicks_table_arn = module.dynamodb.clicks_table_arn
  dynamodb_users_table_arn  = module.dynamodb.users_table_arn

  # SQS ARNs
  sqs_clicks_queue_arn             = module.sqs.queue_arn
  sqs_clicks_queue_arn_for_trigger = module.sqs.queue_arn

  # Worker DLQ
  worker_dlq_arn = module.sqs.dlq_arn

  # Log retention
  log_retention_days = local.log_retention

  # Concurrency
  redirect_provisioned_concurrency = local.is_prod ? 2 : 0

  # Environment variables
  redirect_environment_variables = {
    DYNAMODB_TABLE_LINKS = module.dynamodb.links_table_name
    SQS_QUEUE_URL        = module.sqs.queue_url
    REDIS_ADDR           = module.elasticache.redis_address
    OTEL_SERVICE_NAME    = "${var.project}-redirect"
    LOG_LEVEL            = local.is_prod ? "info" : "debug"
  }

  api_environment_variables = {
    DYNAMODB_TABLE_LINKS  = module.dynamodb.links_table_name
    DYNAMODB_TABLE_CLICKS = module.dynamodb.clicks_table_name
    DYNAMODB_TABLE_USERS  = module.dynamodb.users_table_name
    REDIS_ADDR            = module.elasticache.redis_address
    COGNITO_USER_POOL_ID  = module.cognito.user_pool_id
    COGNITO_CLIENT_ID     = module.cognito.client_id
    OTEL_SERVICE_NAME     = "${var.project}-api"
    LOG_LEVEL             = local.is_prod ? "info" : "debug"
  }

  worker_environment_variables = {
    DYNAMODB_TABLE_CLICKS = module.dynamodb.clicks_table_name
    DYNAMODB_TABLE_LINKS  = module.dynamodb.links_table_name
    OTEL_SERVICE_NAME     = "${var.project}-worker"
    LOG_LEVEL             = local.is_prod ? "info" : "debug"
  }
}

# =============================================================================
# Cognito
# =============================================================================

module "cognito" {
  source = "./modules/cognito"

  project     = var.project
  environment = var.environment

  deletion_protection = local.is_prod
  domain_prefix       = var.cognito_domain_prefix
  callback_urls       = var.cognito_callback_urls
  logout_urls         = var.cognito_logout_urls

  enable_google_idp    = var.enable_google_idp
  google_client_id     = var.google_client_id
  google_client_secret = var.google_client_secret
}

# =============================================================================
# API Gateway
# =============================================================================

module "api_gateway" {
  source = "./modules/api_gateway"

  project     = var.project
  environment = var.environment
  region      = var.region

  redirect_lambda_invoke_arn = module.lambda.redirect_alias_invoke_arn
  api_lambda_invoke_arn      = module.lambda.api_alias_invoke_arn
  redirect_function_name     = module.lambda.redirect_function_name
  api_function_name          = module.lambda.api_function_name
  redirect_alias_arn         = module.lambda.redirect_alias_arn
  api_alias_arn              = module.lambda.api_alias_arn

  cognito_user_pool_id = module.cognito.user_pool_id
  cognito_client_id    = module.cognito.client_id

  cors_allow_origins = var.cors_allow_origins

  access_log_retention_days = local.apigw_log_retention

  enable_custom_domain = var.enable_custom_domain
  domain_name          = var.domain_name
  acm_certificate_arn  = var.acm_certificate_arn
  hosted_zone_id       = var.hosted_zone_id
}

# =============================================================================
# WAF (must be in us-east-1 for CLOUDFRONT scope)
# =============================================================================

module "waf" {
  source = "./modules/waf"

  providers = {
    aws = aws.us_east_1
  }

  project     = var.project
  environment = var.environment

  enable_bot_control = var.enable_bot_control
  sns_topic_arn      = module.monitoring.sns_topic_arn
}

# =============================================================================
# CloudFront
# =============================================================================

module "cloudfront" {
  source = "./modules/cloudfront"

  providers = {
    aws = aws.us_east_1
  }

  project     = var.project
  environment = var.environment

  api_gateway_endpoint = replace(module.api_gateway.api_endpoint, "https://", "")
  origin_verify_secret = var.origin_verify_secret
  domain_aliases       = var.cloudfront_domain_aliases
  acm_certificate_arn  = var.acm_certificate_arn_us_east_1
  web_acl_arn          = module.waf.web_acl_arn
}

# =============================================================================
# Monitoring
# =============================================================================

module "monitoring" {
  source = "./modules/monitoring"

  project     = var.project
  environment = var.environment

  api_gateway_id         = module.api_gateway.api_id
  redirect_function_name = module.lambda.redirect_function_name
  api_function_name      = module.lambda.api_function_name
  worker_function_name   = module.lambda.worker_function_name
  links_table_name       = module.dynamodb.links_table_name
  clicks_table_name      = module.dynamodb.clicks_table_name

  sns_email = var.alert_email
}

# =============================================================================
# Outputs
# =============================================================================

output "api_endpoint" {
  description = "API Gateway endpoint URL"
  value       = module.api_gateway.api_endpoint
}

output "cloudfront_domain" {
  description = "CloudFront distribution domain name"
  value       = module.cloudfront.distribution_domain_name
}

output "cognito_user_pool_id" {
  description = "Cognito User Pool ID"
  value       = module.cognito.user_pool_id
}

output "cognito_client_id" {
  description = "Cognito App Client ID"
  value       = module.cognito.client_id
}

output "redis_address" {
  description = "ElastiCache Redis endpoint"
  value       = module.elasticache.redis_address
}

output "sqs_queue_url" {
  description = "SQS clicks queue URL"
  value       = module.sqs.queue_url
}

output "sns_alerts_topic_arn" {
  description = "SNS alerts topic ARN"
  value       = module.monitoring.sns_topic_arn
}

output "api_gateway_id" {
  description = "API Gateway v2 API ID"
  value       = module.api_gateway.api_id
}
