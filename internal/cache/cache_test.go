package cache

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/romanitalian/shorty/internal/store"
)

// --- Mock Redis Client ---

type mockRedisClient struct {
	store map[string]string
}

func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{store: make(map[string]string)}
}

func (m *mockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	val, ok := m.store[key]
	cmd := redis.NewStringCmd(ctx)
	if !ok {
		cmd.SetErr(redis.Nil)
		return cmd
	}
	cmd.SetVal(val)
	return cmd
}

func (m *mockRedisClient) Set(ctx context.Context, key string, value interface{}, _ time.Duration) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	switch v := value.(type) {
	case string:
		m.store[key] = v
	case []byte:
		m.store[key] = string(v)
	default:
		cmd.SetErr(redis.Nil)
		return cmd
	}
	cmd.SetVal("OK")
	return cmd
}

func (m *mockRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)
	var deleted int64
	for _, key := range keys {
		if _, ok := m.store[key]; ok {
			delete(m.store, key)
			deleted++
		}
	}
	cmd.SetVal(deleted)
	return cmd
}

// --- Tests ---

func TestSetLink_GetLink_RoundTrip(t *testing.T) {
	mock := newMockRedisClient()
	c := NewRedisCache(mock)

	link := &store.Link{
		Code:        "abc1234",
		OriginalURL: "https://example.com",
		IsActive:    true,
		ClickCount:  42,
	}

	err := c.SetLink(context.Background(), "abc1234", link, 5*time.Minute)
	if err != nil {
		t.Fatalf("SetLink: %v", err)
	}

	got, err := c.GetLink(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got == nil {
		t.Fatal("expected link, got nil")
	}
	if got.OriginalURL != "https://example.com" {
		t.Errorf("expected URL 'https://example.com', got %q", got.OriginalURL)
	}
	if got.ClickCount != 42 {
		t.Errorf("expected click_count 42, got %d", got.ClickCount)
	}
	if !got.IsActive {
		t.Error("expected is_active=true")
	}
}

func TestGetLink_Miss(t *testing.T) {
	mock := newMockRedisClient()
	c := NewRedisCache(mock)

	got, err := c.GetLink(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on cache miss, got %v", got)
	}
}

func TestDeleteLink(t *testing.T) {
	mock := newMockRedisClient()
	c := NewRedisCache(mock)

	link := &store.Link{
		Code:        "abc1234",
		OriginalURL: "https://example.com",
		IsActive:    true,
	}
	_ = c.SetLink(context.Background(), "abc1234", link, 5*time.Minute)

	err := c.DeleteLink(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}

	got, _ := c.GetLink(context.Background(), "abc1234")
	if got != nil {
		t.Error("expected nil after delete, got link")
	}
}

func TestSetNegative_IsNegative(t *testing.T) {
	mock := newMockRedisClient()
	c := NewRedisCache(mock)

	// Not negative initially
	neg, err := c.IsNegative(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("IsNegative: %v", err)
	}
	if neg {
		t.Error("expected not negative initially")
	}

	// Set negative
	err = c.SetNegative(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("SetNegative: %v", err)
	}

	neg, err = c.IsNegative(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("IsNegative: %v", err)
	}
	if !neg {
		t.Error("expected negative after SetNegative")
	}
}

func TestGetLink_CorruptedData(t *testing.T) {
	mock := newMockRedisClient()
	mock.store["link:corrupt"] = "not valid json"
	c := NewRedisCache(mock)

	got, err := c.GetLink(context.Background(), "corrupt")
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got != nil {
		t.Error("expected nil for corrupted data, got link")
	}
}

func TestSetLink_WithExpiry(t *testing.T) {
	mock := newMockRedisClient()
	c := NewRedisCache(mock)

	expiresAt := time.Now().Add(2 * time.Minute).Unix()
	link := &store.Link{
		Code:        "expiring",
		OriginalURL: "https://example.com",
		IsActive:    true,
		ExpiresAt:   &expiresAt,
	}

	// Use 0 TTL to trigger computeTTL
	err := c.SetLink(context.Background(), "expiring", link, 0)
	if err != nil {
		t.Fatalf("SetLink: %v", err)
	}

	got, _ := c.GetLink(context.Background(), "expiring")
	if got == nil {
		t.Fatal("expected cached link")
	}
	if got.OriginalURL != "https://example.com" {
		t.Errorf("unexpected URL: %q", got.OriginalURL)
	}
}

func TestSetLink_AlreadyExpired(t *testing.T) {
	mock := newMockRedisClient()
	c := NewRedisCache(mock)

	past := time.Now().Add(-1 * time.Hour).Unix()
	link := &store.Link{
		Code:        "expired",
		OriginalURL: "https://example.com",
		IsActive:    true,
		ExpiresAt:   &past,
	}

	err := c.SetLink(context.Background(), "expired", link, 0)
	if err != nil {
		t.Fatalf("SetLink: %v", err)
	}

	// Already expired link should not be cached
	got, _ := c.GetLink(context.Background(), "expired")
	if got != nil {
		t.Error("expected nil for already-expired link")
	}
}

func TestSetLink_UTMFields(t *testing.T) {
	mock := newMockRedisClient()
	c := NewRedisCache(mock)

	link := &store.Link{
		Code:        "utm",
		OriginalURL: "https://example.com",
		IsActive:    true,
		UTMSource:   "twitter",
		UTMMedium:   "social",
		UTMCampaign: "launch",
	}

	err := c.SetLink(context.Background(), "utm", link, 5*time.Minute)
	if err != nil {
		t.Fatalf("SetLink: %v", err)
	}

	// Verify the cached JSON has UTM fields
	raw := mock.store["link:utm"]
	var cl cachedLink
	if err := json.Unmarshal([]byte(raw), &cl); err != nil {
		t.Fatalf("unmarshal cached data: %v", err)
	}
	if cl.UTMSource != "twitter" {
		t.Errorf("expected utm_source 'twitter', got %q", cl.UTMSource)
	}
	if cl.UTMMedium != "social" {
		t.Errorf("expected utm_medium 'social', got %q", cl.UTMMedium)
	}
	if cl.UTMCampaign != "launch" {
		t.Errorf("expected utm_campaign 'launch', got %q", cl.UTMCampaign)
	}

	got, _ := c.GetLink(context.Background(), "utm")
	if got == nil {
		t.Fatal("expected link")
	}
	if got.UTMSource != "twitter" {
		t.Errorf("expected utm_source 'twitter', got %q", got.UTMSource)
	}
}

func TestComputeTTL(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt *int64
		wantMax   time.Duration
		wantMin   time.Duration
	}{
		{
			name:      "no expiry",
			expiresAt: nil,
			wantMax:   defaultTTL,
			wantMin:   defaultTTL,
		},
		{
			name:      "far future expiry",
			expiresAt: ptrInt64(time.Now().Add(1 * time.Hour).Unix()),
			wantMax:   defaultTTL,
			wantMin:   defaultTTL,
		},
		{
			name:      "near expiry (2 min)",
			expiresAt: ptrInt64(time.Now().Add(2 * time.Minute).Unix()),
			wantMax:   2*time.Minute + time.Second,
			wantMin:   1*time.Minute + 59*time.Second,
		},
		{
			name:      "already expired",
			expiresAt: ptrInt64(time.Now().Add(-1 * time.Hour).Unix()),
			wantMax:   0,
			wantMin:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := &store.Link{ExpiresAt: tt.expiresAt}
			got := computeTTL(link)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("computeTTL() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func ptrInt64(v int64) *int64 { return &v }
