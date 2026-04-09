# ADR-010: Cognito SSO with JWT in httpOnly Cookies

## Status

Accepted

## Context

Shorty requires user authentication for the dashboard (link management, analytics)
and API access. The requirements specify (requirements-init.md Section 2.4):

- **Google OAuth 2.0** (MVP)
- **GitHub OAuth 2.0** (post-MVP)
- **Email/password** (Cognito User Pool)
- JWT tokens (Access + Refresh)
- Guest mode with strict quotas

The authentication system must integrate with Lambda authorizers and support
server-side rendering of the dashboard (SSR tokens in cookies, not localStorage).

## Decision

We use **AWS Cognito User Pools** as the identity provider with **JWT tokens stored
in httpOnly cookies** for session management.

### Why Cognito

1. **Native AWS integration**: Cognito integrates directly with API Gateway as a
   JWT authorizer -- no custom Lambda authorizer needed for token validation.
2. **Managed OAuth flows**: Cognito Hosted UI handles the OAuth redirect dance for
   Google (and later GitHub) without custom implementation.
3. **User management built-in**: email verification, password reset, MFA -- all
   managed by Cognito, no custom implementation needed.
4. **Cost**: Free for up to 50,000 MAU (Monthly Active Users). This covers Shorty's
   launch phase without authentication costs.

### OAuth configuration

```
Cognito User Pool
├── Identity Provider: Google (OAuth 2.0)
│   ├── Client ID: (from Google Cloud Console)
│   ├── Scopes: openid, email, profile
│   └── Attribute mapping: email → email, sub → google_sub
├── Identity Provider: GitHub (post-MVP)
│   └── Via OIDC custom provider (GitHub doesn't natively support OIDC)
└── Native: Email/Password
    ├── Password policy: 8+ chars, mixed case, number, special char
    ├── MFA: optional TOTP (post-MVP)
    └── Email verification: required
```

### JWT token strategy

- **Access token**: short-lived (15 minutes), used for API authorization.
- **ID token**: contains user profile claims (email, plan, quotas).
- **Refresh token**: long-lived (30 days), used to obtain new access tokens.
- All tokens stored in **httpOnly, Secure, SameSite=Strict** cookies.

### Why httpOnly cookies (not localStorage)

- **XSS protection**: httpOnly cookies cannot be accessed by JavaScript, preventing
  token theft via XSS attacks.
- **CSRF mitigation**: SameSite=Strict prevents cross-origin cookie sending. The
  API also validates the `Origin` header.
- **SSR compatibility**: cookies are sent automatically with every request, enabling
  server-side rendering without client-side token management.

### Token refresh flow

```
Client → API Gateway → Lambda (check access token)
  ↓ (expired)
Client → /auth/refresh → Lambda
  ↓ (validate refresh token with Cognito)
Cognito → new access token + new ID token
  ↓
Lambda → Set-Cookie (new tokens) → Client
```

The refresh endpoint is a dedicated Lambda that calls Cognito's `InitiateAuth` with
`REFRESH_TOKEN_AUTH` flow. If the refresh token is also expired (30 days), the user
must re-authenticate.

### API Gateway JWT authorizer

```hcl
resource "aws_apigatewayv2_authorizer" "cognito" {
  api_id           = aws_apigatewayv2_api.shorty.id
  authorizer_type  = "JWT"
  identity_sources = ["$request.header.Cookie"]

  jwt_configuration {
    audience = [aws_cognito_user_pool_client.shorty.id]
    issuer   = "https://cognito-idp.${var.region}.amazonaws.com/${aws_cognito_user_pool.shorty.id}"
  }
}
```

Note: API Gateway v2 JWT authorizer reads from headers. A lightweight Lambda@Edge
or API Gateway request mapping extracts the JWT from the cookie and places it in
the `Authorization` header before the authorizer evaluates it.

### Guest mode

Unauthenticated users can create links via `POST /api/v1/shorten` with:
- No JWT required.
- IP-based rate limiting (5 links/hour, see ADR-006).
- Links created by guests have `owner_id = ANON#{ip_hash}` and a maximum 24-hour TTL.

## Consequences

**Positive:**
- Managed authentication with zero custom auth code for the common cases.
- Free for the first 50K MAU, scaling to $0.0055/MAU beyond that.
- Native API Gateway integration eliminates custom authorizer Lambda latency.
- httpOnly cookies provide defense-in-depth against XSS token theft.
- Cognito handles email verification, password reset, and account recovery.

**Negative:**
- Cognito Hosted UI customization is limited. The login/signup pages have a
  specific look that may not match the Shorty brand perfectly. Custom UI requires
  implementing the SRP auth flow client-side.
- GitHub OAuth requires a custom OIDC provider configuration in Cognito (GitHub
  does not support standard OIDC discovery). This adds complexity to the post-MVP
  GitHub SSO implementation.
- Cookie-based auth requires CSRF protection on state-changing endpoints. The
  SameSite=Strict attribute handles most cases, but cross-origin API consumers
  (future developer API) will need Bearer token support.
- Cognito has regional limitations: User Pools are region-specific. Multi-region
  deployment would require Cognito pool replication or a global IdP.

## Alternatives Considered

**Auth0:**
Rejected. Auth0 is a superior product for authentication (better UI customization,
more identity providers, excellent developer experience). However, it adds a
non-AWS dependency, costs $23/month for the first 1,000 MAU on the paid plan,
and requires custom Lambda authorizer code for JWT validation. Cognito's free tier
and native AWS integration outweigh Auth0's UX advantages for MVP.

**Firebase Authentication:**
Rejected. Firebase Auth is Google-ecosystem-centric. Integrating with AWS Lambda
requires custom JWT validation, and the Firebase Admin SDK adds a dependency on
Google Cloud client libraries. Not suitable for an AWS-native stack.

**Custom JWT implementation (self-managed keys):**
Rejected. Rolling custom auth is a security liability. Key management, token
revocation, password hashing, email verification -- all solved problems that
Cognito handles correctly out of the box.

**Bearer tokens in Authorization header (no cookies):**
Rejected as the primary mechanism. Bearer tokens in headers require client-side
JavaScript to manage token storage (typically localStorage), which is vulnerable
to XSS. httpOnly cookies are the secure default. Bearer token support may be
added as an alternative for the developer API (post-MVP).
