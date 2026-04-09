# deploy/terraform/environments/dev/main.tf
#
# Dev environment root configuration.
# Calls the shared root module with dev-specific values.

terraform {
  required_version = ">= 1.7.0"

  backend "s3" {
    bucket         = "shorty-terraform-state-dev"
    key            = "dev/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "shorty-terraform-locks-dev"
    encrypt        = true
  }
}

module "shorty" {
  source = "../../"

  project     = "shorty"
  environment = "dev"
  region      = "us-east-1"

  # VPC / Networking
  vpc_id                   = var.vpc_id
  private_subnet_ids       = var.private_subnet_ids
  lambda_security_group_id = var.lambda_security_group_id

  # Lambda artifacts
  artifacts_bucket = "shorty-dev-artifacts"

  # DynamoDB: on-demand for dev (no capacity planning needed)
  dynamodb_billing_mode       = "PAY_PER_REQUEST"
  enable_dynamodb_autoscaling = false

  # ElastiCache: single small node for dev
  redis_node_type          = "cache.t4g.micro"
  redis_num_cache_clusters = 1

  # WAF: skip Bot Control in dev to save cost
  enable_bot_control = false

  # CloudFront: cheapest price class (NA + EU)
  cloudfront_domain_aliases = []

  # Cognito
  cognito_domain_prefix = "shorty-dev"
  cognito_callback_urls = var.cognito_callback_urls
  cognito_logout_urls   = var.cognito_logout_urls
  enable_google_idp     = false

  # Custom domain disabled in dev
  enable_custom_domain = false

  # CORS: allow local dev origins
  cors_allow_origins = ["http://localhost:3000", "http://localhost:8080"]

  # Monitoring
  alert_email = var.alert_email
}

# =============================================================================
# Variables (env-specific overrides)
# =============================================================================

variable "vpc_id" {
  description = "VPC ID for dev environment"
  type        = string
}

variable "private_subnet_ids" {
  description = "Private subnet IDs"
  type        = list(string)
}

variable "lambda_security_group_id" {
  description = "Security group ID for Lambda functions"
  type        = string
}

variable "alert_email" {
  description = "Email for CloudWatch alarm notifications"
  type        = string
  default     = ""
}

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
