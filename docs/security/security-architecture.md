# Security Architecture -- Shorty URL Shortener

**Version:** 1.0
**Date:** 2026-04-05
**Author:** Security Engineer (S1-T04)
**References:** OWASP Top 10 2021, ADR-007 (IP Anonymization), threat-model.md

---

## 1. Defense in Depth Layers

```
Layer 1 -- Network:     CloudFront TLS termination (TLS 1.2 min, 1.3 preferred)
Layer 2 -- Edge:        AWS WAF (Bot Control, rate limiting, OWASP rules, geo-blocking)
Layer 3 -- Application: Input validation, URL safety check, rate limiter (Redis)
Layer 4 -- Authentication: Cognito JWT (RS256, short TTL, httpOnly cookie)
Layer 5 -- Authorization:  Per-user quota enforcement (DynamoDB atomic counter)
Layer 6 -- Data:        DynamoDB SSE (KMS CMK), Redis AUTH + TLS, SQS SSE
Layer 7 -- Audit:       Immutable click log, CloudTrail, CloudFront access logs
```

---

## 2. URL Validation Specification

URL validation is the most critical security control in Shorty. All validation happens at **creation time** (`POST /api/v1/links`, `POST /api/v1/shorten`), never at redirect time. This ensures malicious URLs are never stored.

**OWASP references:** A10:2021 SSRF, A03:2021 Injection

### 2.1 Blocked Schemes

Reject any URL whose scheme (case-insensitive) is not `http` or `https`:

```go
package validator

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// blockedSchemes lists URI schemes that must never be stored.
var blockedSchemes = map[string]bool{
	"javascript": true,
	"data":       true,
	"vbscript":   true,
	"file":       true,
	"ftp":        true,
	"gopher":     true,
	"telnet":     true,
	"ssh":        true,
}

// ErrBlockedScheme is returned when the URL uses a disallowed scheme.
var ErrBlockedScheme = fmt.Errorf("URL scheme is not allowed; only http and https are permitted")

// ValidateScheme checks that the URL uses http or https.
func ValidateScheme(rawURL string) error {
	// Normalize to catch tricks like "JAVASCRIPT:" or "Java\tScript:"
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return -1 // strip control chars and whitespace
		}
		return r
	}, rawURL)

	u, err := url.Parse(cleaned)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrBlockedScheme
	}

	if blockedSchemes[scheme] {
		return ErrBlockedScheme
	}

	return nil
}
```

### 2.2 Blocked IP Ranges (SSRF Prevention)

Block URLs that resolve to private, loopback, or link-local addresses. This prevents SSRF attacks targeting AWS metadata (`169.254.169.254`), internal services, or localhost.

```go
// privateRanges defines CIDR blocks that must be blocked.
var privateRanges = []string{
	"127.0.0.0/8",     // IPv4 loopback
	"10.0.0.0/8",      // RFC 1918
	"172.16.0.0/12",   // RFC 1918
	"192.168.0.0/16",  // RFC 1918
	"169.254.0.0/16",  // link-local (AWS metadata endpoint)
	"0.0.0.0/8",       // "this" network
	"100.64.0.0/10",   // carrier-grade NAT (RFC 6598)
	"192.0.0.0/24",    // IETF protocol assignments
	"198.18.0.0/15",   // benchmark testing
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved
	"::1/128",         // IPv6 loopback
	"fc00::/7",        // IPv6 unique local
	"fe80::/10",       // IPv6 link-local
	"::ffff:0:0/96",   // IPv4-mapped IPv6
}

var parsedPrivateRanges []*net.IPNet

func init() {
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %s: %v", cidr, err))
		}
		parsedPrivateRanges = append(parsedPrivateRanges, network)
	}
}

// IsPrivateIP checks whether an IP falls within any blocked range.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true // treat unparseable IPs as private (fail closed)
	}
	for _, network := range parsedPrivateRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateHost resolves the hostname and checks against private ranges.
// Must be called at link creation time.
func ValidateHost(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Block raw IP addresses in private ranges
	if ip := net.ParseIP(hostname); ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf("URL host resolves to a private/reserved IP address")
		}
		return nil
	}

	// Resolve hostname and check all returned IPs
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", hostname, err)
	}

	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return fmt.Errorf("URL host %q resolves to a private/reserved IP address", hostname)
		}
	}

	return nil
}
```

### 2.3 IDN Homograph Attack Detection

Internationalized Domain Names (IDN) can use visually similar Unicode characters to impersonate legitimate domains (e.g., `xn--pple-43d.com` looks like `apple.com`).

```go
// ValidateIDN checks for IDN homograph attacks.
// Rejects domains with mixed-script characters (e.g., Cyrillic + Latin).
func ValidateIDN(hostname string) error {
	// Convert to ASCII (punycode) for analysis
	ascii, err := idna.Lookup.ToASCII(hostname)
	if err != nil {
		return fmt.Errorf("invalid internationalized domain name: %w", err)
	}

	// If the ASCII form starts with "xn--", it is a punycode domain.
	// Convert back to Unicode and check for mixed scripts.
	if strings.HasPrefix(ascii, "xn--") || containsLabel(ascii, "xn--") {
		unicode, err := idna.Lookup.ToUnicode(ascii)
		if err != nil {
			return fmt.Errorf("invalid punycode domain: %w", err)
		}

		if hasMixedScripts(unicode) {
			return fmt.Errorf(
				"internationalized domain %q uses mixed scripts (potential homograph attack)",
				unicode,
			)
		}
	}

	return nil
}

// containsLabel checks if any label in a dotted domain starts with the prefix.
func containsLabel(domain, prefix string) bool {
	for _, label := range strings.Split(domain, ".") {
		if strings.HasPrefix(label, prefix) {
			return true
		}
	}
	return false
}

// hasMixedScripts detects domains using characters from multiple Unicode scripts.
// A domain mixing Latin and Cyrillic (e.g., "аpple.com") is suspicious.
func hasMixedScripts(domain string) bool {
	scripts := make(map[string]bool)
	for _, r := range domain {
		if r == '.' || r == '-' {
			continue
		}
		switch {
		case unicode.Is(unicode.Latin, r):
			scripts["Latin"] = true
		case unicode.Is(unicode.Cyrillic, r):
			scripts["Cyrillic"] = true
		case unicode.Is(unicode.Greek, r):
			scripts["Greek"] = true
		case unicode.Is(unicode.Han, r):
			scripts["Han"] = true
		case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			scripts["Japanese"] = true
		case unicode.Is(unicode.Hangul, r):
			scripts["Korean"] = true
		case unicode.Is(unicode.Arabic, r):
			scripts["Arabic"] = true
		case unicode.Is(unicode.Common, r) || unicode.Is(unicode.Number, r):
			// digits, hyphens -- common to all scripts
			continue
		default:
			scripts["Other"] = true
		}
	}
	return len(scripts) > 1
}
```

### 2.4 Complete URL Validation Pipeline

```go
// MaxURLLength is the maximum allowed URL length.
const MaxURLLength = 2048

// ValidateURL runs the full validation pipeline. Call at link creation time.
func ValidateURL(rawURL string) error {
	// Step 1: Length check
	if len(rawURL) > MaxURLLength {
		return fmt.Errorf("URL exceeds maximum length of %d characters", MaxURLLength)
	}
	if len(rawURL) == 0 {
		return fmt.Errorf("URL is required")
	}

	// Step 2: Scheme validation (blocks javascript:, data:, etc.)
	if err := ValidateScheme(rawURL); err != nil {
		return err
	}

	// Step 3: Parse URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Step 4: Host must be present
	if u.Host == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	hostname := u.Hostname()

	// Step 5: IDN homograph check
	if err := ValidateIDN(hostname); err != nil {
		return err
	}

	// Step 6: SSRF check (private IP ranges)
	if err := ValidateHost(hostname); err != nil {
		return err
	}

	return nil
}
```

---

## 3. Input Validation for All API Fields

**OWASP reference:** A03:2021 Injection

All input validation is enforced in the Lambda handler **after** OpenAPI schema-level validation at API Gateway. The application must not trust API Gateway validation alone.

### 3.1 Field Validation Rules

| Field | Type | Max Length | Pattern/Constraint | Sanitization |
|---|---|---|---|---|
| `url` (original_url) | string | 2,048 | Valid URI, http/https only, passes URL validation pipeline | None (stored as-is after validation) |
| `custom_code` (alias) | string | 32 | `^[a-zA-Z0-9]{3,32}$` (alphanumeric only) | None (regex rejects special chars) |
| `title` | string | 255 | Any UTF-8 | HTML-entity encode on output only |
| `password` | string | 128 | Min 4 chars, max 128 | Hashed with bcrypt before storage |
| `expires_at` | string | -- | ISO 8601 datetime, must be in the future | Parsed to Unix timestamp |
| `max_clicks` | integer | -- | 1 to 1,000,000 | Validated range |
| `utm_source` | string | 128 | `^[a-zA-Z0-9_-]+$` | None (regex rejects special chars) |
| `utm_medium` | string | 128 | `^[a-zA-Z0-9_-]+$` | None |
| `utm_campaign` | string | 128 | `^[a-zA-Z0-9_-]+$` | None |
| `code` (path param) | string | 32 | `^[a-zA-Z0-9]{3,32}$` | None |
| `cursor` (query param) | string | 256 | Base64-encoded JSON | Decoded and validated server-side |
| `limit` (query param) | integer | -- | 1 to 100 | Clamped to range |
| `from` / `to` (query) | string | -- | ISO 8601 date | Parsed, max range 1 year |
| `password` (form, `/p/{code}`) | string | 128 | Min 1, max 128 | Compared via bcrypt, never stored again |
| `csrf_token` (form) | string | 64 | HMAC token | Validated via HMAC comparison |

### 3.2 Request Body Size Limit

```go
// MaxRequestBodySize limits the request body to 10 KB.
const MaxRequestBodySize = 10 * 1024

func LimitBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
		next.ServeHTTP(w, r)
	})
}
```

---

## 4. Output Encoding (XSS Prevention)

**OWASP reference:** A03:2021 Injection

### 4.1 JSON Responses

Go's `encoding/json` automatically escapes `<`, `>`, and `&` in string values. No additional encoding is needed for JSON API responses.

### 4.2 HTML Responses (Password Form)

The password form (`GET /{code}` returns 403 with HTML, `POST /p/{code}` returns error form) must use Go's `html/template` package, which auto-escapes all interpolated values.

```go
import "html/template"

var passwordFormTmpl = template.Must(template.New("password").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Password Required</title>
</head>
<body>
    <h1>Password Required</h1>
    {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
    <form method="POST" action="/p/{{.Code}}">
        <label for="password">Password:</label>
        <input type="password" id="password" name="password"
               maxlength="128" required autocomplete="off">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <button type="submit">Submit</button>
    </form>
</body>
</html>
`))
```

All values (`.Error`, `.Code`, `.CSRFToken`) are auto-escaped by `html/template`. Never use `text/template` for HTML output.

### 4.3 Redirect Responses

The `Location` header in 301/302 responses contains the `original_url` which has already been validated at creation time. No additional encoding is needed for the `Location` header value -- it is a validated URI.

**Do not** render the original URL in an HTML body without escaping. If a "you are being redirected to..." page is ever added, use `html/template`.

---

## 5. Security Headers

All Lambda responses must include these headers. Applied via middleware.

```go
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent clickjacking
		h.Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		h.Set("X-Content-Type-Options", "nosniff")

		// HSTS: 1 year, include subdomains, preload-eligible
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

		// Referrer: send origin only on cross-origin requests
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions: disable all unnecessary browser features
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// CSP: restrictive policy for HTML responses
		h.Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'none'",
			"script-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"connect-src 'self'",
			"frame-ancestors 'none'",
			"form-action 'self'",
			"base-uri 'self'",
		}, "; "))

		next.ServeHTTP(w, r)
	})
}
```

**Note:** For redirect responses (302), these headers have limited effect since the browser immediately follows the `Location` header. They are critical for the password form (HTML response) and any error pages.

---

## 6. CSRF Protection

**OWASP reference:** A01:2021 Broken Access Control

CSRF protection applies to the password form at `POST /p/{code}`. API endpoints use JWT Bearer tokens (not cookies for auth decisions) and are not vulnerable to CSRF.

### 6.1 Cookie Configuration

```go
func SetAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,           // not accessible via JavaScript
		Secure:   true,           // HTTPS only
		SameSite: http.SameSiteStrictMode, // no cross-site sending
		MaxAge:   3600,           // 1 hour (matches JWT expiry)
	})
}
```

### 6.2 CSRF Token Generation and Validation

```go
import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// csrfKey is loaded from Secrets Manager at Lambda init.
var csrfKey []byte

// GenerateCSRFToken creates an HMAC-signed token with a timestamp.
func GenerateCSRFToken(sessionID string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)

	payload := ts + ":" + base64.RawURLEncoding.EncodeToString(nonce) + ":" + sessionID
	mac := hmac.New(sha256.New, csrfKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payload + ":" + sig
}

// ValidateCSRFToken verifies the token and checks freshness (15 min max).
func ValidateCSRFToken(token, sessionID string) error {
	parts := strings.SplitN(token, ":", 4)
	if len(parts) != 4 {
		return fmt.Errorf("invalid CSRF token format")
	}

	ts, nonce, sid, sig := parts[0], parts[1], parts[2], parts[3]

	// Verify session binding
	if sid != sessionID {
		return fmt.Errorf("CSRF token session mismatch")
	}

	// Verify signature (timing-safe comparison)
	payload := ts + ":" + nonce + ":" + sid
	mac := hmac.New(sha256.New, csrfKey)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return fmt.Errorf("CSRF token signature invalid")
	}

	// Verify freshness
	timestamp, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("CSRF token timestamp invalid")
	}
	if time.Since(time.Unix(timestamp, 0)) > 15*time.Minute {
		return fmt.Errorf("CSRF token expired")
	}

	return nil
}
```

---

## 7. JWT Security

**OWASP reference:** A07:2021 Identification and Authentication Failures

### 7.1 JWT Validation Requirements

| Check | Requirement |
|---|---|
| Algorithm | RS256 only. Reject `none`, `HS256`, `HS384`, `HS512`, `ES256`, `PS256`. |
| Issuer (`iss`) | Must match `https://cognito-idp.{region}.amazonaws.com/{userPoolId}` |
| Audience (`aud`) | Must match Cognito app client ID |
| Expiration (`exp`) | Must be in the future (with 30-second clock skew tolerance) |
| Issued at (`iat`) | Must be in the past |
| Token use (`token_use`) | Must be `access` (not `id`) for API authorization |
| Key ID (`kid`) | Must match a key in the Cognito JWKS |
| Signature | Verified against the Cognito JWKS public key |

### 7.2 JWKS Key Caching

```go
import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// JWKSCache caches Cognito public keys with automatic refresh.
type JWKSCache struct {
	mu       sync.RWMutex
	keys     map[string]*rsa.PublicKey
	jwksURL  string
	lastFetch time.Time
	cacheTTL  time.Duration
}

func NewJWKSCache(region, userPoolID string) *JWKSCache {
	return &JWKSCache{
		keys:     make(map[string]*rsa.PublicKey),
		jwksURL:  fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s/.well-known/jwks.json", region, userPoolID),
		cacheTTL: 1 * time.Hour,
	}
}

// GetKey returns the RSA public key for the given key ID.
// Refreshes the cache if the key is not found or the cache is stale.
func (c *JWKSCache) GetKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	stale := time.Since(c.lastFetch) > c.cacheTTL
	c.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}

	// Refresh the cache
	if err := c.refresh(); err != nil {
		return nil, fmt.Errorf("failed to refresh JWKS: %w", err)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	key, ok = c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key ID %q not found in JWKS", kid)
	}
	return key, nil
}

func (c *JWKSCache) refresh() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(c.lastFetch) < 5*time.Second {
		return nil // another goroutine just refreshed
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(c.jwksURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Alg != "RS256" {
			continue // skip non-RSA keys
		}
		pub, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		newKeys[k.Kid] = pub
	}

	c.keys = newKeys
	c.lastFetch = time.Now()
	return nil
}

func parseRSAPublicKey(nBase64, eBase64 string) (*rsa.PublicKey, error) {
	// Base64url decode n and e, construct rsa.PublicKey
	// (implementation uses encoding/base64.RawURLEncoding)
	nBytes, err := base64.RawURLEncoding.DecodeString(nBase64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eBase64)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
```

### 7.3 Token Validation (Algorithm Pinning)

```go
import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ValidateJWT validates a Cognito JWT with strict security checks.
func ValidateJWT(tokenString string, cache *JWKSCache, expectedAud, expectedIss string) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode header
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid JWT header encoding")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("invalid JWT header")
	}

	// CRITICAL: Algorithm pinning -- only RS256 is accepted.
	// Prevents algorithm confusion attacks (CVE-2015-9235).
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("JWT algorithm %q is not allowed; only RS256 is accepted", header.Alg)
	}

	// Get the public key for this key ID
	pubKey, err := cache.GetKey(header.Kid)
	if err != nil {
		return nil, fmt.Errorf("JWT key lookup failed: %w", err)
	}

	// Verify signature
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid JWT signature encoding")
	}

	hash := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature); err != nil {
		return nil, fmt.Errorf("JWT signature verification failed")
	}

	// Decode claims
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid JWT claims encoding")
	}

	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid JWT claims")
	}

	// Validate claims
	now := time.Now()
	clockSkew := 30 * time.Second

	if claims.Exp == 0 || time.Unix(claims.Exp, 0).Add(clockSkew).Before(now) {
		return nil, fmt.Errorf("JWT has expired")
	}

	if claims.Iat != 0 && time.Unix(claims.Iat, 0).After(now.Add(clockSkew)) {
		return nil, fmt.Errorf("JWT issued in the future")
	}

	if claims.Iss != expectedIss {
		return nil, fmt.Errorf("JWT issuer mismatch")
	}

	if claims.Aud != expectedAud && claims.ClientID != expectedAud {
		return nil, fmt.Errorf("JWT audience mismatch")
	}

	if claims.TokenUse != "access" {
		return nil, fmt.Errorf("JWT token_use must be 'access', got %q", claims.TokenUse)
	}

	return &claims, nil
}

// Claims represents the validated JWT payload.
type Claims struct {
	Sub      string `json:"sub"`
	Iss      string `json:"iss"`
	Aud      string `json:"aud"`
	ClientID string `json:"client_id"` // Cognito uses client_id instead of aud for access tokens
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
	TokenUse string `json:"token_use"`
	Scope    string `json:"scope"`
}
```

---

## 8. Password Hashing

**OWASP reference:** A02:2021 Cryptographic Failures

### 8.1 bcrypt Configuration

```go
import (
	"crypto/subtle"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost is the work factor for password hashing.
// Cost 12 produces ~250ms hash time on ARM64 Graviton -- acceptable for
// link creation (p99 target 300ms) but expensive enough to deter brute-force.
const BcryptCost = 12

// HashPassword generates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	if len(password) < 4 || len(password) > 128 {
		return "", fmt.Errorf("password must be 4-128 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword compares a password against a bcrypt hash.
// Uses bcrypt's built-in timing-safe comparison.
func VerifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// TimingSafeEqual compares two strings in constant time.
// Used for CSRF tokens, API keys, and other secret comparisons.
func TimingSafeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
```

---

## 9. Brute-Force Protection

**OWASP reference:** A07:2021 Identification and Authentication Failures

### 9.1 Password Attempt Rate Limiting

The password form (`POST /p/{code}`) is rate-limited to 5 attempts per 15 minutes per IP. This uses the same Redis sliding window mechanism as the redirect rate limiter.

```go
// Rate limiter key for password attempts: rl:pwd:{code}:{ip_hash}
// Limit: 5 attempts per 900 seconds (15 minutes)

func CheckPasswordRateLimit(ctx context.Context, redis *redis.Client, code, ipHash string) (bool, int, error) {
	key := fmt.Sprintf("rl:pwd:%s:%s", code, ipHash)
	return slidingWindowCheck(ctx, redis, key, 5, 900*time.Second)
}
```

After 5 failed attempts, the endpoint returns HTTP 429 with a `Retry-After` header indicating when the window resets.

### 9.2 Account Lockout

Cognito User Pool handles account lockout for email/password authentication:
- 5 failed login attempts -> temporary lockout (Cognito-managed, exponential backoff).
- No permanent lockout (prevents denial-of-service against user accounts).

---

## 10. Open Redirect Prevention

**OWASP reference:** A01:2021 Broken Access Control

Open redirect is prevented by validating the `original_url` at **creation time**, not at redirect time. The redirect Lambda simply reads the stored URL and issues a `Location` header.

Key controls:
1. Only `http://` and `https://` schemes are accepted (Section 2.1).
2. Private IP ranges are blocked (Section 2.2).
3. The redirect Lambda never fetches the target URL -- the *browser* follows the redirect.
4. Google Safe Browsing API is checked asynchronously at creation time (non-blocking).

The `original_url` stored in DynamoDB is treated as validated and trusted. If validation logic is updated (e.g., new blocked pattern), a background job should scan existing URLs.

---

## 11. SSRF Prevention

**OWASP reference:** A10:2021 SSRF

SSRF is a critical concern because Shorty accepts arbitrary URLs from users. The primary defense is URL validation at creation time (Section 2.2).

Additional SSRF controls:
1. The redirect Lambda does **not** fetch the target URL. It issues an HTTP redirect, and the browser follows it. This eliminates the most common SSRF vector.
2. If a URL preview/unfurl feature is added in the future, it **must** use a sandboxed HTTP client with:
   - DNS resolution check against private ranges
   - Connection timeout of 5 seconds
   - Response size limit of 1 MB
   - No automatic redirect following
   - Separate IAM role with no access to AWS metadata service

---

## 12. Dependency Scanning

**OWASP reference:** A06:2021 Vulnerable and Outdated Components

### 12.1 govulncheck (Go Vulnerability Database)

```bash
# CI gate: fail on any known vulnerability in used code paths
govulncheck ./...
```

govulncheck uses call graph analysis -- it only reports vulnerabilities in code paths actually reachable from the application. This reduces false positives compared to module-level scanning.

### 12.2 gosec (Go Static Security Analysis)

```bash
# CI gate: fail on medium+ severity findings
gosec -fmt=sarif -out=gosec.sarif -severity=medium -confidence=medium ./...
```

Key rules for Shorty:
- **G107** (URL taint): flags any user-controlled URL passed to `http.Get()` (SSRF detection).
- **G401/G501** (weak crypto): ensures SHA-256+ for IP hashing (not MD5/SHA1).
- **G104** (unhandled errors): critical for DynamoDB and Redis error handling.
- **G114** (HTTP server with no timeout): ensures all HTTP clients have timeouts.

### 12.3 Trivy (Container + Dependency)

```bash
# CI gate: fail on HIGH or CRITICAL
trivy fs . --severity HIGH,CRITICAL --exit-code 1
```

### 12.4 CI Integration

All three scanners run in the `check` CI stage. Results are uploaded as SARIF to GitHub Code Scanning (Advanced Security) for centralized tracking.

```yaml
# .github/workflows/ci.yml (security stage)
security:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with: { go-version-file: go.mod }
    - run: go install golang.org/x/vuln/cmd/govulncheck@latest
    - run: govulncheck ./...
    - run: gosec -fmt=sarif -out=gosec.sarif ./...
    - uses: github/codeql-action/upload-sarif@v3
      with: { sarif_file: gosec.sarif }
      if: always()
    - run: |
        curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh
        trivy fs . --severity HIGH,CRITICAL --exit-code 1
```

---

## 13. Summary of Security Controls by Attack Vector

| Attack Vector | Primary Control | Secondary Control | Monitoring |
|---|---|---|---|
| SSRF | URL validation (private IP blocking) | Lambda does not fetch URLs | gosec G107 rule |
| XSS | CSP header, html/template auto-escaping | X-Content-Type-Options: nosniff | DAST (ZAP) |
| Open Redirect | URL scheme validation at creation | Only http/https stored | Safe Browsing API |
| CSRF | SameSite=Strict + CSRF token on forms | Bearer tokens for API (immune) | WAF logs |
| JWT Forgery | RS256 algorithm pinning + JWKS validation | Short TTL (1h) + refresh rotation | Auth failure rate alert |
| Password Brute-force | 5 attempts/15min/IP rate limit | bcrypt cost 12 (250ms/hash) | Rate limit hit rate alert |
| NoSQL Injection | AWS SDK expression package (parameterized) | Input regex validation | gosec scan |
| DDoS | WAF rate rules + Shield Standard | CloudFront edge absorption | Request rate alert |
| Data Leakage | IP hashing (ADR-007) | Encryption at rest (KMS CMK) | CloudTrail |
| Credential Stuffing | Cognito adaptive auth | WAF rate limits on auth endpoints | Failed login rate alert |
| Dependency Vulnerabilities | govulncheck + Trivy in CI | GitHub Dependabot alerts | Weekly scan schedule |
| IDN Homograph | Mixed-script detection | Punycode normalization | Creation rejection logs |
