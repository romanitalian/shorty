package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- test helpers ---

// testKeyPair generates an RSA key pair for testing.
func testKeyPair(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return priv, &priv.PublicKey
}

// jwksJSON builds a JWKS JSON response containing the given public key.
func jwksJSON(t *testing.T, kid string, pub *rsa.PublicKey) []byte {
	t.Helper()
	nBase64 := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eBase64 := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())

	resp := jwksResponse{
		Keys: []jwkKey{
			{
				Kid: kid,
				Alg: "RS256",
				Kty: "RSA",
				N:   nBase64,
				E:   eBase64,
			},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	return b
}

// mockHTTPClient returns a fixed response body for any GET request.
type mockHTTPClient struct {
	body       []byte
	statusCode int
}

func (m *mockHTTPClient) Get(_ string) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(bytes.NewReader(m.body)),
	}, nil
}

// signToken creates a signed JWT string using the given private key and claims map.
func signToken(t *testing.T, priv *rsa.PrivateKey, kid string, claimsMap map[string]interface{}) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims(claimsMap))
	token.Header["kid"] = kid

	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// newTestAuthenticator creates a CognitoAuthenticator wired to a mock JWKS endpoint.
func newTestAuthenticator(t *testing.T, priv *rsa.PrivateKey, pub *rsa.PublicKey, kid string) *CognitoAuthenticator {
	t.Helper()
	cfg := CognitoConfig{
		Region:     "us-east-1",
		UserPoolID: "us-east-1_TestPool",
		ClientID:   "test-client-id",
	}
	auth := NewCognitoAuthenticator(cfg)
	auth.SetHTTPClient(&mockHTTPClient{
		body:       jwksJSON(t, kid, pub),
		statusCode: http.StatusOK,
	})
	return auth
}

// validClaims returns a claims map that passes all validation checks.
func validClaims(issuer, clientID, sub string) map[string]interface{} {
	now := time.Now()
	return map[string]interface{}{
		"sub":             sub,
		"iss":             issuer,
		"client_id":       clientID,
		"token_use":       "access",
		"exp":             now.Add(1 * time.Hour).Unix(),
		"iat":             now.Add(-1 * time.Minute).Unix(),
		"email":           "user@example.com",
		"cognito:groups":  []string{"free-tier"},
	}
}

// --- tests ---

func TestValidateToken_ValidJWT(t *testing.T) {
	kid := "test-kid-1"
	priv, pub := testKeyPair(t)
	auth := newTestAuthenticator(t, priv, pub, kid)

	claims := validClaims(auth.config.issuerURL(), auth.config.ClientID, "user-uuid-123")
	tokenStr := signToken(t, priv, kid, claims)

	result, err := auth.ValidateToken(context.Background(), tokenStr)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if result.Subject != "user-uuid-123" {
		t.Errorf("expected subject %q, got %q", "user-uuid-123", result.Subject)
	}
	if result.Email != "user@example.com" {
		t.Errorf("expected email %q, got %q", "user@example.com", result.Email)
	}
	if len(result.Groups) != 1 || result.Groups[0] != "free-tier" {
		t.Errorf("expected groups [free-tier], got %v", result.Groups)
	}
	if result.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}
}

func TestValidateToken_ExpiredJWT(t *testing.T) {
	kid := "test-kid-2"
	priv, pub := testKeyPair(t)
	auth := newTestAuthenticator(t, priv, pub, kid)

	claims := validClaims(auth.config.issuerURL(), auth.config.ClientID, "user-uuid-expired")
	// Set expiration to 2 minutes in the past (beyond 30s leeway).
	claims["exp"] = time.Now().Add(-2 * time.Minute).Unix()

	tokenStr := signToken(t, priv, kid, claims)

	_, err := auth.ValidateToken(context.Background(), tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateToken_WrongAudience(t *testing.T) {
	kid := "test-kid-3"
	priv, pub := testKeyPair(t)
	auth := newTestAuthenticator(t, priv, pub, kid)

	claims := validClaims(auth.config.issuerURL(), "wrong-client-id", "user-uuid-aud")
	tokenStr := signToken(t, priv, kid, claims)

	_, err := auth.ValidateToken(context.Background(), tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong audience, got nil")
	}
}

func TestMiddleware_NoToken(t *testing.T) {
	auth := &stubAuthenticator{}
	handler := Middleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should proceed as anonymous.
		uid := UserIDFromContext(r.Context())
		if uid != "" {
			t.Errorf("expected empty user ID for anonymous, got %q", uid)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	auth := &stubAuthenticator{
		claims: &Claims{
			Subject: "user-123",
			Email:   "user@example.com",
			Groups:  []string{"free-tier"},
		},
	}
	handler := Middleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			t.Error("expected claims in context")
		}
		if claims.Subject != "user-123" {
			t.Errorf("expected subject %q, got %q", "user-123", claims.Subject)
		}
		if r.Header.Get("X-User-Id") != "user-123" {
			t.Errorf("expected X-User-Id header %q, got %q", "user-123", r.Header.Get("X-User-Id"))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	auth := &stubAuthenticator{
		err: fmt.Errorf("token is invalid"),
	}
	handler := Middleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called for invalid token")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuth_NoClaims(t *testing.T) {
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called without claims")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuth_WithClaims(t *testing.T) {
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &Claims{Subject: "user-456", Email: "test@example.com"}
	ctx := context.WithValue(context.Background(), claimsKey, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_SessionCookie(t *testing.T) {
	auth := &stubAuthenticator{
		claims: &Claims{
			Subject: "cookie-user",
			Email:   "cookie@example.com",
		},
	}
	handler := Middleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := UserIDFromContext(r.Context())
		if uid != "cookie-user" {
			t.Errorf("expected user ID %q, got %q", "cookie-user", uid)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "some-jwt-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- stub authenticator for middleware tests ---

type stubAuthenticator struct {
	claims *Claims
	err    error
}

func (s *stubAuthenticator) ValidateToken(_ context.Context, _ string) (*Claims, error) {
	return s.claims, s.err
}
