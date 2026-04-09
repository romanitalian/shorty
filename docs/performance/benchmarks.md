# Benchmark Specifications

> All benchmarks use `go test -bench` with `-benchmem` for allocation tracking.
> CI regression gate: fail if p50 degrades > 20% from baseline (use `benchstat`).

---

## Running Benchmarks

```bash
# All benchmarks with allocation reporting, 5 iterations for statistical significance
go test -bench=. -benchmem -count=5 ./internal/... ./cmd/redirect/... 2>&1 | tee bench.txt

# Compare against baseline
benchstat baseline.txt bench.txt

# Single benchmark
go test -bench=BenchmarkRedirectCacheHit -benchmem -count=5 ./cmd/redirect/...

# With CPU profile
go test -bench=BenchmarkRedirectCacheHit -cpuprofile=cpu.prof ./cmd/redirect/...
go tool pprof -http=:8080 cpu.prof

# With memory profile
go test -bench=BenchmarkRedirectCacheHit -memprofile=mem.prof ./cmd/redirect/...
go tool pprof -http=:8080 mem.prof
```

---

## 1. BenchmarkBase62Generate

**File:** `internal/shortener/bench_test.go`

**What it measures:** Raw Base62 code generation throughput (the `generateRandomCode` function). This is crypto/rand-bound, not CPU-bound.

```go
package shortener

import "testing"

func BenchmarkGenerateRandomCode(b *testing.B) {
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _, err := generateRandomCode(DefaultCodeLength)
        if err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkGenerateRandomCodeParallel(b *testing.B) {
    b.ReportAllocs()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, _ = generateRandomCode(DefaultCodeLength)
        }
    })
}
```

**Expected results:**

| Metric | Target | Notes |
|---|---|---|
| ns/op | < 5,000 | crypto/rand reads from /dev/urandom; 7 calls per code |
| B/op | <= 112 | 7 * big.Int allocation (16 bytes each) |
| allocs/op | <= 8 | 7 big.Int + 1 result byte slice |

**Location of code under test:** `internal/shortener/shortener.go:118-131`

**Key observation:** Each character requires a separate `crypto/rand.Int()` call, which involves a `big.Int` allocation. The `make([]byte, length)` at line 120 is the only necessary allocation. The `big.Int` allocations (line 121: `rand.Int(rand.Reader, charsetLen)`) could be avoided by reading 7 random bytes and using modulo-62, at the cost of slight bias. For a URL shortener, this bias is acceptable.

---

## 2. BenchmarkURLValidation

**File:** `internal/shortener/bench_test.go`

**What it measures:** Custom alias validation regex performance.

```go
func BenchmarkCustomAliasValidation(b *testing.B) {
    b.ReportAllocs()
    codes := []string{"abc123", "my-custom-link", "A1_b2", "x"}
    for i := 0; i < b.N; i++ {
        _ = customAliasPattern.MatchString(codes[i%len(codes)])
    }
}
```

**Expected results:**

| Metric | Target | Notes |
|---|---|---|
| ns/op | < 200 | Compiled regex, short strings |
| B/op | 0 | MatchString should not allocate |
| allocs/op | 0 | |

**Location of code under test:** `internal/shortener/shortener.go:24`

---

## 3. BenchmarkRedirectHandler

**File:** `cmd/redirect/bench_test.go`

**What it measures:** Full redirect handler logic with mocked I/O dependencies. Isolates Go processing overhead from network latency.

```go
package main

import (
    "context"
    "testing"
    "time"

    "github.com/aws/aws-lambda-go/events"
    "github.com/romanitalian/shorty/internal/store"
)

// mockCache implements cache.Cache with a pre-loaded link for cache hits.
type benchCache struct {
    link *store.Link
}

func (c *benchCache) GetLink(_ context.Context, _ string) (*store.Link, error) {
    return c.link, nil
}
func (c *benchCache) SetLink(_ context.Context, _ string, _ *store.Link, _ time.Duration) error {
    return nil
}
func (c *benchCache) DeleteLink(_ context.Context, _ string) error { return nil }
func (c *benchCache) SetNegative(_ context.Context, _ string) error { return nil }
func (c *benchCache) IsNegative(_ context.Context, _ string) (bool, error) { return false, nil }

// mockStore implements store.Store with IncrementClickCount returning true.
// (Only IncrementClickCount and GetLink are called on the hot path.)
type benchStore struct{}

func (s *benchStore) CreateLink(_ context.Context, _ *store.Link) error { return nil }
func (s *benchStore) GetLink(_ context.Context, _ string) (*store.Link, error) {
    return nil, store.ErrLinkNotFound
}
func (s *benchStore) UpdateLink(_ context.Context, _ string, _ string, _ map[string]interface{}) error {
    return nil
}
func (s *benchStore) DeleteLink(_ context.Context, _ string, _ string) error { return nil }
func (s *benchStore) ListLinksByOwner(_ context.Context, _ string, _ string, _ int) ([]*store.Link, string, error) {
    return nil, "", nil
}
func (s *benchStore) IncrementClickCount(_ context.Context, _ string, _ *int64) (bool, error) {
    return true, nil
}
func (s *benchStore) BatchWriteClicks(_ context.Context, _ []*store.ClickEvent) error { return nil }
func (s *benchStore) GetUser(_ context.Context, _ string) (*store.User, error) { return nil, nil }
func (s *benchStore) UpdateUserQuota(_ context.Context, _ string) error { return nil }

// mockLimiter always allows.
type benchLimiter struct{}

func (l *benchLimiter) Allow(_ context.Context, _ string, limit int64, _ time.Duration) (*ratelimit.Result, error) {
    return &ratelimit.Result{Allowed: true, Limit: limit, Remaining: limit - 1}, nil
}

func BenchmarkRedirectCacheHit(b *testing.B) {
    link := &store.Link{
        Code:        "abc1234",
        OriginalURL: "https://example.com/long-url",
        IsActive:    true,
    }
    h := NewRedirectHandler(&benchStore{}, &benchCache{link: link}, &benchLimiter{}, nil, "", "test-salt")
    req := events.APIGatewayV2HTTPRequest{
        RawPath: "/abc1234",
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
                Method:   "GET",
                SourceIP: "203.0.113.1",
            },
        },
        Headers: map[string]string{},
    }
    ctx := context.Background()

    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        resp, _ := h.Handle(ctx, req)
        if resp.StatusCode != 302 {
            b.Fatalf("expected 302, got %d", resp.StatusCode)
        }
    }
}

func BenchmarkRedirectCacheHitParallel(b *testing.B) {
    link := &store.Link{
        Code:        "abc1234",
        OriginalURL: "https://example.com/long-url",
        IsActive:    true,
    }
    h := NewRedirectHandler(&benchStore{}, &benchCache{link: link}, &benchLimiter{}, nil, "", "test-salt")
    req := events.APIGatewayV2HTTPRequest{
        RawPath: "/abc1234",
        RequestContext: events.APIGatewayV2HTTPRequestContext{
            HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
                Method:   "GET",
                SourceIP: "203.0.113.1",
            },
        },
        Headers: map[string]string{},
    }
    ctx := context.Background()

    b.ReportAllocs()
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, _ = h.Handle(ctx, req)
        }
    })
}
```

**Expected results:**

| Benchmark | ns/op Target | B/op Target | allocs/op Target |
|---|---|---|---|
| BenchmarkRedirectCacheHit | < 2,000 | < 2,048 | < 15 |
| BenchmarkRedirectCacheHitParallel | < 3,000 | < 2,048 | < 15 |

**Note:** These targets measure pure Go processing with mocked I/O. Real-world latency is dominated by network calls (Redis, DynamoDB), which are excluded.

---

## 4. BenchmarkCacheSerialization

**File:** `internal/cache/bench_test.go`

**What it measures:** JSON marshal/unmarshal of the `cachedLink` struct, which happens on every cache hit (unmarshal) and miss (marshal + unmarshal on next hit).

```go
package cache

import (
    "encoding/json"
    "testing"

    "github.com/romanitalian/shorty/internal/store"
)

var benchLink = &store.Link{
    Code:        "abc1234",
    OriginalURL: "https://example.com/very/long/url/path?param1=value1&param2=value2",
    IsActive:    true,
    ClickCount:  42,
    UTMSource:   "twitter",
    UTMMedium:   "social",
    UTMCampaign: "launch2024",
}

func BenchmarkCacheMarshal(b *testing.B) {
    cl := cachedLink{
        OriginalURL: benchLink.OriginalURL,
        IsActive:    benchLink.IsActive,
        ClickCount:  benchLink.ClickCount,
        UTMSource:   benchLink.UTMSource,
        UTMMedium:   benchLink.UTMMedium,
        UTMCampaign: benchLink.UTMCampaign,
    }
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := json.Marshal(cl)
        if err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkCacheUnmarshal(b *testing.B) {
    cl := cachedLink{
        OriginalURL: benchLink.OriginalURL,
        IsActive:    benchLink.IsActive,
        ClickCount:  benchLink.ClickCount,
        UTMSource:   benchLink.UTMSource,
        UTMMedium:   benchLink.UTMMedium,
        UTMCampaign: benchLink.UTMCampaign,
    }
    data, _ := json.Marshal(cl)

    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        var out cachedLink
        if err := json.Unmarshal(data, &out); err != nil {
            b.Fatal(err)
        }
    }
}
```

**Expected results:**

| Benchmark | ns/op Target | B/op Target | allocs/op Target |
|---|---|---|---|
| BenchmarkCacheMarshal | < 500 | < 256 | < 3 |
| BenchmarkCacheUnmarshal | < 1,000 | < 512 | < 10 |

**Optimization note:** If unmarshal proves expensive (> 1 us), consider switching from JSON to a binary format like MessagePack or a hand-written serializer. The `cachedLink` struct (cache.go:55-65) has a fixed schema that makes hand-rolled serialization straightforward.

---

## 5. BenchmarkIPHash

**File:** `cmd/redirect/bench_test.go`

**What it measures:** SHA-256 IP hashing performance. Called once per redirect request.

```go
func BenchmarkIPHash(b *testing.B) {
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = HashIP("203.0.113.42", "my-secret-salt-value-here")
    }
}

func BenchmarkIPHashParallel(b *testing.B) {
    b.ReportAllocs()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _ = HashIP("203.0.113.42", "my-secret-salt-value-here")
        }
    })
}
```

**Expected results:**

| Benchmark | ns/op Target | B/op Target | allocs/op Target |
|---|---|---|---|
| BenchmarkIPHash | < 500 | < 200 | < 3 |
| BenchmarkIPHashParallel | < 300 | < 200 | < 3 |

**Location of code under test:** `cmd/redirect/main.go:97-102`

**Allocation sources in HashIP:**
1. `sha256.New()` - allocates digest state (~100 bytes)
2. `h.Sum(nil)` - allocates result slice (32 bytes)
3. `hex.EncodeToString()` - allocates hex string (64 bytes)

**Optimization:** Use `sha256.Sum256([]byte)` instead of `sha256.New()` + `Write` + `Sum` to avoid the hash state allocation. However, this requires concatenating salt+ip into a single byte slice first. With `sync.Pool` for the hash state, allocations drop to 1 (the hex string).

---

## 6. BenchmarkRateLimitLua

**File:** `internal/ratelimit/bench_test.go`

**What it measures:** Full rate limiter path including Redis Lua script execution. Requires a running Redis instance.

```go
package ratelimit

import (
    "context"
    "fmt"
    "testing"
    "time"

    "github.com/redis/go-redis/v9"
)

func BenchmarkRateLimiterAllow(b *testing.B) {
    // Requires Redis running at localhost:6379
    client := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    ctx := context.Background()
    if err := client.Ping(ctx).Err(); err != nil {
        b.Skip("Redis not available, skipping benchmark")
    }
    defer client.Close()

    limiter := NewRedisLimiter(client)

    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        key := fmt.Sprintf("bench:rl:%d", i%1000) // spread across 1000 keys
        _, err := limiter.Allow(ctx, key, 10000, 1*time.Minute)
        if err != nil {
            b.Fatal(err)
        }
    }
    b.StopTimer()

    // Cleanup
    client.FlushDB(ctx)
}

// BenchmarkRateLimiterKeyConstruction measures only the Go-side overhead
// (key building, UUID generation, result parsing) without Redis.
func BenchmarkRateLimiterKeyConstruction(b *testing.B) {
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = fmt.Sprintf("rl:redirect:%s", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
    }
}
```

**Expected results:**

| Benchmark | ns/op Target | Notes |
|---|---|---|
| BenchmarkRateLimiterAllow | < 500,000 (0.5 ms) | Includes Redis roundtrip (localhost) |
| BenchmarkRateLimiterKeyConstruction | < 200 | Pure string formatting |

**Run command (requires Redis):**
```bash
make dev-up  # start Redis via Docker Compose
go test -bench=BenchmarkRateLimiter -benchmem -count=5 ./internal/ratelimit/...
```

---

## 7. BenchmarkExtractCode

**File:** `cmd/redirect/bench_test.go`

**What it measures:** Path parsing for short code extraction.

```go
func BenchmarkExtractCode(b *testing.B) {
    paths := []string{"/abc1234", "/x", "/my-custom-link", "/abc?foo=bar"}
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = extractCode(paths[i%len(paths)])
    }
}
```

**Expected results:**

| Metric | Target |
|---|---|
| ns/op | < 50 |
| allocs/op | 0 |

**Location of code under test:** `cmd/redirect/main.go:222-229`

---

## 8. BenchmarkBuildRedirectURL

**File:** `cmd/redirect/bench_test.go`

**What it measures:** URL construction with UTM parameter appending.

```go
func BenchmarkBuildRedirectURLNoUTM(b *testing.B) {
    link := &store.Link{OriginalURL: "https://example.com/page"}
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = buildRedirectURL(link)
    }
}

func BenchmarkBuildRedirectURLWithUTM(b *testing.B) {
    link := &store.Link{
        OriginalURL: "https://example.com/page?existing=param",
        UTMSource:   "twitter",
        UTMMedium:   "social",
        UTMCampaign: "launch",
    }
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = buildRedirectURL(link)
    }
}
```

**Expected results:**

| Benchmark | ns/op Target | allocs/op Target |
|---|---|---|
| NoUTM | < 10 | 0 (fast path: returns OriginalURL directly) |
| WithUTM | < 2,000 | < 5 (url.Parse + Query + Encode) |

**Location of code under test:** `cmd/redirect/main.go:296-318`

---

## CI Integration

### Saving Baselines

```bash
# After a release, save the benchmark baseline
go test -bench=. -benchmem -count=10 ./... > benchmarks/baseline-v1.0.txt

# On PR, compare
go test -bench=. -benchmem -count=10 ./... > benchmarks/pr.txt
benchstat benchmarks/baseline-v1.0.txt benchmarks/pr.txt
```

### Regression Detection

Add to CI pipeline:
```yaml
- name: Benchmark regression check
  run: |
    go test -bench=. -benchmem -count=5 ./... > /tmp/current.txt
    benchstat benchmarks/baseline.txt /tmp/current.txt | grep -E "^\w" | while read line; do
      # Parse benchstat output for >20% degradation
      echo "$line"
    done
```

### Install benchstat

```bash
go install golang.org/x/perf/cmd/benchstat@latest
```
