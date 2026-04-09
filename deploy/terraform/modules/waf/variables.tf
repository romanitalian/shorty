# deploy/terraform/modules/waf/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

# --- Rate limits ---

variable "rate_limit_global" {
  description = "Global rate limit per IP per 5 minutes"
  type        = number
  default     = 1000
}

variable "rate_limit_password" {
  description = "Password endpoint rate limit per IP per 5 minutes (WAF minimum is 100)"
  type        = number
  default     = 100
}

variable "rate_limit_create" {
  description = "Link creation rate limit per IP per 5 minutes (WAF minimum is 100)"
  type        = number
  default     = 100
}

# --- Feature flags ---

variable "enable_bot_control" {
  description = "Enable AWS Bot Control managed rule (incurs additional cost)"
  type        = bool
  default     = false
}

variable "bot_control_action" {
  description = "Action for bot control rule: 'count' or 'none' (block)"
  type        = string
  default     = "count"
}

variable "enable_captcha_on_create" {
  description = "Enable CAPTCHA challenge on link creation endpoint"
  type        = bool
  default     = true
}

variable "captcha_immunity_time" {
  description = "CAPTCHA immunity time in seconds after solving"
  type        = number
  default     = 300
}

variable "blocked_country_codes" {
  description = "List of country codes to block (empty = no geo-blocking)"
  type        = list(string)
  default     = []
}

# --- Logging ---

variable "waf_log_retention_days" {
  description = "CloudWatch log retention for WAF logs"
  type        = number
  default     = 90
}

# --- SNS ---

variable "sns_topic_arn" {
  description = "SNS topic ARN for WAF alarm notifications"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
