package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	gen "github.com/romanitalian/shorty/internal/api/generated"
	"github.com/romanitalian/shorty/internal/auth"
	"github.com/romanitalian/shorty/internal/cache"
	"github.com/romanitalian/shorty/internal/ratelimit"
	"github.com/romanitalian/shorty/internal/shortener"
	"github.com/romanitalian/shorty/internal/store"
	"github.com/romanitalian/shorty/internal/validator"
)

// ---------- Mock Store ----------

type mockStore struct {
	links       map[string]*store.Link
	statsMap    map[string]*store.LinkStats
	timelineMap map[string][]store.TimelineBucket
	geoMap      map[string][]store.GeoStat
	referrerMap map[string][]store.ReferrerStat
}

func newMockStore() *mockStore {
	return &mockStore{
		links:       make(map[string]*store.Link),
		statsMap:    make(map[string]*store.LinkStats),
		timelineMap: make(map[string][]store.TimelineBucket),
		geoMap:      make(map[string][]store.GeoStat),
		referrerMap: make(map[string][]store.ReferrerStat),
	}
}

func (m *mockStore) CreateLink(_ context.Context, link *store.Link) error {
	if _, exists := m.links[link.Code]; exists {
		return store.ErrCodeCollision
	}
	m.links[link.Code] = link
	return nil
}

func (m *mockStore) GetLink(_ context.Context, code string) (*store.Link, error) {
	l, ok := m.links[code]
	if !ok {
		return nil, store.ErrLinkNotFound
	}
	return l, nil
}

func (m *mockStore) UpdateLink(_ context.Context, code string, callerID string, updates map[string]interface{}) error {
	l, ok := m.links[code]
	if !ok || l.OwnerID != callerID {
		return store.ErrLinkNotFound
	}
	if v, ok := updates["title"]; ok {
		l.Title = v.(string)
	}
	if v, ok := updates["is_active"]; ok {
		l.IsActive = v.(bool)
	}
	l.UpdatedAt = time.Now().Unix()
	return nil
}

func (m *mockStore) DeleteLink(_ context.Context, code string, callerID string) error {
	l, ok := m.links[code]
	if !ok || l.OwnerID != callerID {
		return store.ErrLinkNotFound
	}
	l.IsActive = false
	return nil
}

func (m *mockStore) ListLinksByOwner(_ context.Context, ownerID string, _ string, limit int) ([]*store.Link, string, error) {
	var result []*store.Link
	for _, l := range m.links {
		if l.OwnerID == ownerID {
			result = append(result, l)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, "", nil
}

func (m *mockStore) IncrementClickCount(_ context.Context, _ string, _ *int64) (bool, error) {
	return true, nil
}

func (m *mockStore) BatchWriteClicks(_ context.Context, _ []*store.ClickEvent) error {
	return nil
}

func (m *mockStore) GetLinkStats(_ context.Context, code string) (*store.LinkStats, error) {
	if s, ok := m.statsMap[code]; ok {
		return s, nil
	}
	return &store.LinkStats{}, nil
}

func (m *mockStore) GetLinkTimeline(_ context.Context, code string, _, _ time.Time, _ string) ([]store.TimelineBucket, error) {
	if t, ok := m.timelineMap[code]; ok {
		return t, nil
	}
	return nil, nil
}

func (m *mockStore) GetLinkGeo(_ context.Context, code string) ([]store.GeoStat, error) {
	if g, ok := m.geoMap[code]; ok {
		return g, nil
	}
	return nil, nil
}

func (m *mockStore) GetLinkReferrers(_ context.Context, code string) ([]store.ReferrerStat, error) {
	if r, ok := m.referrerMap[code]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockStore) GetUser(_ context.Context, _ string) (*store.User, error) {
	return nil, store.ErrUserNotFound
}

func (m *mockStore) UpdateUserQuota(_ context.Context, _ string) error {
	return nil
}

// Verify mockStore implements store.Store at compile time.
var _ store.Store = (*mockStore)(nil)

// ---------- Mock Cache ----------

type mockCache struct{}

func (m *mockCache) GetLink(_ context.Context, _ string) (*store.Link, error) { return nil, nil }
func (m *mockCache) SetLink(_ context.Context, _ string, _ *store.Link, _ time.Duration) error {
	return nil
}
func (m *mockCache) DeleteLink(_ context.Context, _ string) error         { return nil }
func (m *mockCache) SetNegative(_ context.Context, _ string) error        { return nil }
func (m *mockCache) IsNegative(_ context.Context, _ string) (bool, error) { return false, nil }

var _ cache.Cache = (*mockCache)(nil)

// ---------- Mock Limiter ----------

type mockLimiter struct {
	allowed   bool
	callCount int
}

func (m *mockLimiter) Allow(_ context.Context, _ string, limit int64, window time.Duration) (*ratelimit.Result, error) {
	m.callCount++
	return &ratelimit.Result{
		Allowed:   m.allowed,
		Count:     int64(m.callCount),
		Limit:     limit,
		Remaining: limit - int64(m.callCount),
		ResetAt:   time.Now().Add(window),
		RetryAfter: func() time.Duration {
			if m.allowed {
				return 0
			}
			return 60 * time.Second
		}(),
	}, nil
}

var _ ratelimit.Limiter = (*mockLimiter)(nil)

// ---------- Mock Generator ----------

type mockGenerator struct {
	code string
	seq  int
}

func (m *mockGenerator) Generate(_ context.Context) (string, error) {
	m.seq++
	return fmt.Sprintf("%s%d", m.code, m.seq), nil
}

func (m *mockGenerator) GenerateCustom(_ context.Context, _ string) error {
	return nil
}

var _ shortener.Generator = (*mockGenerator)(nil)

// ---------- Mock Validator ----------

type mockValidator struct {
	err error
}

func (m *mockValidator) ValidateURL(_ context.Context, _ string) error {
	return m.err
}

var _ validator.Validator = (*mockValidator)(nil)

// ---------- Mock Authenticator ----------

type mockAuthenticator struct {
	claims *auth.Claims
	err    error
}

func (m *mockAuthenticator) ValidateToken(_ context.Context, _ string) (*auth.Claims, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.claims, nil
}

var _ auth.Authenticator = (*mockAuthenticator)(nil)

// ---------- APIServer (re-created for integration package) ----------
//
// Because cmd/api is package main, we cannot import it. We re-create
// a minimal NewAPIServer constructor that wires up the same dependencies
// and registers the generated handler. This keeps integration tests
// decoupled from the build binary.

func setupTestServer(t *testing.T) (*gen.ServerInterface, http.Handler, *mockStore) {
	t.Helper()
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: true}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{}

	srv := newIntegrationAPIServer(ms, mc, ml, mg, mv)

	r := chi.NewRouter()
	handler := gen.HandlerFromMux(srv, r)

	iface := gen.ServerInterface(srv)
	return &iface, handler, ms
}

func setupTestServerWithLimiter(t *testing.T, limiter *mockLimiter) (*gen.ServerInterface, http.Handler, *mockStore) {
	t.Helper()
	ms := newMockStore()
	mc := &mockCache{}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{}

	srv := newIntegrationAPIServer(ms, mc, limiter, mg, mv)

	r := chi.NewRouter()
	handler := gen.HandlerFromMux(srv, r)

	iface := gen.ServerInterface(srv)
	return &iface, handler, ms
}

func setupTestServerWithValidator(t *testing.T, v validator.Validator) (*gen.ServerInterface, http.Handler, *mockStore) {
	t.Helper()
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: true}
	mg := &mockGenerator{code: "abc"}

	if v == nil {
		v = &mockValidator{}
	}
	srv := newIntegrationAPIServer(ms, mc, ml, mg, v)

	r := chi.NewRouter()
	handler := gen.HandlerFromMux(srv, r)

	iface := gen.ServerInterface(srv)
	return &iface, handler, ms
}

func setupTestServerWithAuth(t *testing.T, authenticator auth.Authenticator) (http.Handler, *mockStore) {
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

func setupTestServerWithAuthAndRequire(t *testing.T, authenticator auth.Authenticator) (http.Handler, *mockStore) {
	t.Helper()
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: true}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{}

	srv := newIntegrationAPIServer(ms, mc, ml, mg, mv)

	r := chi.NewRouter()
	r.Use(auth.Middleware(authenticator))
	r.Use(func(next http.Handler) http.Handler {
		return auth.RequireAuth(next)
	})
	handler := gen.HandlerFromMux(srv, r)

	return handler, ms
}

// authHeaders returns headers that simulate an authenticated user via X-User-Id.
func authHeaders(userID string) map[string]string {
	return map[string]string{headerUserID: userID}
}

// doJSONRequest creates an HTTP request with optional JSON body and fires it.
func doJSONRequest(handler http.Handler, method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
