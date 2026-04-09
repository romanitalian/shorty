package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testRedisAddr returns the Redis address for testing.
// Falls back to localhost:6379 for local development.
const testRedisAddr = "localhost:6379"

func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at %s: %v", testRedisAddr, err)
	}
	t.Cleanup(func() {
		client.Close()
	})
	return client
}

// uniqueKey generates a unique rate limit key for test isolation.
func uniqueKey(t *testing.T) string {
	t.Helper()
	return "rl:test:" + t.Name() + ":" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func TestAllowUnderLimit(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := uniqueKey(t)
	limiter := NewRedisLimiter(client)

	result, err := limiter.Allow(ctx, key, 10, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected request to be allowed")
	}
	if result.Count != 1 {
		t.Errorf("expected count=1, got %d", result.Count)
	}
	if result.Remaining != 9 {
		t.Errorf("expected remaining=9, got %d", result.Remaining)
	}
	if result.Limit != 10 {
		t.Errorf("expected limit=10, got %d", result.Limit)
	}
}

func TestDenyAtLimit(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := uniqueKey(t)
	limiter := NewRedisLimiter(client)

	limit := int64(3)

	// Fill up to the limit.
	for i := int64(0); i < limit; i++ {
		result, err := limiter.Allow(ctx, key, limit, time.Minute)
		if err != nil {
			t.Fatalf("unexpected error on request %d: %v", i+1, err)
		}
		if !result.Allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// Next request should be denied.
	result, err := limiter.Allow(ctx, key, limit, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected request to be denied at limit")
	}
	if result.Count != limit {
		t.Errorf("expected count=%d, got %d", limit, result.Count)
	}
	if result.Remaining != 0 {
		t.Errorf("expected remaining=0, got %d", result.Remaining)
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter when rate limited")
	}
}

func TestSlidingWindowExpiry(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := uniqueKey(t)

	// Use a controllable clock.
	now := time.Now()
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	setNow := func(t2 time.Time) {
		mu.Lock()
		defer mu.Unlock()
		now = t2
	}

	limiter := NewRedisLimiter(client, WithClock(clock))

	window := 2 * time.Second
	limit := int64(2)

	// Add two requests.
	for i := int64(0); i < limit; i++ {
		result, err := limiter.Allow(ctx, key, limit, window)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// At limit: denied.
	result, err := limiter.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denial at limit")
	}

	// Advance time past the window so entries expire.
	setNow(now.Add(window + 100*time.Millisecond))

	// Should be allowed again.
	result, err = limiter.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected request to be allowed after window expiry")
	}
	if result.Count != 1 {
		t.Errorf("expected count=1 after window expiry, got %d", result.Count)
	}
}

func TestResultFieldsCorrect(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := uniqueKey(t)

	now := time.Now()
	limiter := NewRedisLimiter(client, WithClock(func() time.Time { return now }))

	limit := int64(5)
	window := 10 * time.Second

	result, err := limiter.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Limit != 5 {
		t.Errorf("expected Limit=5, got %d", result.Limit)
	}
	if result.Remaining != 4 {
		t.Errorf("expected Remaining=4, got %d", result.Remaining)
	}
	if result.Count != 1 {
		t.Errorf("expected Count=1, got %d", result.Count)
	}

	// ResetAt should be approximately now + window.
	expectedReset := now.Add(window)
	if result.ResetAt.Sub(expectedReset) > time.Second {
		t.Errorf("expected ResetAt near %v, got %v", expectedReset, result.ResetAt)
	}
	if result.RetryAfter != 0 {
		t.Errorf("expected RetryAfter=0 when allowed, got %v", result.RetryAfter)
	}
}

func TestIndependentKeys(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key1 := uniqueKey(t) + ":key1"
	key2 := uniqueKey(t) + ":key2"
	limiter := NewRedisLimiter(client)

	limit := int64(1)
	window := time.Minute

	// Exhaust key1.
	result, err := limiter.Allow(ctx, key1, limit, window)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("key1 first request should be allowed")
	}

	// key1 is now at limit.
	result, err = limiter.Allow(ctx, key1, limit, window)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("key1 should be denied at limit")
	}

	// key2 should still be allowed (independent).
	result, err = limiter.Allow(ctx, key2, limit, window)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("key2 should be allowed (independent from key1)")
	}
}

func TestFailOpenWhenRedisUnavailable(t *testing.T) {
	// Connect to a non-existent Redis to simulate failure.
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:59999", // unlikely to be running
		DialTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() { client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	limiter := NewRedisLimiter(client, WithFailurePolicy(FailOpen))

	result, err := limiter.Allow(ctx, "rl:test:failopen", 10, time.Minute)
	if err != nil {
		t.Fatalf("fail-open should not return error, got: %v", err)
	}
	if !result.Allowed {
		t.Fatal("fail-open should allow request when Redis is unavailable")
	}
	if result.Remaining != 10 {
		t.Errorf("fail-open should report full remaining, got %d", result.Remaining)
	}
}

func TestFailClosedWhenRedisUnavailable(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "localhost:59999",
		DialTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() { client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	limiter := NewRedisLimiter(client, WithFailurePolicy(FailClosed))

	result, err := limiter.Allow(ctx, "rl:test:failclosed", 10, time.Minute)
	if err == nil {
		t.Fatal("fail-closed should return error when Redis is unavailable")
	}
	if result.Allowed {
		t.Fatal("fail-closed should deny request when Redis is unavailable")
	}
	if result.Remaining != 0 {
		t.Errorf("fail-closed should report 0 remaining, got %d", result.Remaining)
	}
}

func TestConcurrentRequests(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	key := uniqueKey(t)
	limiter := NewRedisLimiter(client)

	limit := int64(10)
	window := time.Minute
	goroutines := 20

	var allowed atomic.Int64
	var denied atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := limiter.Allow(ctx, key, limit, window)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result.Allowed {
				allowed.Add(1)
			} else {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()

	if allowed.Load() != limit {
		t.Errorf("expected exactly %d allowed requests, got %d", limit, allowed.Load())
	}
	if denied.Load() != int64(goroutines)-limit {
		t.Errorf("expected %d denied requests, got %d", int64(goroutines)-limit, denied.Load())
	}
}

func TestSetRateLimitHeaders(t *testing.T) {
	now := time.Now()

	t.Run("allowed request", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := &Result{
			Allowed:   true,
			Count:     5,
			Limit:     100,
			Remaining: 95,
			ResetAt:   now.Add(time.Minute),
		}

		SetRateLimitHeaders(w, result)

		resp := w.Result()
		defer resp.Body.Close()

		if got := resp.Header.Get("X-RateLimit-Limit"); got != "100" {
			t.Errorf("X-RateLimit-Limit = %q, want %q", got, "100")
		}
		if got := resp.Header.Get("X-RateLimit-Remaining"); got != "95" {
			t.Errorf("X-RateLimit-Remaining = %q, want %q", got, "95")
		}
		expectedReset := strconv.FormatInt(now.Add(time.Minute).Unix(), 10)
		if got := resp.Header.Get("X-RateLimit-Reset"); got != expectedReset {
			t.Errorf("X-RateLimit-Reset = %q, want %q", got, expectedReset)
		}
		if got := resp.Header.Get("Retry-After"); got != "" {
			t.Errorf("Retry-After should not be set for allowed requests, got %q", got)
		}
	})

	t.Run("denied request", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := &Result{
			Allowed:    false,
			Count:      100,
			Limit:      100,
			Remaining:  0,
			ResetAt:    now.Add(30 * time.Second),
			RetryAfter: 30 * time.Second,
		}

		SetRateLimitHeaders(w, result)

		resp := w.Result()
		defer resp.Body.Close()

		if got := resp.Header.Get("X-RateLimit-Remaining"); got != "0" {
			t.Errorf("X-RateLimit-Remaining = %q, want %q", got, "0")
		}
		if got := resp.Header.Get("Retry-After"); got != "30" {
			t.Errorf("Retry-After = %q, want %q", got, "30")
		}
	})
}

func TestSetRateLimitHeadersHTTP429(t *testing.T) {
	w := httptest.NewRecorder()
	result := &Result{
		Allowed:    false,
		Count:      200,
		Limit:      200,
		Remaining:  0,
		ResetAt:    time.Now().Add(45 * time.Second),
		RetryAfter: 45 * time.Second,
	}

	SetRateLimitHeaders(w, result)
	w.WriteHeader(http.StatusTooManyRequests)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got != "45" {
		t.Errorf("Retry-After = %q, want %q", got, "45")
	}
}

func TestTierValues(t *testing.T) {
	tests := []struct {
		tier   Tier
		name   string
		limit  int64
		window time.Duration
	}{
		{AnonymousRedirect, "anon_redirect", 200, time.Minute},
		{AnonymousCreate, "anon_create", 5, time.Hour},
		{FreeCreate, "free_create", 50, 24 * time.Hour},
		{ProCreate, "pro_create", 500, 24 * time.Hour},
		{PasswordAttempt, "password", 5, 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.tier.Name != tt.name {
				t.Errorf("Name = %q, want %q", tt.tier.Name, tt.name)
			}
			if tt.tier.Limit != tt.limit {
				t.Errorf("Limit = %d, want %d", tt.tier.Limit, tt.limit)
			}
			if tt.tier.Window != tt.window {
				t.Errorf("Window = %v, want %v", tt.tier.Window, tt.window)
			}
		})
	}
}
