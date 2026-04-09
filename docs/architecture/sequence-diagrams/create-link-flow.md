# Create Link Flow -- `POST /api/v1/links`

Authenticated link creation with code generation, collision handling, and cache warming.

```mermaid
sequenceDiagram
    participant Client
    participant APIGW as API Gateway v2
    participant Auth as Cognito Authorizer
    participant Lambda as Lambda (api)
    participant Redis
    participant DDB as DynamoDB (links)
    participant Users as DynamoDB (users)

    Client->>APIGW: POST /api/v1/links<br/>Authorization: Bearer {jwt}
    
    APIGW->>Auth: Validate JWT
    alt Invalid/expired token
        Auth-->>APIGW: 401 Unauthorized
        APIGW-->>Client: 401 Unauthorized
    end
    Auth-->>APIGW: Claims {sub, email, ...}
    APIGW->>Lambda: Invoke (body + claims)

    Note over Lambda: Step 1: Rate limit check
    Lambda->>Redis: EVAL sliding_window.lua rl:create:{ip_hash}
    Redis-->>Lambda: [allowed, remaining, reset]

    alt Rate limited
        Lambda-->>APIGW: 429 Too Many Requests<br/>Retry-After: {seconds}
        APIGW-->>Client: 429
    end

    Note over Lambda: Step 2: Quota check
    Lambda->>Users: GetItem(PK=USER#{sub}, SK=PROFILE)
    Users-->>Lambda: {plan, daily_link_quota, links_created_today, last_reset_date, total_active_links, total_link_quota}

    alt Daily reset needed (last_reset_date != today)
        Lambda->>Users: UpdateItem SET links_created_today=0, last_reset_date=today<br/>Condition: last_reset_date <> :today
    end

    alt Daily quota exceeded
        Lambda-->>APIGW: 429 {error: "daily_quota_exceeded", limit: 50}
        APIGW-->>Client: 429
    end

    alt Total quota exceeded
        Lambda-->>APIGW: 429 {error: "total_quota_exceeded", limit: 500}
        APIGW-->>Client: 429
    end

    Note over Lambda: Step 3: Input validation
    Note over Lambda: - URL format + max 2048 chars<br/>- Custom alias: ^[a-zA-Z0-9_-]{3,32}$<br/>- Password min 4 chars<br/>- expires_at must be in the future

    alt Validation failure
        Lambda-->>APIGW: 422 Unprocessable Entity + field errors
        APIGW-->>Client: 422
    end

    Note over Lambda: Step 4: Code generation
    alt Custom alias provided
        Note over Lambda: Use provided alias as code
    else Auto-generate
        Note over Lambda: Generate Base62 code (7 chars)
    end

    Note over Lambda: Step 5: DynamoDB PutItem with collision check
    
    loop Up to 3 retries (then extend code length +1)
        Lambda->>DDB: PutItem(PK=LINK#{code}, SK=META, ...)<br/>Condition: attribute_not_exists(PK)
        alt Collision (ConditionalCheckFailedException)
            Note over Lambda: Generate new code and retry
        else Success
            DDB-->>Lambda: OK
        end
    end

    alt All retries exhausted (extremely rare)
        Lambda-->>APIGW: 503 Service Unavailable
        APIGW-->>Client: 503 (retry with backoff)
    end

    Note over Lambda: Step 6: Increment user quota counters
    Lambda->>Users: UpdateItem SET links_created_today += 1, total_active_links += 1<br/>Condition: links_created_today < daily_link_quota

    alt Quota race condition (ConditionalCheckFailed)
        Note over Lambda: Link was created but quota exceeded.<br/>Mark link inactive, return 429.
        Lambda->>DDB: UpdateItem SET is_active=false
        Lambda-->>APIGW: 429 {error: "daily_quota_exceeded"}
        APIGW-->>Client: 429
    end

    Users-->>Lambda: OK

    Note over Lambda: Step 7: Warm cache
    Lambda->>Redis: SET link:{code} {JSON} EX 300
    Redis-->>Lambda: OK

    Lambda-->>APIGW: 201 Created + Link object
    APIGW-->>Client: 201 {code, short_url, original_url, ...}

    Note right of Client: Total latency: 50-200ms<br/>p99 target: < 300ms
```

## Collision Handling Strategy

| Attempt | Action |
|---------|--------|
| 1 | Generate 7-char Base62 code, PutItem with `attribute_not_exists(PK)` |
| 2 | Generate new 7-char code, retry PutItem |
| 3 | Generate new 7-char code, retry PutItem |
| 4 | Generate 8-char code (62^8 = 218 trillion combinations), retry PutItem |
| 5+ | Return 503 Service Unavailable |

With 7-char codes and <100M links, collision probability per attempt is ~0.003%. Three consecutive collisions have probability ~2.7 x 10^-14.

## Password Handling

If `password` is provided in the request body:
1. Hash with bcrypt (cost=12)
2. Store `password_hash` in the link record
3. The redirect Lambda checks for `password_hash` presence and returns a 403 + HTML form if set
