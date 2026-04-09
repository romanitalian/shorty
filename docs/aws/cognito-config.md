# Cognito Configuration

> AWS Specialist specification for DevOps implementation.
> Reference: ADR-010, requirements-init.md Section 2.4.

---

## 1. User Pool Configuration

| Setting | Value |
|---|---|
| Name | `shorty-users-{env}` |
| Sign-in attributes | Email (case-insensitive) |
| Username | Email address |
| Deletion protection | Enabled (production) |
| Advanced security mode | AUDIT (promote to ENFORCED after baseline) |

### 1.1 Password Policy

| Setting | Value |
|---|---|
| Minimum length | 8 characters |
| Require uppercase | Yes |
| Require lowercase | Yes |
| Require numbers | Yes |
| Require symbols | Yes |
| Temporary password validity | 7 days |

### 1.2 Multi-Factor Authentication (MFA)

| Setting | Value |
|---|---|
| MFA enforcement | OPTIONAL (user can enable, not required) |
| MFA methods | TOTP (authenticator app) |
| SMS MFA | Disabled (cost and complexity) |
| Remember device | Enabled (30 days) |

MFA is optional for MVP to reduce onboarding friction. Can be promoted to REQUIRED for specific user groups (enterprise tier) post-MVP.

### 1.3 Email Verification

| Setting | Value |
|---|---|
| Verification method | Code (not link) |
| Email subject | "Shorty - Verify your email" |
| Email body | Custom template with verification code |
| Auto-verify | Email address |
| From address | `noreply@shorty.io` (requires SES verified domain) |

For MVP, use Cognito's default email (limited to 50 emails/day). For production, configure SES as the email provider.

### 1.4 Account Recovery

| Setting | Value |
|---|---|
| Recovery method | Email only |
| Phone recovery | Disabled |
| Admin-create-user | Allowed (for enterprise provisioning) |
| Self-registration | Enabled |

### 1.5 Custom Attributes

| Attribute | Type | Mutable | Purpose |
|---|---|---|---|
| `custom:plan` | String | Yes | User tier: `free`, `pro`, `enterprise` |
| `custom:max_links` | Number | Yes | Maximum total links for user |
| `custom:daily_limit` | Number | Yes | Daily link creation limit |
| `custom:api_key_enabled` | String | Yes | Whether API key access is enabled (post-MVP) |

Custom attributes are set by Lambda triggers and admin operations, not by users directly.

---

## 2. Lambda Triggers

### 2.1 Pre Sign-Up Trigger

Purpose: Validate and normalize user data before registration.

```go
// Pre-signup Lambda: validate email domain, auto-confirm for SSO users
func handler(ctx context.Context, event events.CognitoEventUserPoolsPreSignUp) (events.CognitoEventUserPoolsPreSignUp, error) {
    // Auto-confirm users from external identity providers (Google, GitHub)
    if event.TriggerSource == "PreSignUp_ExternalProvider" {
        event.Response.AutoConfirmUser = true
        event.Response.AutoVerifyEmail = true
    }

    // Block disposable email domains
    email := event.Request.UserAttributes["email"]
    if isDisposableEmail(email) {
        return event, fmt.Errorf("disposable email addresses are not allowed")
    }

    return event, nil
}
```

### 2.2 Post Confirmation Trigger

Purpose: Initialize user record in DynamoDB `users` table.

```go
// Post-confirmation Lambda: create user record with default quotas
func handler(ctx context.Context, event events.CognitoEventUserPoolsPostConfirmation) (events.CognitoEventUserPoolsPostConfirmation, error) {
    user := User{
        UserID:     event.Request.UserAttributes["sub"],
        Email:      event.Request.UserAttributes["email"],
        Plan:       "free",
        MaxLinks:   500,
        DailyLimit: 50,
        CreatedAt:  time.Now().UTC().Format(time.RFC3339),
    }

    err := dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String("shorty-users"),
        Item:      marshalUser(user),
    })

    return event, err
}
```

### 2.3 Pre Token Generation Trigger

Purpose: Add custom claims to JWT tokens.

```go
// Pre-token-generation Lambda: inject plan and quotas into ID token
func handler(ctx context.Context, event events.CognitoEventUserPoolsPreTokenGen) (events.CognitoEventUserPoolsPreTokenGen, error) {
    // Fetch user record from DynamoDB for current quotas
    user, err := getUserByID(ctx, event.Request.UserAttributes["sub"])
    if err != nil {
        return event, err
    }

    event.Response.ClaimsOverrideDetails.ClaimsToAddOrOverride = map[string]string{
        "custom:plan":        user.Plan,
        "custom:max_links":   strconv.Itoa(user.MaxLinks),
        "custom:daily_limit": strconv.Itoa(user.DailyLimit),
    }

    return event, nil
}
```

---

## 3. Identity Providers

### 3.1 Google OAuth 2.0 (MVP)

| Setting | Value |
|---|---|
| Provider type | Google |
| Client ID | Stored in Secrets Manager (`shorty/cognito/google-client-id`) |
| Client secret | Stored in Secrets Manager (`shorty/cognito/google-client-secret`) |
| Authorized scopes | `openid email profile` |
| Authorize endpoint | `https://accounts.google.com/o/oauth2/v2/auth` |
| Token endpoint | `https://oauth2.googleapis.com/token` |
| UserInfo endpoint | `https://openidconnect.googleapis.com/v1/userinfo` |

**Attribute mapping:**

| Google Attribute | Cognito Attribute |
|---|---|
| `sub` | `username` (prefixed as `Google_<sub>`) |
| `email` | `email` |
| `given_name` | `given_name` |
| `family_name` | `family_name` |
| `picture` | `picture` |

**Google Cloud Console setup:**

1. Create OAuth 2.0 Client ID (Web application type)
2. Authorized redirect URIs:
   - `https://{cognito-domain}.auth.{region}.amazoncognito.com/oauth2/idpresponse`
3. Authorized JavaScript origins:
   - `https://shorty.io`
   - `http://localhost:8080` (development only)

### 3.2 GitHub OAuth 2.0 (Post-MVP)

GitHub does not support standard OIDC discovery. Integration requires a custom OIDC provider in Cognito.

| Setting | Value |
|---|---|
| Provider type | OIDC (custom) |
| Client ID | Stored in Secrets Manager (`shorty/cognito/github-client-id`) |
| Client secret | Stored in Secrets Manager (`shorty/cognito/github-client-secret`) |
| Issuer URL | Custom (requires proxy Lambda, see below) |
| Authorized scopes | `user:email read:user` |

**GitHub OIDC workaround:**

GitHub's OAuth does not provide an OIDC-compliant discovery endpoint. Options:

1. **Lambda proxy** (recommended): Deploy a Lambda that implements the OIDC discovery document (`.well-known/openid-configuration`) and translates GitHub's OAuth responses to OIDC-compliant format.
2. **Cognito Identity Pool** (alternative): Use Identity Pool federation with GitHub as a custom developer provider. More complex but avoids the proxy Lambda.

Document this as post-MVP work. The Lambda proxy approach is simpler and well-documented.

---

## 4. App Client Configuration

| Setting | Value |
|---|---|
| Name | `shorty-web-{env}` |
| Generate client secret | No (public client for PKCE) |
| Auth flows | `ALLOW_USER_SRP_AUTH`, `ALLOW_REFRESH_TOKEN_AUTH` |
| OAuth grant types | Authorization code (`code`) |
| PKCE | Required (no implicit grant) |
| Prevent user existence errors | Enabled |

### 4.1 OAuth Scopes

| Scope | Purpose |
|---|---|
| `openid` | Required for OIDC |
| `email` | Access to user's email |
| `profile` | Access to user's name, picture |

### 4.2 Token Validity

| Token | Validity | Rationale |
|---|---|---|
| Access token | 1 hour | Short-lived for API authorization |
| ID token | 1 hour | Matches access token lifetime |
| Refresh token | 30 days | Long-lived for session persistence |

Note: ADR-010 specifies 15-minute access tokens for higher security. The 1-hour setting here is a UX trade-off for MVP -- evaluate after launch whether 15 minutes is viable without excessive token refresh traffic.

### 4.3 Callback URLs

| Environment | Callback URL | Logout URL |
|---|---|---|
| Production | `https://shorty.io/auth/callback` | `https://shorty.io/` |
| Staging | `https://staging.shorty.io/auth/callback` | `https://staging.shorty.io/` |
| Development | `http://localhost:8080/auth/callback` | `http://localhost:8080/` |

**Important:** `http://localhost` callback URLs must be removed from the production app client. Use separate Cognito app clients per environment.

### 4.4 Token Revocation

| Setting | Value |
|---|---|
| Enable token revocation | Yes |
| Revoke refresh token on sign-out | Yes |
| Revoke all tokens on password change | Yes |

---

## 5. Hosted UI vs Custom UI

### Trade-offs

| Aspect | Hosted UI | Custom UI |
|---|---|---|
| Implementation effort | Zero -- Cognito provides it | Must implement SRP auth flow |
| Branding | Limited customization (logo, CSS) | Full control |
| SSO redirects | Handled automatically | Must implement OAuth redirect dance |
| Security | Cognito manages CSRF, state parameter | Developer responsibility |
| Maintenance | AWS maintains | Team maintains |

### Recommendation

- **MVP:** Use Cognito Hosted UI. Customize with logo and brand colors.
- **Post-MVP:** Migrate to custom UI using Amplify JS SDK for the auth flows.

### Hosted UI Domain

```
Domain prefix: shorty
Full URL:      https://shorty.auth.{region}.amazoncognito.com
Custom domain: https://auth.shorty.io (requires ACM cert in us-east-1)
```

---

## 6. JWT Claims Structure

### Access Token Claims

```json
{
  "sub": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "iss": "https://cognito-idp.{region}.amazonaws.com/{user-pool-id}",
  "client_id": "{app-client-id}",
  "token_use": "access",
  "scope": "openid email profile",
  "auth_time": 1680000000,
  "exp": 1680003600,
  "iat": 1680000000,
  "jti": "unique-token-id",
  "username": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

### ID Token Claims

```json
{
  "sub": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "iss": "https://cognito-idp.{region}.amazonaws.com/{user-pool-id}",
  "aud": "{app-client-id}",
  "token_use": "id",
  "auth_time": 1680000000,
  "exp": 1680003600,
  "iat": 1680000000,
  "email": "user@example.com",
  "email_verified": true,
  "given_name": "John",
  "family_name": "Doe",
  "cognito:username": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "cognito:groups": ["free-tier"],
  "custom:plan": "free",
  "custom:max_links": "500",
  "custom:daily_limit": "50"
}
```

### Claims Used by Application

| Claim | Source Token | Used By | Purpose |
|---|---|---|---|
| `sub` | Access/ID | All Lambdas | User identity (`owner_id` in DynamoDB) |
| `email` | ID | API Lambda | Display, notifications |
| `cognito:groups` | ID | API Lambda | Group-based authorization |
| `custom:plan` | ID | API Lambda | Quota enforcement |
| `custom:max_links` | ID | API Lambda | Link creation limit |
| `custom:daily_limit` | ID | API Lambda | Daily creation limit |
| `scope` | Access | API Gateway | Scope-based authorization |

---

## 7. API Gateway JWT Authorizer Integration

### Configuration

```hcl
resource "aws_apigatewayv2_authorizer" "cognito" {
  api_id           = aws_apigatewayv2_api.shorty.id
  authorizer_type  = "JWT"
  name             = "cognito-jwt"
  identity_sources = ["$request.header.Authorization"]

  jwt_configuration {
    audience = [aws_cognito_user_pool_client.shorty.id]
    issuer   = "https://cognito-idp.${var.region}.amazonaws.com/${aws_cognito_user_pool.shorty.id}"
  }
}
```

### Cookie-to-Header Extraction

Since JWTs are stored in httpOnly cookies (ADR-010), a Lambda@Edge function at the CloudFront origin-request stage extracts the token and sets the `Authorization` header. See `cloudfront-config.md` Section 8 for the Lambda@Edge implementation.

### Protected Routes

| Route | Authorizer | Notes |
|---|---|---|
| `GET /{code}` | None | Public redirect endpoint |
| `POST /api/v1/shorten` | None (guest) or JWT | Guest access with IP rate limiting; JWT for authenticated users |
| `GET /api/v1/links` | JWT required | User's link list |
| `GET /api/v1/links/{id}` | JWT required | Link details |
| `PATCH /api/v1/links/{id}` | JWT required | Update link |
| `DELETE /api/v1/links/{id}` | JWT required | Delete link |
| `GET /api/v1/links/{id}/stats` | JWT required | Link analytics |
| `GET /api/v1/users/me` | JWT required | User profile |

### Guest Mode

Unauthenticated users can create links via `POST /api/v1/shorten` without a JWT:

- IP-based rate limiting: 5 links/hour (enforced by Redis rate limiter)
- Links have `owner_id = ANON#{sha256(ip + salt)}`
- Maximum 24-hour TTL
- No dashboard access, no analytics

---

## 8. Cognito User Groups

| Group | Purpose | Custom Attributes |
|---|---|---|
| `free-tier` | Default group for new users | `max_links=500`, `daily_limit=50` |
| `pro-tier` | Paid users (post-MVP) | `max_links=10000`, `daily_limit=500` |
| `enterprise-tier` | Enterprise customers | Custom limits |
| `admin` | System administrators | Full access |

Group membership is managed via:
- Post-confirmation Lambda trigger (assigns `free-tier` by default)
- Admin API (plan upgrades)
- Stripe webhook handler (post-MVP, automatic plan changes)

---

## 9. Terraform Example

```hcl
# --- Cognito User Pool ---
resource "aws_cognito_user_pool" "shorty" {
  name = "shorty-users-${var.environment}"

  # Sign-in configuration
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  # Deletion protection
  deletion_protection = var.environment == "prod" ? "ACTIVE" : "INACTIVE"

  # Password policy
  password_policy {
    minimum_length                   = 8
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = true
    require_uppercase                = true
    temporary_password_validity_days = 7
  }

  # MFA
  mfa_configuration = "OPTIONAL"
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
    email_sending_account = var.environment == "prod" ? "DEVELOPER" : "COGNITO_DEFAULT"
    source_arn            = var.environment == "prod" ? aws_ses_email_identity.noreply.arn : null
    from_email_address    = var.environment == "prod" ? "noreply@shorty.io" : null
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
      min_value = 0
      max_value = 1000000
    }
  }

  schema {
    name                = "daily_limit"
    attribute_data_type = "Number"
    mutable             = true
    number_attribute_constraints {
      min_value = 0
      max_value = 100000
    }
  }

  # Lambda triggers
  lambda_config {
    pre_sign_up           = aws_lambda_function.cognito_pre_signup.arn
    post_confirmation     = aws_lambda_function.cognito_post_confirmation.arn
    pre_token_generation  = aws_lambda_function.cognito_pre_token.arn
  }

  tags = {
    Project     = "shorty"
    Environment = var.environment
  }
}

# --- Cognito User Pool Domain ---
resource "aws_cognito_user_pool_domain" "shorty" {
  domain       = var.environment == "prod" ? "auth.shorty.io" : "shorty-${var.environment}"
  user_pool_id = aws_cognito_user_pool.shorty.id

  # Custom domain requires ACM certificate
  certificate_arn = var.environment == "prod" ? aws_acm_certificate.auth.arn : null
}

# --- Google Identity Provider ---
resource "aws_cognito_identity_provider" "google" {
  user_pool_id  = aws_cognito_user_pool.shorty.id
  provider_name = "Google"
  provider_type = "Google"

  provider_details = {
    client_id        = data.aws_secretsmanager_secret_version.google_client_id.secret_string
    client_secret    = data.aws_secretsmanager_secret_version.google_client_secret.secret_string
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

# --- App Client ---
resource "aws_cognito_user_pool_client" "shorty" {
  name         = "shorty-web-${var.environment}"
  user_pool_id = aws_cognito_user_pool.shorty.id

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

  supported_identity_providers = [
    "COGNITO",
    "Google",
  ]

  # Callback URLs
  callback_urls = var.environment == "prod" ? [
    "https://shorty.io/auth/callback",
  ] : [
    "https://${var.environment}.shorty.io/auth/callback",
    "http://localhost:8080/auth/callback",
  ]

  logout_urls = var.environment == "prod" ? [
    "https://shorty.io/",
  ] : [
    "https://${var.environment}.shorty.io/",
    "http://localhost:8080/",
  ]

  # Token validity
  access_token_validity  = 1   # hours
  id_token_validity      = 1   # hours
  refresh_token_validity = 30  # days

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
}

# --- User Groups ---
resource "aws_cognito_user_group" "free_tier" {
  name         = "free-tier"
  user_pool_id = aws_cognito_user_pool.shorty.id
  description  = "Free tier users - 500 links, 50/day"
  precedence   = 3
}

resource "aws_cognito_user_group" "pro_tier" {
  name         = "pro-tier"
  user_pool_id = aws_cognito_user_pool.shorty.id
  description  = "Pro tier users - 10000 links, 500/day"
  precedence   = 2
}

resource "aws_cognito_user_group" "admin" {
  name         = "admin"
  user_pool_id = aws_cognito_user_pool.shorty.id
  description  = "System administrators"
  precedence   = 1
  role_arn     = aws_iam_role.cognito_admin.arn
}

# --- Lambda trigger permissions ---
resource "aws_lambda_permission" "cognito_pre_signup" {
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.cognito_pre_signup.function_name
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = aws_cognito_user_pool.shorty.arn
}

resource "aws_lambda_permission" "cognito_post_confirmation" {
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.cognito_post_confirmation.function_name
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = aws_cognito_user_pool.shorty.arn
}

resource "aws_lambda_permission" "cognito_pre_token" {
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.cognito_pre_token.function_name
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = aws_cognito_user_pool.shorty.arn
}
```

---

## 10. Security Considerations

1. **Client secrets:** Google OAuth client ID and secret are stored in AWS Secrets Manager, never in Terraform state or code.
2. **PKCE required:** The app client uses authorization code flow with PKCE (Proof Key for Code Exchange). Implicit flow is disabled.
3. **Token storage:** All tokens in httpOnly, Secure, SameSite=Strict cookies (ADR-010).
4. **User existence errors:** Cognito is configured to return generic errors, preventing email enumeration attacks.
5. **Advanced security:** Cognito Advanced Security Features (adaptive authentication, compromised credential checks) are enabled in AUDIT mode initially.
6. **Custom domain:** Use `auth.shorty.io` instead of the default Cognito domain to prevent phishing via lookalike Cognito domains.
7. **Localhost callbacks:** Only present on non-production app clients. Production app client has only `https://shorty.io` callbacks.
