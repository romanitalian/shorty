package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	gen "github.com/romanitalian/shorty/internal/api/generated"
	"github.com/romanitalian/shorty/internal/ratelimit"
	"github.com/romanitalian/shorty/internal/store"
)

// ---------- Mock Store ----------

type mockStore struct {
	links    map[string]*store.Link
	createFn func(ctx context.Context, link *store.Link) error
}

func newMockStore() *mockStore {
	return &mockStore{links: make(map[string]*store.Link)}
}

func (m *mockStore) CreateLink(_ context.Context, link *store.Link) error {
	if m.createFn != nil {
		return m.createFn(nil, link)
	}
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
	return &store.LinkStats{}, nil
}

func (m *mockStore) GetLinkTimeline(_ context.Context, _ string, _, _ time.Time, _ string) ([]store.TimelineBucket, error) {
	return nil, nil
}

func (m *mockStore) GetLinkGeo(_ context.Context, _ string) ([]store.GeoStat, error) {
	return nil, nil
}

func (m *mockStore) GetLinkReferrers(_ context.Context, _ string) ([]store.ReferrerStat, error) {
	return nil, nil
}

func (m *mockStore) GetUser(_ context.Context, _ string) (*store.User, error) {
	return nil, store.ErrUserNotFound
}

func (m *mockStore) UpdateUserQuota(_ context.Context, _ string) error {
	return nil
}

// ---------- Mock Cache ----------

type mockCache struct{}

func (m *mockCache) GetLink(_ context.Context, _ string) (*store.Link, error) { return nil, nil }
func (m *mockCache) SetLink(_ context.Context, _ string, _ *store.Link, _ time.Duration) error {
	return nil
}
func (m *mockCache) DeleteLink(_ context.Context, _ string) error         { return nil }
func (m *mockCache) SetNegative(_ context.Context, _ string) error        { return nil }
func (m *mockCache) IsNegative(_ context.Context, _ string) (bool, error) { return false, nil }

// ---------- Mock Limiter ----------

type mockLimiter struct {
	allowed bool
}

func (m *mockLimiter) Allow(_ context.Context, _ string, limit int64, window time.Duration) (*ratelimit.Result, error) {
	return &ratelimit.Result{
		Allowed:   m.allowed,
		Count:     1,
		Limit:     limit,
		Remaining: limit - 1,
		ResetAt:   time.Now().Add(window),
		RetryAfter: func() time.Duration {
			if m.allowed {
				return 0
			}
			return 60 * time.Second
		}(),
	}, nil
}

// ---------- Mock Generator ----------

type mockGenerator struct {
	code string
	seq  int
}

func (m *mockGenerator) Generate(_ context.Context) (string, error) {
	m.seq++
	return fmt.Sprintf("%s%d", m.code, m.seq), nil
}

func (m *mockGenerator) GenerateCustom(_ context.Context, code string) error {
	return nil
}

// ---------- Mock Validator ----------

type mockValidator struct {
	err error
}

func (m *mockValidator) ValidateURL(_ context.Context, _ string) error {
	return m.err
}

// ---------- Test helpers ----------

func setupServer(t *testing.T) (*APIServer, http.Handler, *mockStore) {
	t.Helper()
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: true}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{}
	srv := NewAPIServer(ms, mc, ml, mg, mv)
	r := chi.NewRouter()
	handler := gen.HandlerFromMux(srv, r)
	return srv, handler, ms
}

func doRequest(handler http.Handler, method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
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

func authHeaders(userID string) map[string]string {
	return map[string]string{headerUserID: userID}
}

// ---------- Tests ----------

func TestGuestShorten_201(t *testing.T) {
	_, handler, _ := setupServer(t)
	body := gen.GuestShortenRequest{Url: "https://example.com/long-url"}
	rec := doRequest(handler, http.MethodPost, "/api/v1/shorten", body, nil)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.GuestShortenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code == "" {
		t.Fatal("expected non-empty code")
	}
	if resp.OriginalUrl != "https://example.com/long-url" {
		t.Fatalf("expected original URL, got %s", resp.OriginalUrl)
	}
}

func TestGuestShorten_RateLimited_429(t *testing.T) {
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: false}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{}
	srv := NewAPIServer(ms, mc, ml, mg, mv)
	r := chi.NewRouter()
	handler := gen.HandlerFromMux(srv, r)

	body := gen.GuestShortenRequest{Url: "https://example.com"}
	rec := doRequest(handler, http.MethodPost, "/api/v1/shorten", body, nil)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateLink_201(t *testing.T) {
	_, handler, _ := setupServer(t)
	body := gen.CreateLinkRequest{Url: "https://example.com/page"}
	rec := doRequest(handler, http.MethodPost, "/api/v1/links", body, authHeaders("user1"))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.Link
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OwnerId != "user1" {
		t.Fatalf("expected owner user1, got %s", resp.OwnerId)
	}
}

func TestCreateLink_InvalidURL_400(t *testing.T) {
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: true}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{err: errors.New("URL is not valid")}
	srv := NewAPIServer(ms, mc, ml, mg, mv)
	r := chi.NewRouter()
	handler := gen.HandlerFromMux(srv, r)

	body := gen.CreateLinkRequest{Url: "not-a-url"}
	rec := doRequest(handler, http.MethodPost, "/api/v1/links", body, authHeaders("user1"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateLink_DuplicateCustomAlias_409(t *testing.T) {
	ms := newMockStore()
	mc := &mockCache{}
	ml := &mockLimiter{allowed: true}
	mg := &mockGenerator{code: "abc"}
	mv := &mockValidator{}

	// Use a generator that returns collision for custom alias.
	_ = mg // not needed for this test
	mg2 := &duplicateGenerator{}
	srv := NewAPIServer(ms, mc, ml, mg2, mv)
	r := chi.NewRouter()
	handler := gen.HandlerFromMux(srv, r)

	custom := "my-alias"
	body := gen.CreateLinkRequest{Url: "https://example.com", CustomCode: &custom}
	rec := doRequest(handler, http.MethodPost, "/api/v1/links", body, authHeaders("user1"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

type duplicateGenerator struct{}

func (d *duplicateGenerator) Generate(_ context.Context) (string, error) {
	return "test123", nil
}
func (d *duplicateGenerator) GenerateCustom(_ context.Context, _ string) error {
	return store.ErrCodeCollision
}

func TestListLinks_200(t *testing.T) {
	_, handler, ms := setupServer(t)
	// Seed a link.
	now := time.Now().Unix()
	ms.links["abc1"] = &store.Link{
		Code:        "abc1",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	rec := doRequest(handler, http.MethodGet, "/api/v1/links", nil, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.LinkListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
}

func TestGetLink_200(t *testing.T) {
	_, handler, ms := setupServer(t)
	now := time.Now().Unix()
	ms.links["mycode"] = &store.Link{
		Code:        "mycode",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	rec := doRequest(handler, http.MethodGet, "/api/v1/links/mycode", nil, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetLink_NotFound_404(t *testing.T) {
	_, handler, _ := setupServer(t)

	rec := doRequest(handler, http.MethodGet, "/api/v1/links/nonexistent", nil, authHeaders("user1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateLink_200(t *testing.T) {
	_, handler, ms := setupServer(t)
	now := time.Now().Unix()
	ms.links["upd1"] = &store.Link{
		Code:        "upd1",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	title := "New Title"
	body := gen.UpdateLinkRequest{Title: &title}
	rec := doRequest(handler, http.MethodPatch, "/api/v1/links/upd1", body, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.Link
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Title == nil || *resp.Title != "New Title" {
		t.Fatalf("expected title 'New Title', got %v", resp.Title)
	}
}

func TestDeleteLink_204(t *testing.T) {
	_, handler, ms := setupServer(t)
	now := time.Now().Unix()
	ms.links["del1"] = &store.Link{
		Code:        "del1",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	rec := doRequest(handler, http.MethodDelete, "/api/v1/links/del1", nil, authHeaders("user1"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}
