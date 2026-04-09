# Authentication Flow -- Cognito OAuth + JWT

Covers the full lifecycle: user login via Cognito Hosted UI, JWT issuance, API request authentication, and token refresh.

## 1. Initial Login (OAuth / Cognito Hosted UI)

```mermaid
sequenceDiagram
    participant User
    participant App as Shorty Frontend
    participant Cognito as AWS Cognito<br/>(Hosted UI)
    participant Google as Google OAuth
    participant Lambda as Lambda (api)

    User->>App: Click "Sign in with Google"
    App->>Cognito: Redirect to /oauth2/authorize<br/>response_type=code<br/>client_id={app_client_id}<br/>redirect_uri=https://shorty.example.com/callback<br/>scope=openid email profile

    alt Google OAuth
        Cognito->>Google: Redirect to Google consent
        User->>Google: Grant consent
        Google-->>Cognito: Authorization code
        Cognito->>Google: Exchange code for Google tokens
        Google-->>Cognito: Google ID token + access token
        Note over Cognito: Map Google claims to Cognito user pool<br/>Create user on first login (auto-signup)
    else Email/Password
        Cognito-->>User: Show login form
        User->>Cognito: Enter email + password
        Note over Cognito: Validate credentials against User Pool
    end

    Cognito-->>App: Redirect to /callback?code={auth_code}

    App->>Cognito: POST /oauth2/token<br/>grant_type=authorization_code<br/>code={auth_code}<br/>client_id={app_client_id}
    Cognito-->>App: {access_token, id_token, refresh_token, expires_in: 3600}

    Note over App: Store tokens:<br/>access_token: httpOnly cookie (1h TTL)<br/>refresh_token: httpOnly cookie (30d TTL)<br/>SameSite=Strict, Secure

    Note over App: First login: create user profile in DynamoDB
    App->>Lambda: GET /api/v1/me<br/>Authorization: Bearer {access_token}
    
    alt User profile does not exist
        Lambda->>Lambda: Create user profile<br/>PutItem(PK=USER#{sub}, SK=PROFILE)<br/>Condition: attribute_not_exists(PK)
        Note over Lambda: Default plan=free,<br/>daily_link_quota=50,<br/>total_link_quota=500
    end

    Lambda-->>App: 200 {user profile}
```

## 2. Authenticated API Request

```mermaid
sequenceDiagram
    participant Client
    participant CF as CloudFront
    participant APIGW as API Gateway v2
    participant Authorizer as Cognito JWT Authorizer
    participant Lambda as Lambda (api)
    participant Redis

    Client->>CF: POST /api/v1/links<br/>Authorization: Bearer {access_token}<br/>Cookie: access_token={jwt}
    CF->>APIGW: Forward (TLS terminated at CF)
    
    APIGW->>Authorizer: Validate JWT
    Note over Authorizer: 1. Decode JWT header (kid)<br/>2. Fetch JWKS from Cognito (cached)<br/>3. Verify RS256 signature<br/>4. Check exp, iss, aud claims<br/>5. Extract sub, email from claims

    alt Token expired
        Authorizer-->>APIGW: 401 Unauthorized
        APIGW-->>CF: 401 {error: "token_expired"}
        CF-->>Client: 401 Unauthorized
        Note over Client: Trigger token refresh flow
    end

    alt Token invalid (bad signature, wrong issuer)
        Authorizer-->>APIGW: 401 Unauthorized
        APIGW-->>CF: 401 {error: "invalid_token"}
        CF-->>Client: 401 Unauthorized
    end

    Authorizer-->>APIGW: Authorized<br/>claims: {sub, email, cognito:groups}
    
    APIGW->>Lambda: Invoke with request context<br/>(claims injected by authorizer)

    Note over Lambda: Rate limit check (per-user)
    Lambda->>Redis: EVAL sliding_window.lua rl:user:{sub}
    Redis-->>Lambda: [allowed, remaining, reset]

    alt Rate limited
        Lambda-->>APIGW: 429 + X-RateLimit-* headers
        APIGW-->>CF: 429
        CF-->>Client: 429 Too Many Requests
    end

    Note over Lambda: Process request with owner_id = USER#{sub}
    Lambda-->>APIGW: 200/201 + response body
    APIGW-->>CF: Response
    CF-->>Client: Response + X-RateLimit-* headers
```

## 3. Token Refresh

```mermaid
sequenceDiagram
    participant Client
    participant App as Shorty Frontend
    participant Cognito as AWS Cognito

    Note over Client: API returns 401 (token expired)
    
    Client->>App: Intercept 401 response

    App->>Cognito: POST /oauth2/token<br/>grant_type=refresh_token<br/>refresh_token={refresh_token}<br/>client_id={app_client_id}

    alt Refresh token valid
        Cognito-->>App: {access_token (new), id_token (new), expires_in: 3600}
        Note over App: Update httpOnly cookies with new tokens
        App->>Client: Retry original request with new access_token
    else Refresh token expired/revoked
        Cognito-->>App: 400 {error: "invalid_grant"}
        App->>Client: Redirect to login page
        Note over Client: Full re-authentication required
    end
```

## 4. Guest (Anonymous) Access

```mermaid
sequenceDiagram
    participant Client
    participant APIGW as API Gateway v2
    participant Lambda as Lambda (api)
    participant Redis

    Client->>APIGW: POST /api/v1/shorten<br/>(no Authorization header)
    APIGW->>Lambda: Invoke (no auth context)

    Note over Lambda: No JWT validation.<br/>Identify by IP hash: ANON#{SHA256(IP+salt)}

    Lambda->>Redis: EVAL sliding_window.lua rl:create:{ip_hash}
    Redis-->>Lambda: [allowed, remaining, reset]

    alt Rate limited (5 links/hour per IP)
        Lambda-->>APIGW: 429 Too Many Requests<br/>Retry-After: {seconds}
        APIGW-->>Client: 429
    end

    Note over Lambda: Create link with:<br/>owner_id = ANON#{ip_hash}<br/>expires_at = now + 24h (forced)<br/>No custom alias, no password

    Lambda-->>APIGW: 201 Created
    APIGW-->>Client: 201 {code, short_url, expires_at}
```

## JWT Claims Structure

| Claim | Source | Usage |
|-------|--------|-------|
| `sub` | Cognito | User ID (UUID). Becomes `USER#{sub}` in DynamoDB. |
| `email` | Cognito | User email. Stored in `users` table. |
| `cognito:groups` | Cognito | `free`, `pro`, `enterprise`. Maps to plan tier. |
| `iss` | Cognito | `https://cognito-idp.{region}.amazonaws.com/{user_pool_id}` |
| `aud` | Cognito | App client ID. Validated by authorizer. |
| `exp` | Cognito | Token expiry (1 hour from issuance). |
| `iat` | Cognito | Token issued-at timestamp. |

## Security Controls

| Control | Implementation |
|---------|----------------|
| Token storage | httpOnly + Secure + SameSite=Strict cookies |
| Token lifetime | Access: 1 hour. Refresh: 30 days. |
| JWKS caching | API Gateway caches Cognito JWKS (5 min TTL) |
| CSRF protection | SameSite=Strict cookie + CSRF token on forms |
| Token revocation | Cognito GlobalSignOut invalidates all refresh tokens |
| Brute-force protection | Cognito native: account lockout after 5 failed attempts |
