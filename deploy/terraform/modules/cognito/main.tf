# deploy/terraform/modules/cognito/main.tf

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

  pool_name    = var.pool_name != "" ? var.pool_name : "${var.project}-users-${var.environment}"
  use_ses      = var.ses_email_arn != ""
  has_triggers = var.pre_sign_up_lambda_arn != "" || var.post_confirmation_lambda_arn != "" || var.pre_token_generation_lambda_arn != ""

  supported_identity_providers = var.enable_google_idp ? ["COGNITO", "Google"] : ["COGNITO"]
}

# =============================================================================
# User Pool
# =============================================================================

resource "aws_cognito_user_pool" "this" {
  name = local.pool_name

  # Sign-in configuration
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  # Deletion protection
  deletion_protection = var.deletion_protection ? "ACTIVE" : "INACTIVE"

  # Password policy
  password_policy {
    minimum_length                   = var.password_minimum_length
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = true
    require_uppercase                = true
    temporary_password_validity_days = 7
  }

  # MFA
  mfa_configuration = var.mfa_configuration
  software_token_mfa_configuration {
    enabled = true
  }

  # Account recovery
  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  # Email configuration
  email_configuration {
    email_sending_account = local.use_ses ? "DEVELOPER" : "COGNITO_DEFAULT"
    source_arn            = local.use_ses ? var.ses_email_arn : null
    from_email_address    = local.use_ses ? var.from_email_address : null
  }

  # Advanced security
  user_pool_add_ons {
    advanced_security_mode = "AUDIT"
  }

  # Verification message
  verification_message_template {
    default_email_option = "CONFIRM_WITH_CODE"
    email_subject        = "Shorty - Verify your email"
    email_message        = "Your verification code is: {####}"
  }

  # Custom attributes
  schema {
    name                = "plan"
    attribute_data_type = "String"
    mutable             = true
    string_attribute_constraints {
      min_length = 1
      max_length = 20
    }
  }

  schema {
    name                = "max_links"
    attribute_data_type = "Number"
    mutable             = true
    number_attribute_constraints {
      min_value = "0"
      max_value = "1000000"
    }
  }

  schema {
    name                = "daily_limit"
    attribute_data_type = "Number"
    mutable             = true
    number_attribute_constraints {
      min_value = "0"
      max_value = "100000"
    }
  }

  # Lambda triggers (conditionally set)
  dynamic "lambda_config" {
    for_each = local.has_triggers ? [1] : []
    content {
      pre_sign_up          = var.pre_sign_up_lambda_arn != "" ? var.pre_sign_up_lambda_arn : null
      post_confirmation    = var.post_confirmation_lambda_arn != "" ? var.post_confirmation_lambda_arn : null
      pre_token_generation = var.pre_token_generation_lambda_arn != "" ? var.pre_token_generation_lambda_arn : null
    }
  }

  tags = local.common_tags
}

# =============================================================================
# Hosted UI Domain
# =============================================================================

resource "aws_cognito_user_pool_domain" "prefix" {
  count        = var.domain_prefix != "" && var.custom_domain == "" ? 1 : 0
  domain       = var.domain_prefix
  user_pool_id = aws_cognito_user_pool.this.id
}

resource "aws_cognito_user_pool_domain" "custom" {
  count           = var.custom_domain != "" ? 1 : 0
  domain          = var.custom_domain
  user_pool_id    = aws_cognito_user_pool.this.id
  certificate_arn = var.custom_domain_certificate_arn
}

# =============================================================================
# Google Identity Provider (optional)
# =============================================================================

resource "aws_cognito_identity_provider" "google" {
  count         = var.enable_google_idp ? 1 : 0
  user_pool_id  = aws_cognito_user_pool.this.id
  provider_name = "Google"
  provider_type = "Google"

  provider_details = {
    client_id        = var.google_client_id
    client_secret    = var.google_client_secret
    authorize_scopes = "openid email profile"
  }

  attribute_mapping = {
    email       = "email"
    given_name  = "given_name"
    family_name = "family_name"
    picture     = "picture"
    username    = "sub"
  }
}

# =============================================================================
# App Client
# =============================================================================

resource "aws_cognito_user_pool_client" "this" {
  name         = "${var.project}-web-${var.environment}"
  user_pool_id = aws_cognito_user_pool.this.id

  # No client secret (public client for PKCE)
  generate_secret = false

  # Auth flows
  explicit_auth_flows = [
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]

  # OAuth configuration
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes                 = ["openid", "email", "profile"]

  supported_identity_providers = local.supported_identity_providers

  # Callback URLs
  callback_urls = var.callback_urls
  logout_urls   = var.logout_urls

  # Token validity
  access_token_validity  = var.access_token_validity_hours
  id_token_validity      = var.id_token_validity_hours
  refresh_token_validity = var.refresh_token_validity_days

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }

  # Security
  prevent_user_existence_errors = "ENABLED"
  enable_token_revocation       = true

  # Read/write attributes
  read_attributes = [
    "email",
    "email_verified",
    "given_name",
    "family_name",
    "picture",
    "custom:plan",
    "custom:max_links",
    "custom:daily_limit",
  ]

  write_attributes = [
    "email",
    "given_name",
    "family_name",
    "picture",
  ]

  depends_on = [aws_cognito_identity_provider.google]
}

# =============================================================================
# User Groups
# =============================================================================

resource "aws_cognito_user_group" "free_tier" {
  name         = "free-tier"
  user_pool_id = aws_cognito_user_pool.this.id
  description  = "Free tier users - 500 links, 50/day"
  precedence   = 3
}

resource "aws_cognito_user_group" "pro_tier" {
  name         = "pro-tier"
  user_pool_id = aws_cognito_user_pool.this.id
  description  = "Pro tier users - 10000 links, 500/day"
  precedence   = 2
}

resource "aws_cognito_user_group" "admin" {
  name         = "admin"
  user_pool_id = aws_cognito_user_pool.this.id
  description  = "System administrators"
  precedence   = 1
}

# =============================================================================
# Lambda Trigger Permissions
# =============================================================================

resource "aws_lambda_permission" "pre_sign_up" {
  count         = var.pre_sign_up_lambda_arn != "" ? 1 : 0
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = var.pre_sign_up_lambda_arn
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = aws_cognito_user_pool.this.arn
}

resource "aws_lambda_permission" "post_confirmation" {
  count         = var.post_confirmation_lambda_arn != "" ? 1 : 0
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = var.post_confirmation_lambda_arn
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = aws_cognito_user_pool.this.arn
}

resource "aws_lambda_permission" "pre_token_generation" {
  count         = var.pre_token_generation_lambda_arn != "" ? 1 : 0
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = var.pre_token_generation_lambda_arn
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = aws_cognito_user_pool.this.arn
}
