package mocks

import (
	"context"
	"time"

	"github.com/romanitalian/shorty/internal/ratelimit"
)

// MockLimiter implements ratelimit.Limiter for testing.
type MockLimiter struct {
	AllowFn func(ctx context.Context, key string, limit int64, window time.Duration) (*ratelimit.Result, error)
}

func (m *MockLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (*ratelimit.Result, error) {
	if m.AllowFn != nil {
		return m.AllowFn(ctx, key, limit, window)
	}
	return &ratelimit.Result{
		Allowed:   true,
		Count:     0,
		Limit:     limit,
		Remaining: limit,
		ResetAt:   time.Now().Add(window),
	}, nil
}
