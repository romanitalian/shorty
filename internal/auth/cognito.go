package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CognitoConfig holds the configuration for Cognito JWT validation.
type CognitoConfig struct {
	Region     string
	UserPoolID string
	ClientID   string
}

// issuerURL returns the expected JWT issuer URL for this Cognito user pool.
func (c *CognitoConfig) issuerURL() string {
	return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", c.Region, c.UserPoolID)
}

// jwksURL returns the JWKS endpoint for this Cognito user pool.
func (c *CognitoConfig) jwksURL() string {
	return c.issuerURL() + "/.well-known/jwks.json"
}

// CognitoAuthenticator validates Cognito JWTs using cached JWKS keys.
type CognitoAuthenticator struct {
	config          CognitoConfig
	keys            map[string]*rsa.PublicKey
	mu              sync.RWMutex
	lastFetch       time.Time
	cacheTTL        time.Duration
	minRefreshInterval time.Duration // B3 fix: minimum interval between JWKS refreshes
	// httpClient is used to fetch JWKS; injectable for testing.
	httpClient HTTPClient
}

// HTTPClient abstracts HTTP GET requests for JWKS fetching.
type HTTPClient interface {
	Get(url string) (*http.Response, error)
}

// NewCognitoAuthenticator creates a new authenticator for the given Cognito config.
func NewCognitoAuthenticator(cfg CognitoConfig) *CognitoAuthenticator {
	return &CognitoAuthenticator{
		config:             cfg,
		keys:               make(map[string]*rsa.PublicKey),
		cacheTTL:           1 * time.Hour,
		minRefreshInterval: 60 * time.Second, // B3 fix: rate-limit JWKS refreshes
		httpClient:         &http.Client{Timeout: 5 * time.Second},
	}
}

// SetHTTPClient replaces the default HTTP client (useful for testing).
func (a *CognitoAuthenticator) SetHTTPClient(c HTTPClient) {
	a.httpClient = c
}

// cognitoClaims maps the JWT payload fields from a Cognito access token.
type cognitoClaims struct {
	jwt.RegisteredClaims
	ClientID string   `json:"client_id"`
	TokenUse string   `json:"token_use"`
	Scope    string   `json:"scope"`
	Email    string   `json:"email"`
	Groups   []string `json:"cognito:groups"`
}

// ValidateToken parses and validates a Cognito JWT, returning application-level Claims.
func (a *CognitoAuthenticator) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Parse the token with a key-finding function that looks up the JWKS.
	token, err := jwt.ParseWithClaims(tokenString, &cognitoClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Algorithm pinning: only RS256 is accepted.
		if token.Method.Alg() != "RS256" {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}

		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("missing kid in token header")
		}

		return a.getKey(kid)
	},
		jwt.WithIssuer(a.config.issuerURL()),
		jwt.WithLeeway(30*time.Second),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	cc, ok := token.Claims.(*cognitoClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// B1 fix: Validate token_use claim — only accept access tokens.
	if cc.TokenUse != "access" {
		return nil, fmt.Errorf("token_use must be 'access', got %q", cc.TokenUse)
	}

	// Audience check: Cognito access tokens use client_id instead of aud.
	if cc.ClientID != a.config.ClientID {
		// Also check standard audience.
		auds := cc.Audience
		found := false
		for _, aud := range auds {
			if aud == a.config.ClientID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("token audience mismatch")
		}
	}

	// Convert to application Claims.
	claims := &Claims{
		Subject: cc.Subject,
		Email:   cc.Email,
		Groups:  cc.Groups,
	}
	if cc.ExpiresAt != nil {
		claims.ExpiresAt = cc.ExpiresAt.Time
	}
	if cc.IssuedAt != nil {
		claims.IssuedAt = cc.IssuedAt.Time
	}

	return claims, nil
}

// getKey returns the RSA public key for the given key ID, refreshing the cache if needed.
func (a *CognitoAuthenticator) getKey(kid string) (*rsa.PublicKey, error) {
	a.mu.RLock()
	key, ok := a.keys[kid]
	stale := time.Since(a.lastFetch) > a.cacheTTL
	a.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}

	if err := a.refreshKeys(); err != nil {
		return nil, fmt.Errorf("failed to refresh JWKS: %w", err)
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	key, ok = a.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key ID %q not found in JWKS", kid)
	}
	return key, nil
}

// jwksResponse represents the JSON structure of a JWKS endpoint response.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// refreshKeys fetches the JWKS endpoint and updates the key cache.
func (a *CognitoAuthenticator) refreshKeys() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// B3 fix: rate-limit JWKS refreshes to prevent DoS via crafted kid values.
	if time.Since(a.lastFetch) < a.minRefreshInterval {
		return nil
	}

	resp, err := a.httpClient.Get(a.config.jwksURL())
	if err != nil {
		return fmt.Errorf("JWKS fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("JWKS decode failed: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Alg != "RS256" {
			continue
		}
		pub, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		newKeys[k.Kid] = pub
	}

	a.keys = newKeys
	a.lastFetch = time.Now()
	return nil
}

// parseRSAPublicKey constructs an RSA public key from base64url-encoded N and E.
func parseRSAPublicKey(nBase64, eBase64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nBase64)
	if err != nil {
		return nil, fmt.Errorf("decode N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eBase64)
	if err != nil {
		return nil, fmt.Errorf("decode E: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
