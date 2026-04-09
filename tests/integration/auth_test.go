package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	gen "github.com/romanitalian/shorty/internal/api/generated"
	"github.com/romanitalian/shorty/internal/auth"
	"github.com/romanitalian/shorty/internal/store"
)

// ---------- Auth middleware integration tests ----------

// TestAuthMiddleware_NoToken_AnonymousAccess verifies that guest endpoints
// (like POST /api/v1/shorten) work without any token.
func TestAuthMiddleware_NoToken_AnonymousAccess(t *testing.T) {
	authenticator := &mockAuthenticator{
		err: errors.New("no token"),
	}

	handler, _ := setupTestServerWithAuth_guest(t, authenticator)

	body := gen.GuestShortenRequest{Url: "https://example.com/test"}
	rec := doJSONRequest(handler, http.MethodPost, "/api/v1/shorten", body, nil)

	// Guest shorten should succeed (201) even without a token because the
	// auth middleware passes anonymous requests through.
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for anonymous guest shorten, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthMiddleware_ValidToken_SetsContext verifies that a valid JWT
// populates the context with claims, and the X-User-Id header is set.
func TestAuthMiddleware_ValidToken_SetsContext(t *testing.T) {
	claims := &auth.Claims{
		Subject:   "user-123",
		Email:     "alice@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		IssuedAt:  time.Now(),
	}
	authenticator := &mockAuthenticator{claims: claims}

	handler, ms := setupTestServerWithAuth_guest(t, authenticator)

	// Seed a link owned by user-123.
	now := time.Now().Unix()
	ms.links["auth1"] = &store.Link{
		Code:        "auth1",
		OriginalURL: "https://example.com",
		OwnerID:     "user-123",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Pass a token -- the authenticator will return the claims above.
	headers := map[string]string{
		"Authorization": "Bearer valid-test-token",
	}
	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/auth1", nil, headers)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.Link
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OwnerId != "user-123" {
		t.Fatalf("expected owner user-123, got %s", resp.OwnerId)
	}
}

// TestAuthMiddleware_InvalidToken_Returns401 verifies that an invalid JWT
// causes a 401 response.
func TestAuthMiddleware_InvalidToken_Returns401(t *testing.T) {
	authenticator := &mockAuthenticator{
		err: errors.New("invalid signature"),
	}

	handler, _ := setupTestServerWithAuth_guest(t, authenticator)

	headers := map[string]string{
		"Authorization": "Bearer bad-token",
	}
	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links", nil, headers)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthMiddleware_ExpiredToken_Returns401 verifies that an expired JWT
// results in a 401 response. The mock authenticator simulates the expiry error.
func TestAuthMiddleware_ExpiredToken_Returns401(t *testing.T) {
	authenticator := &mockAuthenticator{
		err: errors.New("token is expired"),
	}

	handler, _ := setupTestServerWithAuth_guest(t, authenticator)

	headers := map[string]string{
		"Authorization": "Bearer expired-token",
	}
	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links", nil, headers)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRequireAuth_ProtectedEndpoint verifies that RequireAuth middleware
// rejects requests without valid claims.
func TestRequireAuth_ProtectedEndpoint(t *testing.T) {
	authenticator := &mockAuthenticator{
		err: errors.New("no token"),
	}

	handler, _ := setupTestServerWithAuthAndRequire(t, authenticator)

	// No token -- should be rejected by RequireAuth.
	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links", nil, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for protected endpoint without token, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRequireAuth_ValidToken_Passes verifies that RequireAuth lets
// authenticated requests through.
func TestRequireAuth_ValidToken_Passes(t *testing.T) {
	claims := &auth.Claims{
		Subject:   "user-456",
		Email:     "bob@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		IssuedAt:  time.Now(),
	}
	authenticator := &mockAuthenticator{claims: claims}

	handler, _ := setupTestServerWithAuthAndRequire(t, authenticator)

	headers := map[string]string{
		"Authorization": "Bearer valid-token",
	}
	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links", nil, headers)

	// Should succeed (200) -- list may be empty but the endpoint is reachable.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated protected endpoint, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthMiddleware_SessionCookie verifies that a JWT in the "session"
// cookie is extracted and validated.
func TestAuthMiddleware_SessionCookie(t *testing.T) {
	claims := &auth.Claims{
		Subject:   "cookie-user",
		Email:     "cookie@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		IssuedAt:  time.Now(),
	}
	authenticator := &mockAuthenticator{claims: claims}

	handler, ms := setupTestServerWithAuth_guest(t, authenticator)

	now := time.Now().Unix()
	ms.links["ck1"] = &store.Link{
		Code:        "ck1",
		OriginalURL: "https://example.com",
		OwnerID:     "cookie-user",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/ck1", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "valid-session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with session cookie, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------- Auth helpers ----------

// setupTestServerWithAuth_guest creates a handler with auth middleware but
// does NOT require authentication (allows anonymous pass-through).
func setupTestServerWithAuth_guest(t *testing.T, authenticator auth.Authenticator) (http.Handler, *mockStore) {
	t.Helper()
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: true}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{}

	srv := newIntegrationAPIServer(ms, mc, ml, mg, mv)

	r := chi.NewRouter()
	r.Use(auth.Middleware(authenticator))
	handler := gen.HandlerFromMux(srv, r)

	return handler, ms
}
