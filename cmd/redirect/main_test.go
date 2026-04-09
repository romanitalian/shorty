package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/romanitalian/shorty/internal/ratelimit"
	"github.com/romanitalian/shorty/internal/store"
)

// --- Mock implementations ---

type mockStore struct {
	getLink             func(ctx context.Context, code string) (*store.Link, error)
	incrementClickCount func(ctx context.Context, code string, maxClicks *int64) (bool, error)
}

func (m *mockStore) CreateLink(ctx context.Context, link *store.Link) error { return nil }
func (m *mockStore) GetLink(ctx context.Context, code string) (*store.Link, error) {
	if m.getLink != nil {
		return m.getLink(ctx, code)
	}
	return nil, store.ErrLinkNotFound
}
func (m *mockStore) UpdateLink(ctx context.Context, code, callerID string, updates map[string]interface{}) error {
	return nil
}
func (m *mockStore) DeleteLink(ctx context.Context, code, callerID string) error { return nil }
func (m *mockStore) ListLinksByOwner(ctx context.Context, ownerID, cursor string, limit int) ([]*store.Link, string, error) {
	return nil, "", nil
}
func (m *mockStore) IncrementClickCount(ctx context.Context, code string, maxClicks *int64) (bool, error) {
	if m.incrementClickCount != nil {
		return m.incrementClickCount(ctx, code, maxClicks)
	}
	return true, nil
}
func (m *mockStore) BatchWriteClicks(ctx context.Context, events []*store.ClickEvent) error {
	return nil
}
func (m *mockStore) GetLinkStats(_ context.Context, _ string) (*store.LinkStats, error) {
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
func (m *mockStore) GetUser(ctx context.Context, userID string) (*store.User, error) { return nil, nil }
func (m *mockStore) UpdateUserQuota(ctx context.Context, userID string) error               { return nil }

type mockCache struct {
	getLink     func(ctx context.Context, code string) (*store.Link, error)
	setLink     func(ctx context.Context, code string, link *store.Link, ttl time.Duration) error
	deleteLink  func(ctx context.Context, code string) error
	setNegative func(ctx context.Context, code string) error
	isNegative  func(ctx context.Context, code string) (bool, error)
}

func (m *mockCache) GetLink(ctx context.Context, code string) (*store.Link, error) {
	if m.getLink != nil {
		return m.getLink(ctx, code)
	}
	return nil, nil
}
func (m *mockCache) SetLink(ctx context.Context, code string, link *store.Link, ttl time.Duration) error {
	if m.setLink != nil {
		return m.setLink(ctx, code, link, ttl)
	}
	return nil
}
func (m *mockCache) DeleteLink(ctx context.Context, code string) error {
	if m.deleteLink != nil {
		return m.deleteLink(ctx, code)
	}
	return nil
}
func (m *mockCache) SetNegative(ctx context.Context, code string) error {
	if m.setNegative != nil {
		return m.setNegative(ctx, code)
	}
	return nil
}
func (m *mockCache) IsNegative(ctx context.Context, code string) (bool, error) {
	if m.isNegative != nil {
		return m.isNegative(ctx, code)
	}
	return false, nil
}

type mockLimiter struct {
	allow func(ctx context.Context, key string, limit int64, window time.Duration) (*ratelimit.Result, error)
}

func (m *mockLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (*ratelimit.Result, error) {
	if m.allow != nil {
		return m.allow(ctx, key, limit, window)
	}
	return &ratelimit.Result{Allowed: true, Limit: limit, Remaining: limit - 1}, nil
}

type mockSQS struct {
	mu       sync.Mutex
	messages []*sqs.SendMessageInput
}

func (m *mockSQS) SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, params)
	return &sqs.SendMessageOutput{}, nil
}

// --- Helper ---

func makeRequest(code string) events.APIGatewayV2HTTPRequest {
	return events.APIGatewayV2HTTPRequest{
		RawPath: "/" + code,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				SourceIP: "192.168.1.1",
				Method:   http.MethodGet,
			},
		},
		Headers: map[string]string{
			"user-agent": "Mozilla/5.0",
			"referer":    "https://example.com/page",
		},
	}
}

func newTestHandler(st store.Store, ca *mockCache, lim *mockLimiter, sqsPub SQSPublisher) *RedirectHandler {
	h := NewRedirectHandler(st, ca, lim, sqsPub, "https://sqs.us-east-1.amazonaws.com/123/clicks.fifo", "test-salt", "test-csrf-secret")
	h.nowFunc = func() time.Time {
		return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	}
	return h
}

// --- Tests ---

func TestSuccessfulRedirect302(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://example.com/target",
				IsActive:    true,
			}, nil
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}
	sqsMock := &mockSQS{}

	h := newTestHandler(ms, mc, ml, sqsMock)
	resp, err := h.Handle(context.Background(), makeRequest("abc123"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	if resp.Headers["Location"] != "https://example.com/target" {
		t.Errorf("expected Location header, got %q", resp.Headers["Location"])
	}
}

func TestSuccessfulRedirectWithUTM(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://example.com/target",
				IsActive:    true,
				UTMSource:   "twitter",
				UTMMedium:   "social",
				UTMCampaign: "launch",
			}, nil
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("abc123"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Headers["Location"]
	if !strings.Contains(loc, "utm_source=twitter") {
		t.Errorf("expected utm_source in Location, got %q", loc)
	}
	if !strings.Contains(loc, "utm_medium=social") {
		t.Errorf("expected utm_medium in Location, got %q", loc)
	}
}

func TestRateLimited429(t *testing.T) {
	mc := &mockCache{}
	ml := &mockLimiter{
		allow: func(_ context.Context, _ string, limit int64, window time.Duration) (*ratelimit.Result, error) {
			return &ratelimit.Result{
				Allowed:    false,
				Limit:      limit,
				Remaining:  0,
				RetryAfter: 30 * time.Second,
				ResetAt:    time.Now().Add(30 * time.Second),
			}, nil
		},
	}

	h := newTestHandler(&mockStore{}, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("abc123"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Headers["Retry-After"] == "" {
		t.Error("expected Retry-After header")
	}
	if resp.Headers["X-RateLimit-Limit"] == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
}

func TestNotFound404(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, _ string) (*store.Link, error) {
			return nil, store.ErrLinkNotFound
		},
	}
	negativeCacheSet := false
	mc := &mockCache{
		setNegative: func(_ context.Context, _ string) error {
			negativeCacheSet = true
			return nil
		},
	}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("notexist"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	if !negativeCacheSet {
		t.Error("expected negative cache to be set")
	}
}

func TestNegativeCacheHit404(t *testing.T) {
	storeCalled := false
	ms := &mockStore{
		getLink: func(_ context.Context, _ string) (*store.Link, error) {
			storeCalled = true
			return nil, store.ErrLinkNotFound
		},
	}
	mc := &mockCache{
		isNegative: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("negcached"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	if storeCalled {
		t.Error("DynamoDB should not have been called when negative cache hit")
	}
}

func TestExpiredLink410(t *testing.T) {
	past := int64(1700000000) // well in the past
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://example.com",
				IsActive:    true,
				ExpiresAt:   &past,
			}, nil
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("expired"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusGone {
		t.Errorf("expected 410, got %d", resp.StatusCode)
	}
}

func TestDeactivatedLink410(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://example.com",
				IsActive:    false,
			}, nil
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("inactive"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusGone {
		t.Errorf("expected 410, got %d", resp.StatusCode)
	}
}

func TestClickLimitReached410(t *testing.T) {
	maxClicks := int64(10)
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://example.com",
				IsActive:    true,
				MaxClicks:   &maxClicks,
				ClickCount:  10,
			}, nil
		},
		incrementClickCount: func(_ context.Context, _ string, _ *int64) (bool, error) {
			return false, nil // limit reached
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("maxed"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusGone {
		t.Errorf("expected 410, got %d", resp.StatusCode)
	}
}

func TestPasswordProtectedShowsForm(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:         code,
				OriginalURL:  "https://example.com",
				IsActive:     true,
				PasswordHash: "somehash",
			}, nil
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("protected"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Body, "password") {
		t.Error("expected password form in response body")
	}
	if resp.Headers["Content-Type"] != "text/html; charset=utf-8" {
		t.Errorf("expected HTML content type, got %q", resp.Headers["Content-Type"])
	}
}

func TestCacheHitNoDynamoDBCall(t *testing.T) {
	storeCalled := false
	ms := &mockStore{
		getLink: func(_ context.Context, _ string) (*store.Link, error) {
			storeCalled = true
			return nil, errors.New("should not be called")
		},
	}
	mc := &mockCache{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://cached.example.com",
				IsActive:    true,
			}, nil
		},
	}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("cached"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	if storeCalled {
		t.Error("DynamoDB should not be called on cache hit")
	}
	if resp.Headers["Location"] != "https://cached.example.com" {
		t.Errorf("unexpected Location: %q", resp.Headers["Location"])
	}
}

func TestCacheMissFallbackToDynamoDB(t *testing.T) {
	storeGetCalled := false
	cacheSetCalled := false
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			storeGetCalled = true
			return &store.Link{
				Code:        code,
				OriginalURL: "https://dynamo.example.com",
				IsActive:    true,
			}, nil
		},
	}
	mc := &mockCache{
		setLink: func(_ context.Context, _ string, _ *store.Link, _ time.Duration) error {
			cacheSetCalled = true
			return nil
		},
	}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("uncached"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
	if !storeGetCalled {
		t.Error("expected DynamoDB GetLink to be called on cache miss")
	}
	if !cacheSetCalled {
		t.Error("expected cache SetLink to be called (cache warm)")
	}
}

func TestAsyncSQSPublish(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://example.com",
				IsActive:    true,
			}, nil
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}
	sqsMock := &mockSQS{}

	h := newTestHandler(ms, mc, ml, sqsMock)
	resp, err := h.Handle(context.Background(), makeRequest("sqstest"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}

	// Wait briefly for goroutine to complete
	time.Sleep(100 * time.Millisecond)

	sqsMock.mu.Lock()
	defer sqsMock.mu.Unlock()
	if len(sqsMock.messages) != 1 {
		t.Fatalf("expected 1 SQS message, got %d", len(sqsMock.messages))
	}

	msg := sqsMock.messages[0]
	if *msg.MessageGroupId != "sqstest" {
		t.Errorf("expected MessageGroupId 'sqstest', got %q", *msg.MessageGroupId)
	}

	var body map[string]interface{}
	if err := json.Unmarshal([]byte(*msg.MessageBody), &body); err != nil {
		t.Fatalf("failed to parse SQS body: %v", err)
	}
	if body["code"] != "sqstest" {
		t.Errorf("expected code 'sqstest' in SQS body, got %v", body["code"])
	}
}

func TestMissingCode400(t *testing.T) {
	h := newTestHandler(&mockStore{}, &mockCache{}, &mockLimiter{}, nil)
	req := events.APIGatewayV2HTTPRequest{
		RawPath: "/",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				SourceIP: "192.168.1.1",
				Method:   http.MethodGet,
			},
		},
		Headers: map[string]string{},
	}

	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHashIP(t *testing.T) {
	hash1 := HashIP("192.168.1.1", "salt1")
	hash2 := HashIP("192.168.1.1", "salt1")
	hash3 := HashIP("192.168.1.1", "salt2")
	hash4 := HashIP("10.0.0.1", "salt1")

	if hash1 != hash2 {
		t.Error("same IP + same salt should produce the same hash")
	}
	if hash1 == hash3 {
		t.Error("same IP + different salt should produce different hashes")
	}
	if hash1 == hash4 {
		t.Error("different IP + same salt should produce different hashes")
	}
	if len(hash1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("expected 64-char hex hash, got %d chars", len(hash1))
	}
}

func TestExtractCode(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/abc123", "abc123"},
		{"/abc123/", "abc123"},
		{"/", ""},
		{"", ""},
		{"/abc?q=1", "abc"},
	}
	for _, tt := range tests {
		got := extractCode(tt.path)
		if got != tt.expected {
			t.Errorf("extractCode(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}

func TestBuildRedirectURL(t *testing.T) {
	// No UTM params
	link := &store.Link{OriginalURL: "https://example.com/page"}
	if got := buildRedirectURL(link); got != "https://example.com/page" {
		t.Errorf("unexpected URL: %q", got)
	}

	// With UTM params
	link.UTMSource = "twitter"
	link.UTMMedium = "social"
	got := buildRedirectURL(link)
	if !strings.Contains(got, "utm_source=twitter") {
		t.Errorf("expected utm_source in URL: %q", got)
	}
	if !strings.Contains(got, "utm_medium=social") {
		t.Errorf("expected utm_medium in URL: %q", got)
	}
}

func TestStoreInternalError500(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, _ string) (*store.Link, error) {
			return nil, errors.New("dynamodb connection error")
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("err"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

func TestIncrementClickCountError500(t *testing.T) {
	ms := &mockStore{
		getLink: func(_ context.Context, code string) (*store.Link, error) {
			return &store.Link{
				Code:        code,
				OriginalURL: "https://example.com",
				IsActive:    true,
			}, nil
		},
		incrementClickCount: func(_ context.Context, _ string, _ *int64) (bool, error) {
			return false, errors.New("dynamodb error")
		},
	}
	mc := &mockCache{}
	ml := &mockLimiter{}

	h := newTestHandler(ms, mc, ml, nil)
	resp, err := h.Handle(context.Background(), makeRequest("incerr"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}
