# deploy/terraform/modules/api_gateway/variables.tf

variable "project" {
  description = "Project name used for resource naming"
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
}

# --- Lambda integration ARNs ---

variable "redirect_lambda_invoke_arn" {
  description = "Invoke ARN of the redirect Lambda alias"
  type        = string
}

variable "api_lambda_invoke_arn" {
  description = "Invoke ARN of the API Lambda alias"
  type        = string
}

variable "redirect_function_name" {
  description = "Name of the redirect Lambda function (for permission)"
  type        = string
}

variable "api_function_name" {
  description = "Name of the API Lambda function (for permission)"
  type        = string
}

variable "redirect_alias_arn" {
  description = "ARN of the redirect Lambda alias (for permission qualifier)"
  type        = string
}

variable "api_alias_arn" {
  description = "ARN of the API Lambda alias (for permission qualifier)"
  type        = string
}

# --- Cognito JWT authorizer ---

variable "cognito_user_pool_id" {
  description = "Cognito User Pool ID for JWT authorizer"
  type        = string
}

variable "cognito_client_id" {
  description = "Cognito App Client ID for JWT audience"
  type        = string
}

# --- CORS ---

variable "cors_allow_origins" {
  description = "Allowed origins for CORS"
  type        = list(string)
  default     = ["http://localhost:3000", "http://localhost:8080"]
}

variable "cors_max_age" {
  description = "CORS max age in seconds"
  type        = number
  default     = 3600
}

# --- Throttling ---

variable "default_throttle_burst_limit" {
  description = "Default stage throttle burst limit"
  type        = number
  default     = 5000
}

variable "default_throttle_rate_limit" {
  description = "Default stage throttle rate limit"
  type        = number
  default     = 2000
}

# --- Custom domain (optional) ---

variable "enable_custom_domain" {
  description = "Whether to create a custom domain for the API"
  type        = bool
  default     = false
}

variable "domain_name" {
  description = "Custom domain name for the API (e.g., api.shorty.io)"
  type        = string
  default     = ""
}

variable "acm_certificate_arn" {
  description = "ACM certificate ARN for custom domain"
  type        = string
  default     = ""
}

variable "hosted_zone_id" {
  description = "Route 53 hosted zone ID for DNS record"
  type        = string
  default     = ""
}

# --- Logging ---

variable "access_log_retention_days" {
  description = "CloudWatch log retention for API Gateway access logs"
  type        = number
  default     = 30
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
