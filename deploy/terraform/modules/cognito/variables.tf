# deploy/terraform/modules/cognito/variables.tf

variable "project" {
  description = "Project name used for resource naming"
  type        = string
  default     = "shorty"
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

# --- User Pool ---

variable "pool_name" {
  description = "Name of the Cognito User Pool"
  type        = string
  default     = ""
}

variable "deletion_protection" {
  description = "Enable deletion protection (recommended for prod)"
  type        = bool
  default     = false
}

# --- Password policy ---

variable "password_minimum_length" {
  description = "Minimum password length"
  type        = number
  default     = 8
}

# --- MFA ---

variable "mfa_configuration" {
  description = "MFA enforcement: OFF, ON, or OPTIONAL"
  type        = string
  default     = "OPTIONAL"
}

# --- Email ---

variable "ses_email_arn" {
  description = "SES verified email identity ARN (for prod email sending)"
  type        = string
  default     = ""
}

variable "from_email_address" {
  description = "From email address for Cognito emails"
  type        = string
  default     = ""
}

# --- Hosted UI domain ---

variable "domain_prefix" {
  description = "Cognito hosted UI domain prefix (e.g., 'shorty-dev')"
  type        = string
  default     = ""
}

variable "custom_domain" {
  description = "Custom domain for Cognito hosted UI (e.g., 'auth.shorty.io')"
  type        = string
  default     = ""
}

variable "custom_domain_certificate_arn" {
  description = "ACM certificate ARN for custom Cognito domain (us-east-1)"
  type        = string
  default     = ""
}

# --- Google OAuth ---

variable "enable_google_idp" {
  description = "Enable Google as an identity provider"
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

# --- App client ---

variable "callback_urls" {
  description = "OAuth callback URLs"
  type        = list(string)
  default     = ["http://localhost:8080/auth/callback"]
}

variable "logout_urls" {
  description = "OAuth logout URLs"
  type        = list(string)
  default     = ["http://localhost:8080/"]
}

variable "access_token_validity_hours" {
  description = "Access token validity in hours"
  type        = number
  default     = 1
}

variable "id_token_validity_hours" {
  description = "ID token validity in hours"
  type        = number
  default     = 1
}

variable "refresh_token_validity_days" {
  description = "Refresh token validity in days"
  type        = number
  default     = 30
}

# --- Lambda triggers (optional) ---

variable "pre_sign_up_lambda_arn" {
  description = "ARN of pre sign-up Lambda trigger"
  type        = string
  default     = ""
}

variable "post_confirmation_lambda_arn" {
  description = "ARN of post confirmation Lambda trigger"
  type        = string
  default     = ""
}

variable "pre_token_generation_lambda_arn" {
  description = "ARN of pre token generation Lambda trigger"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default     = {}
}
