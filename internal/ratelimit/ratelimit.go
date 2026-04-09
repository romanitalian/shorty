package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// slidingWindowScript is the Lua script for the atomic sliding window rate limiter.
// It uses a Redis sorted set where members are unique request IDs and scores are
// timestamps in milliseconds.
//
// KEYS[1] = rate limit key (e.g., "rl:redir:a1b2c3...")
// ARGV[1] = current timestamp in milliseconds
// ARGV[2] = window size in milliseconds
// ARGV[3] = maximum allowed requests in the window
// ARGV[4] = unique request ID
//
// Returns: { allowed (0 or 1), current_count, ttl_ms }
const slidingWindowScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local request_id = ARGV[4]

-- Step 1: Remove all entries outside the current window.
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

-- Step 2: Count remaining entries in the window.
local count = redis.call('ZCARD', key)

-- Step 3: Check if the request is within the limit.
if count >= limit then
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local reset_ms = 0
    if #oldest > 0 then
        reset_ms = tonumber(oldest[2]) + window - now
        if reset_ms < 0 then reset_ms = 0 end
    end
    return {0, count, reset_ms}
end

-- Step 4: Add the current request to the window.
redis.call('ZADD', key, now, request_id)

-- Step 5: Set key expiry to window size (cleanup if no further requests).
redis.call('PEXPIRE', key, window)

-- Step 6: Return success with updated count.
return {1, count + 1, 0}
`

// Result contains the outcome of a rate limit check.
type Result struct {
	// Allowed indicates whether the request is permitted.
	Allowed bool
	// Count is the current number of requests in the window.
	Count int64
	// Limit is the maximum number of requests allowed in the window.
	Limit int64
	// Remaining is the number of requests remaining before rate limiting.
	Remaining int64
	// ResetAt is the time when the current window expires.
	ResetAt time.Time
	// RetryAfter is the duration to wait before retrying (zero if allowed).
	RetryAfter time.Duration
}

// Limiter defines the interface for a rate limiter.
type Limiter interface {
	// Allow checks whether a request identified by key is allowed under the given
	// limit and window. Returns a Result with the decision and metadata.
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (*Result, error)
}

// Clock is a function that returns the current time. Injectable for testing.
type Clock func() time.Time

// Scripter is the subset of the go-redis client needed for Lua script execution.
type Scripter interface {
	redis.Scripter
}

// FailurePolicy determines behavior when Redis is unavailable.
type FailurePolicy int

const (
	// FailOpen allows requests when Redis is unavailable.
	// Use for redirect rate limiting where availability is prioritized.
	FailOpen FailurePolicy = iota
	// FailClosed denies requests when Redis is unavailable.
	// Use for creation rate limiting where abuse prevention is prioritized.
	FailClosed
)

// RedisLimiter implements Limiter using a Redis sorted set and Lua script.
type RedisLimiter struct {
	client        Scripter
	script        *redis.Script
	clock         Clock
	failurePolicy FailurePolicy
}

// Option configures a RedisLimiter.
type Option func(*RedisLimiter)

// WithClock sets a custom clock function for the limiter.
// Useful for testing time-dependent behavior.
func WithClock(clock Clock) Option {
	return func(l *RedisLimiter) {
		l.clock = clock
	}
}

// WithFailurePolicy sets the failure policy for when Redis is unavailable.
func WithFailurePolicy(policy FailurePolicy) Option {
	return func(l *RedisLimiter) {
		l.failurePolicy = policy
	}
}

// NewRedisLimiter creates a new Redis-backed sliding window rate limiter.
// The Lua script is registered via redis.NewScript for EVALSHA caching.
func NewRedisLimiter(client Scripter, opts ...Option) *RedisLimiter {
	l := &RedisLimiter{
		client:        client,
		script:        redis.NewScript(slidingWindowScript),
		clock:         time.Now,
		failurePolicy: FailOpen,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Allow checks whether a request identified by key is allowed under the given
// limit and window duration.
//
// Key pattern: "rl:{type}:{identifier}" (e.g., "rl:redirect:sha256(ip)", "rl:create:user:123")
//
// On Redis failure, behavior depends on the configured FailurePolicy:
//   - FailOpen: returns allowed=true (used for redirects)
//   - FailClosed: returns allowed=false (used for creation)
func (l *RedisLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (*Result, error) {
	now := l.clock()
	nowMs := now.UnixMilli()
	windowMs := window.Milliseconds()
	requestID := fmt.Sprintf("%d:%s", nowMs, uuid.New().String()[:8])

	res, err := l.script.Run(ctx, l.client, []string{key},
		nowMs,
		windowMs,
		limit,
		requestID,
	).Int64Slice()
	if err != nil {
		return l.handleFailure(now, limit, window, err)
	}

	allowed := res[0] == 1
	count := res[1]
	retryAfterMs := res[2]

	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	resetAt := now.Add(window)
	var retryAfter time.Duration
	if !allowed && retryAfterMs > 0 {
		retryAfter = time.Duration(retryAfterMs) * time.Millisecond
		resetAt = now.Add(retryAfter)
	}

	return &Result{
		Allowed:    allowed,
		Count:      count,
		Limit:      limit,
		Remaining:  remaining,
		ResetAt:    resetAt,
		RetryAfter: retryAfter,
	}, nil
}

// handleFailure returns a Result based on the configured failure policy when Redis is unavailable.
func (l *RedisLimiter) handleFailure(now time.Time, limit int64, window time.Duration, err error) (*Result, error) {
	switch l.failurePolicy {
	case FailOpen:
		return &Result{
			Allowed:   true,
			Count:     0,
			Limit:     limit,
			Remaining: limit,
			ResetAt:   now.Add(window),
		}, nil
	case FailClosed:
		return &Result{
			Allowed:    false,
			Count:      0,
			Limit:      limit,
			Remaining:  0,
			ResetAt:    now.Add(window),
			RetryAfter: window,
		}, fmt.Errorf("ratelimit: redis unavailable: %w", err)
	default:
		// Unreachable, but fail open as a safety default.
		return &Result{
			Allowed:   true,
			Count:     0,
			Limit:     limit,
			Remaining: limit,
			ResetAt:   now.Add(window),
		}, nil
	}
}
