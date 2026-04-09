# deploy/terraform/environments/prod/terraform.tfvars
#
# Production environment variable values.
#
# SENSITIVE values MUST be set via TF_VAR_* environment variables or
# GitHub Actions secrets. Never commit actual secrets to this file:
#   - TF_VAR_redis_auth_token       (from AWS Secrets Manager)
#   - TF_VAR_origin_verify_secret   (from AWS Secrets Manager)
#   - TF_VAR_google_client_id       (from AWS Secrets Manager)
#   - TF_VAR_google_client_secret   (from AWS Secrets Manager)

# --- VPC / Networking ---
# Replace with actual AWS resource IDs before first apply.
# These are typically outputs from a separate VPC Terraform workspace.
vpc_id                   = "vpc-prod-placeholder"
private_subnet_ids       = ["subnet-prod-a", "subnet-prod-b", "subnet-prod-c"]
lambda_security_group_id = "sg-prod-lambda-placeholder"

# --- Domain ---
# Set these when custom domain is ready (requires ACM certificate validation).
enable_custom_domain          = false
domain_name                   = ""
hosted_zone_id                = ""
acm_certificate_arn           = ""
acm_certificate_arn_us_east_1 = ""
cloudfront_domain_aliases     = []

# --- Cognito ---
cognito_domain_prefix = "shorty"
cognito_callback_urls = ["https://shorty.example.com/auth/callback"]
cognito_logout_urls   = ["https://shorty.example.com/"]

# --- CORS ---
cors_allow_origins = ["https://shorty.example.com"]

# --- Monitoring ---
# On-call rotation email. Replace with actual PagerDuty or team alias.
alert_email = "oncall@example.com"
