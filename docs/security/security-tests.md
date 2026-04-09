# Security Test Specifications -- Shorty URL Shortener

**Version:** 1.0
**Date:** 2026-04-05
**Author:** Security Engineer (S1-T05)
**References:** threat-model.md, security-architecture.md, OWASP Testing Guide v4.2
**Status:** Active

---

## Overview

This document defines ten security test cases (SEC-001 through SEC-010) that verify the mitigations documented in the threat model and security architecture. Each test includes manual reproduction steps (curl), expected results, and a Go test skeleton for automation.

All tests target the local development stack (`make dev-up`) unless noted otherwise. The base URLs are:
- API: `http://localhost:8080`
- Redirect: `http://localhost:8081`

---

## SEC-001: Input Validation -- XSS via javascript: URL scheme

**Category:** Input Validation
**Threat reference:** STRIDE T-001 (Tampering), security-architecture.md section 2.1
**Severity:** High

### Description

Submitting a `javascript:` URI as the target URL must be rejected at creation time. If stored, it would execute arbitrary JavaScript when a victim follows the short link.

### Manual Test

```bash
curl -s -w "\n%{http_code}" -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"original_url": "javascript:alert(1)"}'
```

### Expected Result

- HTTP status: **400 Bad Request**
- Response body contains an error indicating the URL scheme is not allowed
- No record is created in DynamoDB

### Assertions

1. Response status code is 400.
2. Response JSON field `error` or `message` mentions "scheme" or "blocked".
3. A subsequent `GET /{code}` for any recently created code does not redirect to `javascript:alert(1)`.

### Go Test

```go
func TestSEC001_JavascriptSchemeRejected(t *testing.T) {
    body := strings.NewReader(`{"original_url":"javascript:alert(1)"}`)
    req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/shorten", body)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    assert.Contains(t, strings.ToLower(fmt.Sprint(result)), "scheme")
}
```

### Automation Notes

- Also test variations: `JAVASCRIPT:`, `JaVaScRiPt:`, `javascript\t:alert(1)`, `\x00javascript:alert(1)`.
- Include `data:text/html,<script>alert(1)</script>` and `vbscript:MsgBox` as additional scheme tests.

---

## SEC-002: Input Validation -- SSRF via Private IP Address

**Category:** Input Validation
**Threat reference:** STRIDE T-007 (SSRF), security-architecture.md section 2.2
**Severity:** Critical

### Description

Submitting a URL pointing to a private/internal IP address (RFC 1918, link-local, loopback) must be rejected to prevent Server-Side Request Forgery attacks, particularly against the AWS metadata endpoint (`169.254.169.254`).

### Manual Test

```bash
# Test RFC 1918 address
curl -s -w "\n%{http_code}" -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"original_url": "http://10.0.0.1/admin"}'

# Test AWS metadata endpoint
curl -s -w "\n%{http_code}" -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"original_url": "http://169.254.169.254/latest/meta-data/"}'

# Test localhost
curl -s -w "\n%{http_code}" -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"original_url": "http://127.0.0.1:3000/debug"}'
```

### Expected Result

- HTTP status: **400 Bad Request** for all three requests
- Response body indicates the host/IP is blocked

### Assertions

1. Response status code is 400 for each private IP range.
2. No link record exists in DynamoDB after submission.
3. No outbound HTTP request was made to the target IP (verify via network capture or mock).

### Go Test

```go
func TestSEC002_PrivateIPRejected(t *testing.T) {
    blockedURLs := []string{
        "http://10.0.0.1/admin",
        "http://172.16.0.1/",
        "http://192.168.1.1/",
        "http://169.254.169.254/latest/meta-data/",
        "http://127.0.0.1:3000/debug",
        "http://0.0.0.0/",
        "http://[::1]/",
        "http://[::]/",
        "http://localhost/admin",
    }

    for _, u := range blockedURLs {
        t.Run(u, func(t *testing.T) {
            body := strings.NewReader(fmt.Sprintf(`{"original_url":"%s"}`, u))
            req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/shorten", body)
            req.Header.Set("Content-Type", "application/json")

            resp, err := http.DefaultClient.Do(req)
            require.NoError(t, err)
            defer resp.Body.Close()

            assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
                "expected 400 for blocked URL: %s", u)
        })
    }
}
```

### Automation Notes

- Test DNS rebinding: submit a URL whose hostname resolves to `127.0.0.1` (e.g., `http://localtest.me/`). The validator must resolve the hostname and check the resulting IP.
- Test decimal IP notation: `http://2130706433/` (decimal for 127.0.0.1).
- Test IPv6-mapped IPv4: `http://[::ffff:10.0.0.1]/`.

---

## SEC-003: Authentication -- Access API Without JWT

**Category:** Authentication
**Threat reference:** STRIDE S-001 (Spoofing)
**Severity:** High

### Description

Accessing authenticated API endpoints without providing a JWT must return 401 Unauthorized. The API must not fall back to anonymous access for protected routes.

### Manual Test

```bash
# No Authorization header at all
curl -s -w "\n%{http_code}" http://localhost:8080/api/v1/links

# Empty Authorization header
curl -s -w "\n%{http_code}" http://localhost:8080/api/v1/links \
  -H "Authorization: "

# Bearer prefix with no token
curl -s -w "\n%{http_code}" http://localhost:8080/api/v1/links \
  -H "Authorization: Bearer "
```

### Expected Result

- HTTP status: **401 Unauthorized** for all three cases
- Response body: `{"error": "unauthorized"}` or similar
- No `WWW-Authenticate` header leaks internal details

### Assertions

1. Response status code is 401.
2. Response does not contain any link data.
3. Response headers do not expose server version or internal component names.

### Go Test

```go
func TestSEC003_NoJWTReturns401(t *testing.T) {
    cases := []struct {
        name   string
        header string
    }{
        {"no header", ""},
        {"empty bearer", "Bearer "},
        {"malformed", "NotBearer sometoken"},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/links", nil)
            if tc.header != "" {
                req.Header.Set("Authorization", tc.header)
            }

            resp, err := http.DefaultClient.Do(req)
            require.NoError(t, err)
            defer resp.Body.Close()

            assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
        })
    }
}
```

---

## SEC-004: Authentication -- Expired JWT Rejected

**Category:** Authentication
**Threat reference:** STRIDE S-002 (Spoofing), E-001 (Elevation of Privilege)
**Severity:** High

### Description

A JWT whose `exp` claim is in the past must be rejected. The server must not accept tokens even if they are only seconds past expiry.

### Manual Test

```bash
# Generate an expired JWT (exp = 1 second ago)
# This requires a test helper or jwt.io to craft the token.
# Example with a pre-crafted expired token:
EXPIRED_JWT="eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyLTEiLCJleHAiOjE3MDAwMDAwMDB9.INVALID_SIG"

curl -s -w "\n%{http_code}" http://localhost:8080/api/v1/links \
  -H "Authorization: Bearer $EXPIRED_JWT"
```

### Expected Result

- HTTP status: **401 Unauthorized**
- Response body indicates token is expired (without leaking internal details)

### Assertions

1. Response status code is 401.
2. A token with `exp` set to `time.Now().Unix() - 1` is rejected.
3. A token with `exp` set to `time.Now().Unix() + 3600` (valid) is accepted (control test).

### Go Test

```go
func TestSEC004_ExpiredJWTRejected(t *testing.T) {
    // Create a JWT with exp in the past using test signing key
    claims := jwt.MapClaims{
        "sub": "test-user-1",
        "exp": time.Now().Add(-1 * time.Second).Unix(),
        "iss": cognitoIssuer,
    }
    token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
    signedToken, err := token.SignedString(testPrivateKey)
    require.NoError(t, err)

    req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/links", nil)
    req.Header.Set("Authorization", "Bearer "+signedToken)

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
```

---

## SEC-005: Authentication -- JWT Algorithm Confusion (alg: none)

**Category:** Authentication
**Threat reference:** STRIDE E-002 (Elevation of Privilege)
**Severity:** Critical

### Description

An attacker crafts a JWT with `"alg": "none"` and no signature. If the server accepts it, any user can forge arbitrary tokens. The JWT library must enforce the expected algorithm (RS256) and reject `none`, `HS256` (symmetric confusion), and any other unexpected algorithm.

### Manual Test

```bash
# Craft a JWT with alg:none (base64url encoded)
# Header: {"alg":"none","typ":"JWT"}
# Payload: {"sub":"admin","exp":9999999999}
ALG_NONE_JWT="eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJhZG1pbiIsImV4cCI6OTk5OTk5OTk5OX0."

curl -s -w "\n%{http_code}" http://localhost:8080/api/v1/links \
  -H "Authorization: Bearer $ALG_NONE_JWT"
```

### Expected Result

- HTTP status: **401 Unauthorized**
- The server does not treat the token as valid regardless of payload claims

### Assertions

1. Response status code is 401.
2. The same payload signed with RS256 using the correct key IS accepted (control).
3. A JWT with `"alg": "HS256"` signed with the RSA public key (algorithm confusion attack) is rejected.

### Go Test

```go
func TestSEC005_AlgNoneRejected(t *testing.T) {
    // alg:none token
    header := base64url(`{"alg":"none","typ":"JWT"}`)
    payload := base64url(fmt.Sprintf(
        `{"sub":"admin","exp":%d}`, time.Now().Add(time.Hour).Unix(),
    ))
    algNoneToken := header + "." + payload + "."

    req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/links", nil)
    req.Header.Set("Authorization", "Bearer "+algNoneToken)

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSEC005_AlgHS256ConfusionRejected(t *testing.T) {
    // Sign with HS256 using the RSA public key as HMAC secret
    pubKeyBytes := x509.MarshalPKCS1PublicKey(&testPrivateKey.PublicKey)
    claims := jwt.MapClaims{
        "sub": "admin",
        "exp": time.Now().Add(time.Hour).Unix(),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    signedToken, _ := token.SignedString(pubKeyBytes)

    req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/links", nil)
    req.Header.Set("Authorization", "Bearer "+signedToken)

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
```

### Automation Notes

- The JWT validation code must use `jwt.WithValidMethods([]string{"RS256"})` or equivalent to whitelist the expected algorithm.
- Test `"alg": "RS384"`, `"alg": "PS256"` and other valid but unexpected algorithms as well.

---

## SEC-006: Rate Limiting -- Exceed Anonymous Request Quota

**Category:** Rate Limiting
**Threat reference:** STRIDE D-001 (Denial of Service)
**Severity:** High

### Description

The rate limiter must enforce the configured limit (200 requests/minute/IP for redirects). Sending 201 requests within one minute from the same source IP must result in a 429 response.

### Manual Test

```bash
# Send 201 requests in rapid succession
for i in $(seq 1 201); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8081/testcode)
  echo "Request $i: $STATUS"
  if [ "$STATUS" = "429" ]; then
    echo "Rate limited at request $i"
    break
  fi
done
```

### Expected Result

- Requests 1-200: **302 Found** (or 404 if code does not exist)
- Request 201: **429 Too Many Requests**
- Response includes `Retry-After` header with seconds until the window resets

### Assertions

1. The 201st request returns 429.
2. The `Retry-After` header is present and contains a positive integer.
3. After waiting for the retry period, requests succeed again.
4. Rate limiting is per-IP, not global (a different source IP can still make requests).

### Go Test

```go
func TestSEC006_RateLimitExceeded(t *testing.T) {
    if testing.Short() {
        t.Skip("rate limit test requires real Redis")
    }

    client := &http.Client{
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse // don't follow redirects
        },
    }

    var lastStatus int
    for i := 0; i < 201; i++ {
        req, _ := http.NewRequest(http.MethodGet, redirectURL+"/testcode", nil)
        req.Header.Set("X-Forwarded-For", "10.99.99.99") // fixed test IP
        resp, err := client.Do(req)
        require.NoError(t, err)
        resp.Body.Close()
        lastStatus = resp.StatusCode
    }

    assert.Equal(t, http.StatusTooManyRequests, lastStatus)
}
```

### Automation Notes

- Run with `make test-integration` (requires Redis via LocalStack).
- Verify that Redis `EVALSHA` sliding-window script is used, not a simple counter.
- Test the fail-closed behavior: stop Redis, then verify anonymous requests return 503 (not unthrottled 200).

---

## SEC-007: CSRF -- POST Password Form Without CSRF Token

**Category:** CSRF Protection
**Threat reference:** STRIDE T-005 (Tampering)
**Severity:** Medium

### Description

The password-protected link form (`POST /p/{code}`) must require a valid CSRF token. A cross-origin POST without the token must be rejected with 403 Forbidden.

### Manual Test

```bash
# First, get the form page to see the CSRF token
curl -s http://localhost:8081/p/testcode | grep csrf

# Submit without CSRF token
curl -s -w "\n%{http_code}" -X POST http://localhost:8081/p/testcode \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "password=test123"

# Submit with invalid CSRF token
curl -s -w "\n%{http_code}" -X POST http://localhost:8081/p/testcode \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "password=test123&csrf_token=invalid-token-here"
```

### Expected Result

- Without CSRF token: **403 Forbidden**
- With invalid CSRF token: **403 Forbidden**
- With valid CSRF token from the form: **302 Found** (redirect to target) or **401** (wrong password)

### Assertions

1. POST without `csrf_token` returns 403.
2. POST with a forged `csrf_token` returns 403.
3. POST with a valid `csrf_token` from a prior GET is accepted (returns 302 or 401 depending on password correctness).
4. Each CSRF token is single-use or time-limited.

### Go Test

```go
func TestSEC007_CSRFRequired(t *testing.T) {
    // POST without CSRF token
    form := url.Values{"password": {"test123"}}
    req, _ := http.NewRequest(http.MethodPost, redirectURL+"/p/testcode",
        strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSEC007_CSRFInvalidToken(t *testing.T) {
    form := url.Values{
        "password":   {"test123"},
        "csrf_token": {"forged-token-value"},
    }
    req, _ := http.NewRequest(http.MethodPost, redirectURL+"/p/testcode",
        strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
```

---

## SEC-008: Brute Force -- Password-Protected Link Lockout

**Category:** Brute Force Protection
**Threat reference:** STRIDE T-003 (Tampering)
**Severity:** High

### Description

Password-protected links must enforce a lockout after repeated failed attempts. After 5 wrong passwords for the same link from the same IP within 15 minutes, the 6th attempt must return 429 Too Many Requests.

### Manual Test

```bash
# Assumes a password-protected link exists with code "protlink"
# and a valid CSRF token is obtained for each request.

for i in $(seq 1 6); do
  # Get fresh CSRF token
  CSRF=$(curl -s http://localhost:8081/p/protlink | grep -oP 'value="\K[^"]+' | head -1)
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8081/p/protlink \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "password=wrong_password_${i}&csrf_token=${CSRF}")
  echo "Attempt $i: $STATUS"
done
# Expected: attempts 1-5 return 401, attempt 6 returns 429
```

### Expected Result

- Attempts 1-5: **401 Unauthorized** (wrong password)
- Attempt 6: **429 Too Many Requests**
- The `Retry-After` header indicates when the lockout expires (up to 15 minutes)

### Assertions

1. First 5 failed attempts return 401.
2. The 6th attempt returns 429.
3. After the lockout period, attempts are allowed again.
4. A correct password submitted during lockout still returns 429 (lockout is absolute).
5. A different IP can still attempt the password (lockout is per-IP, not per-link).

### Go Test

```go
func TestSEC008_PasswordBruteForce(t *testing.T) {
    if testing.Short() {
        t.Skip("brute force test requires real Redis for rate limiting")
    }

    client := &http.Client{
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }

    // Setup: create a password-protected link (via API)
    code := createPasswordProtectedLink(t, "correct-password")

    var statuses []int
    for i := 0; i < 6; i++ {
        csrfToken := getCSRFToken(t, client, redirectURL+"/p/"+code)
        form := url.Values{
            "password":   {fmt.Sprintf("wrong_%d", i)},
            "csrf_token": {csrfToken},
        }
        req, _ := http.NewRequest(http.MethodPost, redirectURL+"/p/"+code,
            strings.NewReader(form.Encode()))
        req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
        req.Header.Set("X-Forwarded-For", "10.88.88.88")

        resp, err := client.Do(req)
        require.NoError(t, err)
        resp.Body.Close()
        statuses = append(statuses, resp.StatusCode)
    }

    // First 5 should be 401, 6th should be 429
    for i := 0; i < 5; i++ {
        assert.Equal(t, http.StatusUnauthorized, statuses[i],
            "attempt %d should be 401", i+1)
    }
    assert.Equal(t, http.StatusTooManyRequests, statuses[5],
        "attempt 6 should be 429")
}
```

---

## SEC-009: Authorization -- Cross-User Resource Access

**Category:** Authorization
**Threat reference:** STRIDE E-003 (Elevation of Privilege)
**Severity:** Critical

### Description

User A must not be able to delete, update, or view detailed statistics for links owned by User B. The API must enforce ownership checks on all mutating and detail endpoints.

### Manual Test

```bash
# Create a link as User A
LINK_ID=$(curl -s -X POST http://localhost:8080/api/v1/links \
  -H "Authorization: Bearer $USER_A_JWT" \
  -H "Content-Type: application/json" \
  -d '{"original_url": "https://example.com"}' | jq -r '.code')

# Try to delete User A's link as User B
curl -s -w "\n%{http_code}" -X DELETE "http://localhost:8080/api/v1/links/$LINK_ID" \
  -H "Authorization: Bearer $USER_B_JWT"
# Expected: 403

# Try to update User A's link as User B
curl -s -w "\n%{http_code}" -X PATCH "http://localhost:8080/api/v1/links/$LINK_ID" \
  -H "Authorization: Bearer $USER_B_JWT" \
  -H "Content-Type: application/json" \
  -d '{"title": "hacked"}'
# Expected: 403
```

### Expected Result

- DELETE by non-owner: **403 Forbidden**
- PATCH by non-owner: **403 Forbidden**
- GET stats by non-owner: **403 Forbidden**
- GET redirect (public): **302 Found** (redirects are public, not ownership-restricted)

### Assertions

1. User B cannot delete User A's link (403).
2. User B cannot update User A's link (403).
3. User B cannot access User A's link statistics (403).
4. User A can still delete/update their own link (200/204).
5. The 403 response body does not reveal whether the link exists (prevents enumeration).

### Go Test

```go
func TestSEC009_CrossUserAccessDenied(t *testing.T) {
    // Create link as User A
    code := createLinkAsUser(t, userAJWT, "https://example.com")

    // Attempt deletion as User B
    req, _ := http.NewRequest(http.MethodDelete,
        baseURL+"/api/v1/links/"+code, nil)
    req.Header.Set("Authorization", "Bearer "+userBJWT)

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusForbidden, resp.StatusCode)

    // Attempt update as User B
    body := strings.NewReader(`{"title":"hacked"}`)
    req2, _ := http.NewRequest(http.MethodPatch,
        baseURL+"/api/v1/links/"+code, body)
    req2.Header.Set("Authorization", "Bearer "+userBJWT)
    req2.Header.Set("Content-Type", "application/json")

    resp2, err := http.DefaultClient.Do(req2)
    require.NoError(t, err)
    defer resp2.Body.Close()

    assert.Equal(t, http.StatusForbidden, resp2.StatusCode)

    // Verify User A can still delete (control test)
    req3, _ := http.NewRequest(http.MethodDelete,
        baseURL+"/api/v1/links/"+code, nil)
    req3.Header.Set("Authorization", "Bearer "+userAJWT)

    resp3, err := http.DefaultClient.Do(req3)
    require.NoError(t, err)
    defer resp3.Body.Close()

    assert.Equal(t, http.StatusNoContent, resp3.StatusCode)
}
```

---

## SEC-010: Data Leak -- No Raw IP in Logs or Responses

**Category:** Data Leak Prevention
**Threat reference:** STRIDE I-001 (Information Disclosure), privacy.md
**Severity:** Medium

### Description

Raw IP addresses must never appear in API responses, application logs, or DynamoDB records. All IP addresses must be stored as `SHA-256(IP + secret_salt)` per ADR-007. This test verifies that the full data pipeline (create, click, stats) does not leak raw IPs.

### Manual Test

```bash
# Step 1: Create a link
CODE=$(curl -s -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"original_url": "https://example.com"}' | jq -r '.code')

# Step 2: Click the link from a known IP
curl -s -o /dev/null http://localhost:8081/$CODE \
  -H "X-Forwarded-For: 203.0.113.42"

# Step 3: Check stats response for raw IP
STATS=$(curl -s http://localhost:8080/api/v1/links/$CODE/stats \
  -H "Authorization: Bearer $USER_JWT")
echo "$STATS" | grep -c "203.0.113.42"
# Expected: 0 (no matches)

# Step 4: Check DynamoDB directly
aws dynamodb query \
  --endpoint-url http://localhost:4566 \
  --table-name clicks \
  --key-condition-expression "PK = :pk" \
  --expression-attribute-values '{":pk":{"S":"LINK#'$CODE'"}}' \
  | grep -c "203.0.113.42"
# Expected: 0 (no matches)

# Step 5: Check application logs
docker logs shorty-api 2>&1 | grep -c "203.0.113.42"
# Expected: 0 (no matches)
```

### Expected Result

- Raw IP `203.0.113.42` does not appear in:
  - API response bodies (stats endpoints)
  - DynamoDB `clicks` table records
  - Application logs (stdout/stderr)
- Instead, a SHA-256 hash (64-char hex string) appears wherever an IP reference is needed

### Assertions

1. Stats API response contains `ip_hash` field (64-char hex), not `ip` or `ip_address`.
2. DynamoDB click records contain `ip_hash` attribute, not raw IP.
3. Application log output does not contain the test IP string.
4. The `ip_hash` value matches `SHA256("203.0.113.42" + salt)` where `salt` is the configured secret.

### Go Test

```go
func TestSEC010_NoRawIPInResponses(t *testing.T) {
    testIP := "203.0.113.42"

    // Create a link
    code := createLink(t, "https://example.com")

    // Click it with a known IP
    req, _ := http.NewRequest(http.MethodGet, redirectURL+"/"+code, nil)
    req.Header.Set("X-Forwarded-For", testIP)
    client := &http.Client{
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }
    resp, err := client.Do(req)
    require.NoError(t, err)
    resp.Body.Close()

    // Wait for async SQS processing
    time.Sleep(2 * time.Second)

    // Check stats endpoint
    statsReq, _ := http.NewRequest(http.MethodGet,
        baseURL+"/api/v1/links/"+code+"/stats", nil)
    statsReq.Header.Set("Authorization", "Bearer "+ownerJWT)

    statsResp, err := http.DefaultClient.Do(statsReq)
    require.NoError(t, err)
    defer statsResp.Body.Close()

    bodyBytes, _ := io.ReadAll(statsResp.Body)
    bodyStr := string(bodyBytes)

    assert.NotContains(t, bodyStr, testIP,
        "raw IP must not appear in stats response")
    assert.NotContains(t, bodyStr, "ip_address",
        "field name 'ip_address' should not exist")

    // Verify hashed IP is present (64-char hex)
    assert.Regexp(t, `"ip_hash"\s*:\s*"[a-f0-9]{64}"`, bodyStr,
        "ip_hash field with SHA-256 value expected")
}

func TestSEC010_NoRawIPInDynamoDB(t *testing.T) {
    testIP := "203.0.113.42"
    code := createAndClickLink(t, testIP)

    // Query DynamoDB directly
    result, err := dynamoClient.Query(context.Background(), &dynamodb.QueryInput{
        TableName:              aws.String("clicks"),
        KeyConditionExpression: aws.String("PK = :pk"),
        ExpressionAttributeValues: map[string]types.AttributeValue{
            ":pk": &types.AttributeValueMemberS{Value: "LINK#" + code},
        },
    })
    require.NoError(t, err)
    require.NotEmpty(t, result.Items)

    // Serialize all items and search for raw IP
    for _, item := range result.Items {
        itemJSON, _ := json.Marshal(item)
        assert.NotContains(t, string(itemJSON), testIP,
            "raw IP must not appear in DynamoDB click records")
    }
}
```

### Automation Notes

- Run this test as part of `make test-integration` with LocalStack.
- Use a recognizable test IP from the documentation range (RFC 5737: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) to avoid false positives.
- Consider scanning all DynamoDB table contents with a regex for IPv4 patterns (`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`) as a broader check.

---

## Summary Matrix

| ID | Category | Test | Expected | Severity | Automated |
|---|---|---|---|---|---|
| SEC-001 | Input Validation | `javascript:alert(1)` as URL | 400 Bad Request | High | Go unit + integration |
| SEC-002 | Input Validation | Private IP (10.0.0.1) as URL | 400 Bad Request | Critical | Go unit + integration |
| SEC-003 | Auth | Access /api/v1/links without JWT | 401 Unauthorized | High | Go integration |
| SEC-004 | Auth | Access /api/v1/links with expired JWT | 401 Unauthorized | High | Go integration |
| SEC-005 | Auth | JWT with `alg: none` | 401 Unauthorized | Critical | Go unit + integration |
| SEC-006 | Rate Limit | 201 requests in 1 min from same IP | 429 Too Many Requests | High | Go integration |
| SEC-007 | CSRF | POST /p/{code} without CSRF token | 403 Forbidden | Medium | Go integration |
| SEC-008 | Brute Force | 6 wrong passwords for same link | 429 Too Many Requests | High | Go integration |
| SEC-009 | Authz | User A deletes User B's link | 403 Forbidden | Critical | Go integration |
| SEC-010 | Data Leak | Logs/responses contain no raw IP | Pass (no PII found) | Medium | Go integration + scan |

---

## Test Infrastructure Requirements

### Test Helpers

All security tests share these helper functions (place in `tests/security/helpers_test.go`):

```go
package security_test

import (
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"
)

var (
    baseURL     = envOrDefault("SHORTY_API_URL", "http://localhost:8080")
    redirectURL = envOrDefault("SHORTY_REDIRECT_URL", "http://localhost:8081")
)

func envOrDefault(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}

func base64url(s string) string {
    return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func createLink(t *testing.T, originalURL string) string {
    t.Helper()
    body := fmt.Sprintf(`{"original_url":"%s"}`, originalURL)
    resp, err := http.Post(baseURL+"/api/v1/shorten", "application/json",
        strings.NewReader(body))
    require.NoError(t, err)
    defer resp.Body.Close()
    var result map[string]string
    json.NewDecoder(resp.Body).Decode(&result)
    return result["code"]
}

func getCSRFToken(t *testing.T, client *http.Client, formURL string) string {
    t.Helper()
    resp, err := client.Get(formURL)
    require.NoError(t, err)
    defer resp.Body.Close()
    bodyBytes, _ := io.ReadAll(resp.Body)
    // Extract CSRF token from hidden form field
    // Implementation depends on HTML template structure
    _ = bodyBytes
    return "" // TODO: parse from HTML
}
```

### CI Integration

Add to `.github/workflows/ci.yml`:

```yaml
security-tests:
  runs-on: ubuntu-latest
  needs: [build]
  services:
    localstack:
      image: localstack/localstack:3.0
    redis:
      image: redis:7-alpine
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: '1.22'
    - run: go test ./tests/security/... -v -count=1 -timeout 5m
```
