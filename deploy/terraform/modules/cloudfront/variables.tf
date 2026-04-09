# deploy/terraform/modules/cloudfront/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

# --- Origin ---

variable "api_gateway_endpoint" {
  description = "API Gateway v2 endpoint domain (without https://)"
  type        = string
}

variable "origin_verify_secret" {
  description = "Secret value for X-Origin-Verify header to prevent direct API GW access"
  type        = string
  sensitive   = true
}

# --- Domain and TLS ---

variable "domain_aliases" {
  description = "CloudFront alternate domain names (e.g., ['s.shorty.io', 'api.shorty.io'])"
  type        = list(string)
  default     = []
}

variable "acm_certificate_arn" {
  description = "ACM certificate ARN in us-east-1 for CloudFront"
  type        = string
  default     = ""
}

# --- WAF ---

variable "web_acl_arn" {
  description = "WAF Web ACL ARN to associate with the distribution"
  type        = string
  default     = ""
}

# --- Pricing ---

variable "price_class" {
  description = "CloudFront price class (PriceClass_100, PriceClass_200, PriceClass_All)"
  type        = string
  default     = "PriceClass_100"
}

# --- Logging ---

variable "log_bucket_domain_name" {
  description = "S3 bucket domain name for CloudFront standard logs"
  type        = string
  default     = ""
}

variable "enable_logging" {
  description = "Enable CloudFront standard logging to S3"
  type        = bool
  default     = false
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
