package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/romanitalian/shorty/internal/store"
)

const (
	// linkKeyPrefix is the Redis key prefix for cached link records.
	linkKeyPrefix = "link:"
	// negKeyPrefix is the Redis key prefix for negative cache entries.
	negKeyPrefix = "neg:"
	// defaultTTL is the maximum cache TTL for active links without expiry.
	defaultTTL = 5 * time.Minute
	// negativeTTL is the TTL for negative cache entries (non-existent codes).
	negativeTTL = 60 * time.Second
)

// Cache defines the interface for the Redis cache adapter.
type Cache interface {
	GetLink(ctx context.Context, code string) (*store.Link, error)
	SetLink(ctx context.Context, code string, link *store.Link, ttl time.Duration) error
	DeleteLink(ctx context.Context, code string) error
	SetNegative(ctx context.Context, code string) error
	IsNegative(ctx context.Context, code string) (bool, error)
}

// RedisClient is the subset of the go-redis client used by the cache.
// Defined here for testability.
type RedisClient interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// RedisCache implements Cache using go-redis/v9.
type RedisCache struct {
	client RedisClient
}

// NewRedisCache creates a new Redis-backed Cache.
func NewRedisCache(client RedisClient) *RedisCache {
	return &RedisCache{client: client}
}

// cachedLink contains only the fields needed for the redirect hot path.
// Serialized as JSON in Redis under key "link:{code}".
type cachedLink struct {
	OriginalURL  string `json:"original_url"`
	PasswordHash string `json:"password_hash,omitempty"`
	ExpiresAt    *int64 `json:"expires_at,omitempty"`
	MaxClicks    *int64 `json:"max_clicks,omitempty"`
	ClickCount   int64  `json:"click_count"`
	IsActive     bool   `json:"is_active"`
	UTMSource    string `json:"utm_source,omitempty"`
	UTMMedium    string `json:"utm_medium,omitempty"`
	UTMCampaign  string `json:"utm_campaign,omitempty"`
}

// GetLink retrieves a cached link from Redis.
// Returns (nil, nil) on cache miss -- callers should fall through to DynamoDB.
// Cache failures are treated as misses (graceful degradation).
func (c *RedisCache) GetLink(ctx context.Context, code string) (*store.Link, error) {
	key := linkKeyPrefix + code
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // cache miss
		}
		// Redis failure: treat as cache miss, do not propagate error.
		return nil, nil
	}

	var cl cachedLink
	if err := json.Unmarshal(data, &cl); err != nil {
		// Corrupted cache entry: treat as miss.
		return nil, nil
	}

	link := &store.Link{
		Code:         code,
		OriginalURL:  cl.OriginalURL,
		PasswordHash: cl.PasswordHash,
		ExpiresAt:    cl.ExpiresAt,
		MaxClicks:    cl.MaxClicks,
		ClickCount:   cl.ClickCount,
		IsActive:     cl.IsActive,
		UTMSource:    cl.UTMSource,
		UTMMedium:    cl.UTMMedium,
		UTMCampaign:  cl.UTMCampaign,
	}

	return link, nil
}

// SetLink caches a link record in Redis.
// TTL = min(link.ExpiresAt - now, 5 minutes).
// Cache failures are silently ignored (graceful degradation).
func (c *RedisCache) SetLink(ctx context.Context, code string, link *store.Link, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = computeTTL(link)
	}
	if ttl <= 0 {
		return nil // link already expired, don't cache
	}

	cl := cachedLink{
		OriginalURL:  link.OriginalURL,
		PasswordHash: link.PasswordHash,
		ExpiresAt:    link.ExpiresAt,
		MaxClicks:    link.MaxClicks,
		ClickCount:   link.ClickCount,
		IsActive:     link.IsActive,
		UTMSource:    link.UTMSource,
		UTMMedium:    link.UTMMedium,
		UTMCampaign:  link.UTMCampaign,
	}

	data, err := json.Marshal(cl)
	if err != nil {
		return fmt.Errorf("cache.SetLink: marshal: %w", err)
	}

	key := linkKeyPrefix + code
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		// Redis failure: silently ignore (graceful degradation).
		return nil
	}

	return nil
}

// DeleteLink removes a cached link from Redis.
// Used for cache invalidation on link update/deactivation.
// Failures are silently ignored.
func (c *RedisCache) DeleteLink(ctx context.Context, code string) error {
	key := linkKeyPrefix + code
	if err := c.client.Del(ctx, key).Err(); err != nil {
		// Redis failure: silently ignore.
		return nil
	}
	return nil
}

// SetNegative sets a negative cache entry for a non-existent code.
// Prevents repeated DynamoDB reads for bots scanning random codes.
// Key: "neg:{code}" with 60s TTL.
func (c *RedisCache) SetNegative(ctx context.Context, code string) error {
	key := negKeyPrefix + code
	if err := c.client.Set(ctx, key, "1", negativeTTL).Err(); err != nil {
		// Redis failure: silently ignore.
		return nil
	}
	return nil
}

// IsNegative checks if a negative cache entry exists for a code.
// Returns (true, nil) if the code is negatively cached (known to not exist).
// Returns (false, nil) on cache miss or Redis failure.
func (c *RedisCache) IsNegative(ctx context.Context, code string) (bool, error) {
	key := negKeyPrefix + code
	_, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		// Redis failure: treat as not negatively cached.
		return false, nil
	}
	return true, nil
}

// computeTTL calculates the cache TTL for a link.
// Returns min(link.ExpiresAt - now, defaultTTL).
func computeTTL(link *store.Link) time.Duration {
	if link.ExpiresAt == nil {
		return defaultTTL
	}

	remaining := time.Until(time.Unix(*link.ExpiresAt, 0))
	if remaining <= 0 {
		return 0
	}
	if remaining < defaultTTL {
		return remaining
	}
	return defaultTTL
}
