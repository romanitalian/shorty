# Redirect Flow -- `GET /{code}`

The hot path. Target: p99 < 100ms.

```mermaid
sequenceDiagram
    participant Client
    participant CF as CloudFront
    participant WAF
    participant APIGW as API Gateway v2
    participant Lambda as Lambda (redirect)
    participant Redis
    participant DDB as DynamoDB (links)
    participant SQS as SQS FIFO (clicks)

    Client->>CF: GET /abc1234
    CF->>WAF: Forward request
    
    alt WAF blocks (bot/rate limit/geo)
        WAF-->>CF: 403 Forbidden
        CF-->>Client: 403 Forbidden
    end

    WAF->>APIGW: Pass through
    APIGW->>Lambda: Invoke (code=abc1234)
    
    Note over Lambda: ~1ms: Rate limit check
    Lambda->>Redis: EVAL sliding_window.lua rl:redir:{ip_hash}
    Redis-->>Lambda: [allowed=1, remaining=198, reset=1712300060]

    alt Rate limited (allowed=0)
        Lambda-->>APIGW: 429 Too Many Requests
        APIGW-->>CF: 429 + Retry-After header
        CF-->>Client: 429 Too Many Requests
    end

    Note over Lambda: ~1ms: Cache lookup
    Lambda->>Redis: GET link:abc1234
    
    alt Cache HIT (~90% of requests)
        Redis-->>Lambda: JSON {original_url, expires_at, max_clicks, click_count, is_active, password_hash_present}
    else Cache MISS
        Redis-->>Lambda: nil
        Note over Lambda: ~5-10ms: DynamoDB fallback
        Lambda->>DDB: GetItem(PK=LINK#abc1234, SK=META)
        
        alt Link not found
            DDB-->>Lambda: empty
            Lambda->>Redis: SET link:abc1234:miss "1" EX 60
            Lambda-->>APIGW: 404 Not Found
            APIGW-->>CF: 404
            CF-->>Client: 404 Not Found
        end
        
        DDB-->>Lambda: Link record
        Lambda->>Redis: SET link:abc1234 {JSON} EX 300
        Note over Lambda: Cache warmed for 5 min
    end

    Note over Lambda: Validation checks
    
    alt Link inactive (is_active=false)
        Lambda-->>APIGW: 410 Gone
        APIGW-->>CF: 410
        CF-->>Client: 410 Gone
    end

    alt TTL expired (expires_at < now)
        Lambda-->>APIGW: 410 Gone
        APIGW-->>CF: 410
        CF-->>Client: 410 Gone
    end

    alt Click limit reached (click_count >= max_clicks)
        Lambda-->>APIGW: 410 Gone
        APIGW-->>CF: 410
        CF-->>Client: 410 Gone
    end

    alt Password required (password_hash_present=true)
        Lambda-->>APIGW: 403 + HTML password form
        APIGW-->>CF: 403
        CF-->>Client: 403 + Password form (with CSRF token)
    end

    Note over Lambda: Async click recording (goroutine, 2s timeout)
    Lambda-)SQS: SendMessage (code, ip_hash, country, device, referer, timestamp)
    Note over Lambda,SQS: Fire-and-forget: SQS failure does NOT block redirect

    Note over Lambda: Build redirect URL (append UTM params if configured)
    Lambda-->>APIGW: 302 Location: https://example.com/page?utm_source=...
    APIGW-->>CF: 302
    CF-->>Client: 302 Redirect

    Note right of Client: Total latency target:<br/>Cache hit: 5-15ms<br/>Cache miss: 15-30ms<br/>p99: < 100ms
```

## Timing Budget

| Step | Cache Hit | Cache Miss |
|------|-----------|------------|
| CloudFront + WAF | 1-3ms | 1-3ms |
| API Gateway routing | 1-2ms | 1-2ms |
| Lambda init (warm) | 0ms | 0ms |
| Rate limit (Redis) | 1ms | 1ms |
| Cache lookup (Redis) | 1ms | 1ms |
| DynamoDB GetItem | -- | 5-10ms |
| Cache write (Redis) | -- | 1ms |
| Validation logic | <1ms | <1ms |
| SQS SendMessage (async) | 0ms (non-blocking) | 0ms (non-blocking) |
| Response serialization | <1ms | <1ms |
| **Total** | **5-8ms** | **10-20ms** |

Cold start adds 200-400ms (mitigated by provisioned concurrency = 2).
