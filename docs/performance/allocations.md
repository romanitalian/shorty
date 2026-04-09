# Heap Allocation Analysis & Escape Analysis

> Go's GC on Lambda (single vCPU at 512 MB) can introduce 1-5 ms pauses under allocation pressure.
> Goal: minimize allocations on the redirect hot path to keep GC pauses negligible.

---

## Running Escape Analysis

```bash
# Full escape analysis for the redirect Lambda
go build -gcflags='-m=2' ./cmd/redirect/ 2>&1 | grep "escapes to heap"

# Verbose (shows inlining decisions too)
go build -gcflags='-m=2' ./cmd/redirect/ 2>&1 | head -200

# Per-package analysis
go build -gcflags='-m=2' ./internal/cache/ 2>&1 | grep -E "(escapes|moved to heap)"
go build -gcflags='-m=2' ./internal/ratelimit/ 2>&1 | grep -E "(escapes|moved to heap)"
go build -gcflags='-m=2' ./internal/store/ 2>&1 | grep -E "(escapes|moved to heap)"
```

---

## Hot Path Allocation Inventory

The redirect handler (`cmd/redirect/main.go:105-218`) processes every single redirect request. Each allocation here multiplies by the full RPS (target: 10,000).

### Per-Request Allocations (Current)

| # | Location | Allocation | Size | Fix |
|---|---|---|---|---|
| 1 | `main.go:123` | `fmt.Sprintf("rl:redirect:%s", ipHash)` | ~80 B | String concat |
| 2 | `main.go:97-101` | `sha256.New()` | ~108 B | `sha256.Sum256()` |
| 3 | `main.go:101` | `h.Sum(nil)` result slice | 32 B | Pre-alloc buffer |
| 4 | `main.go:101` | `hex.EncodeToString()` | 64 B | Pre-alloc buffer |
| 5 | `ratelimit.go:153` | `uuid.New().String()[:8]` | 36 B (UUID string) | Counter-based ID |
| 6 | `ratelimit.go:153` | `fmt.Sprintf("%d:%s", nowMs, ...)` | ~32 B | strings.Builder |
| 7 | `cache.go:71` | `client.Get().Bytes()` result | ~200 B | Unavoidable (Redis data) |
| 8 | `cache.go:82` | `json.Unmarshal(&cl)` | ~300 B | Consider msgpack |
| 9 | `cache.go:87-98` | `&store.Link{...}` (pointer return) | ~200 B | Return value type |
| 10 | `main.go:109` | `jsonResponse` map[string]string header | ~64 B | Only on error path |
| 11 | `main.go:211-216` | Response `map[string]string` headers | ~64 B | Unavoidable (API GW contract) |
| 12 | `store.go:323-347` | DynamoDB UpdateItem input construction | ~500 B | Unavoidable (SDK) |

**Estimated total per request (cache HIT):** ~1,100 bytes, ~12 allocations
**Estimated total per request (cache MISS):** ~2,000 bytes, ~20 allocations

At 10,000 RPS: ~11 MB/s (HIT) to ~20 MB/s (MISS) of allocation throughput. This is well within Go's GC capacity, but reducing it will lower GC pause frequency.

---

## Optimization Details

### O1: Replace `fmt.Sprintf` with string concatenation

**Location:** `cmd/redirect/main.go:123`

```go
// BEFORE (1 alloc, ~80 bytes)
rlKey := fmt.Sprintf("rl:redirect:%s", ipHash)

// AFTER (1 alloc, ~76 bytes — but avoids fmt machinery overhead)
rlKey := "rl:redirect:" + ipHash
```

`fmt.Sprintf` with a single `%s` verb internally allocates a `[]byte` buffer, formats into it, then converts to string. Simple concatenation with `+` is optimized by the Go compiler into a single `runtime.concatstrings` call.

**Savings:** ~200 ns/op from avoiding fmt reflection, same 1 alloc but smaller.

**Other `fmt.Sprintf` instances on the hot path:**

| Location | Current | Replacement |
|---|---|---|
| `ratelimit.go:153` | `fmt.Sprintf("%d:%s", nowMs, uuid...)` | `strconv.FormatInt(nowMs, 10) + ":" + id` |
| `store.go:330` | `fmt.Sprintf("%d", now)` | `strconv.FormatInt(now, 10)` |

### O2: Optimize HashIP to reduce allocations

**Location:** `cmd/redirect/main.go:97-102`

```go
// BEFORE (3 allocs: sha256 state, Sum result, hex string)
func HashIP(ip string, salt string) string {
    h := sha256.New()
    h.Write([]byte(salt))
    h.Write([]byte(ip))
    return hex.EncodeToString(h.Sum(nil))
}

// AFTER (1 alloc: hex string only)
func HashIP(ip string, salt string) string {
    // Concatenate salt+ip into a stack-allocated buffer (max ~128 bytes for IP+salt)
    var buf [128]byte
    n := copy(buf[:], salt)
    n += copy(buf[n:], ip)
    sum := sha256.Sum256(buf[:n])
    return hex.EncodeToString(sum[:])
}
```

`sha256.Sum256()` returns a `[32]byte` value type (stack-allocated), avoiding the `sha256.New()` heap allocation. The `hex.EncodeToString` allocation remains (64 bytes) but is unavoidable without a pooled buffer.

**Savings:** 2 allocations, ~140 bytes per request.

**Caveat:** The fixed-size `[128]byte` buffer assumes `len(salt) + len(ip) <= 128`. The salt is typically 32-64 chars and IPv6 is max 45 chars, so 128 is safe. Add a runtime check or fall back to the heap path for oversized inputs.

### O3: Replace UUID generation in rate limiter

**Location:** `internal/ratelimit/ratelimit.go:153`

```go
// BEFORE (multiple allocs: uuid bytes, uuid string, fmt.Sprintf)
requestID := fmt.Sprintf("%d:%s", nowMs, uuid.New().String()[:8])

// AFTER (1 alloc: the final string)
// Use a simple counter + timestamp for sorted set uniqueness.
// The member only needs to be unique within one key's window.
var buf [32]byte
n := strconv.AppendInt(buf[:0], nowMs, 10)
n = append(n, ':')
// 4 random bytes = 8 hex chars, sufficient uniqueness
var rnd [4]byte
_, _ = cryptorand.Read(rnd[:])
n = append(n, hex.EncodeToString(rnd[:])...)
requestID := string(n)
```

**Savings:** Eliminates `uuid.New()` (128-bit generation + 36-char format) and `fmt.Sprintf`. Net reduction: ~2 allocations, ~100 bytes.

### O4: sync.Pool for JSON encoder/decoder in cache

**Location:** `internal/cache/cache.go:82, 128`

```go
// Pool for JSON decoders is not beneficial here because json.Unmarshal
// does not accept a reusable decoder. However, we can pool byte buffers
// for the marshal output.

var marshalBufPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 0, 256)
        return &buf
    },
}

func (c *RedisCache) SetLink(ctx context.Context, code string, link *store.Link, ttl time.Duration) error {
    // ... TTL computation ...
    
    bufPtr := marshalBufPool.Get().(*[]byte)
    defer func() {
        *bufPtr = (*bufPtr)[:0]
        marshalBufPool.Put(bufPtr)
    }()
    
    var err error
    *bufPtr, err = json.Marshal(cl) // json.Marshal returns a new slice but reuse reduces pressure
    // ... rest of method ...
}
```

**Assessment:** Marginal benefit. `json.Marshal` allocates its own buffer internally, so pooling the output buffer only saves the final copy. The real win would be switching to a zero-allocation serializer (e.g., hand-written or `encoding/binary`).

**Recommendation:** Low priority. The JSON payload is small (~200 bytes), and marshal/unmarshal runs in < 1 us. Only optimize if benchmarks show it as a bottleneck.

### O5: Return value type instead of pointer from cache

**Location:** `internal/cache/cache.go:87-98`

```go
// BEFORE: returns *store.Link (escapes to heap)
link := &store.Link{
    Code:         code,
    OriginalURL:  cl.OriginalURL,
    // ...
}
return link, nil

// AFTER: return value type (stays on stack if caller doesn't take address)
// This requires changing the Cache interface:
//   GetLink(ctx, code) (store.Link, bool, error)
// where bool indicates cache hit.
```

**Assessment:** The `store.Link` struct has 14 fields totaling ~200 bytes. Returning by value means copying 200 bytes vs one 8-byte pointer. At 10,000 RPS, the copy cost is negligible compared to the GC benefit of avoiding heap allocation. However, this requires an interface change that propagates to all implementations and callers.

**Recommendation:** Medium priority. Implement if cache serialization shows up in allocation profiles. The interface change is safe since both the cache and store return `*store.Link` -- change both simultaneously.

---

## Struct Field Ordering for Alignment

### store.Link (current)

**Location:** `internal/store/models.go:5-22`

```go
type Link struct {
    PK           string  // 16 bytes (string header)
    SK           string  // 16 bytes
    OwnerID      string  // 16 bytes
    OriginalURL  string  // 16 bytes
    Code         string  // 16 bytes
    Title        string  // 16 bytes
    PasswordHash string  // 16 bytes
    ExpiresAt    *int64  // 8 bytes
    MaxClicks    *int64  // 8 bytes
    ClickCount   int64   // 8 bytes
    IsActive     bool    // 1 byte + 7 padding
    UTMSource    string  // 16 bytes
    UTMMedium    string  // 16 bytes
    UTMCampaign  string  // 16 bytes
    CreatedAt    int64   // 8 bytes
    UpdatedAt    int64   // 8 bytes
}
```

**Current size:** Let's check alignment.

```bash
# Check struct sizes and alignment
go vet -vettool=$(which fieldalignment) ./internal/store/...
# Or use the standalone tool:
go install golang.org/x/tools/go/analysis/passes/fieldalignment/cmd/fieldalignment@latest
fieldalignment ./internal/store/...
```

The current ordering groups strings together, which is already well-aligned since strings are 16 bytes (pointer + length). The `bool` field at position 10 causes 7 bytes of padding before the next `string` field.

**Optimized ordering:**

```go
type Link struct {
    // 16-byte aligned fields (strings)
    PK           string
    SK           string
    OwnerID      string
    OriginalURL  string
    Code         string
    Title        string
    PasswordHash string
    UTMSource    string
    UTMMedium    string
    UTMCampaign  string
    // 8-byte aligned fields
    ExpiresAt    *int64
    MaxClicks    *int64
    ClickCount   int64
    CreatedAt    int64
    UpdatedAt    int64
    // 1-byte fields (pack at end to minimize padding)
    IsActive     bool
}
```

**Savings:** Moving `IsActive` to the end saves 7 bytes of padding per struct instance. At ~200 bytes per struct, this is a ~3.5% size reduction. Marginal, but correct practice.

### cache.cachedLink (current)

**Location:** `internal/cache/cache.go:55-65`

The `cachedLink` struct is smaller and has the same alignment concern with `IsActive` (bool) at position 5:

```go
type cachedLink struct {
    OriginalURL  string  // 16 B
    PasswordHash string  // 16 B
    ExpiresAt    *int64  // 8 B
    MaxClicks    *int64  // 8 B
    ClickCount   int64   // 8 B
    IsActive     bool    // 1 B + 7 padding
    UTMSource    string  // 16 B
    UTMMedium    string  // 16 B
    UTMCampaign  string  // 16 B
}
```

**Optimized:** Move `IsActive` to end of struct.

---

## String Interning Opportunities

### Redis Key Prefixes

**Locations:**
- `internal/cache/cache.go:17`: `linkKeyPrefix = "link:"`
- `internal/cache/cache.go:19`: `negKeyPrefix = "neg:"`

These are already constants. The key construction `linkKeyPrefix + code` uses Go's string concatenation, which allocates one new string. Since the code varies per request, there's no interning opportunity for the full key.

### DynamoDB Key Patterns

**Location:** `internal/store/store.go:73, 100, 161, etc.`

The string `"LINK#"` appears in multiple places: `CreateLink`, `GetLink`, `UpdateLink`, `DeleteLink`, `IncrementClickCount`. Each call constructs `"LINK#" + code`.

```go
// Current: repeated string concat
"LINK#" + code  // appears 6+ times across store methods
"META"           // appears 6+ times as SK value
```

The `"META"` string is a constant literal and Go will intern it. The `"LINK#" + code` concatenation allocates once per call. These are unavoidable without a key pool.

### Interning for AttributeValue keys

**Location:** `internal/store/store.go:102-105`

```go
Key: map[string]types.AttributeValue{
    "PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
    "SK": &types.AttributeValueMemberS{Value: "META"},
},
```

The map literal allocates a new map each call. The string keys `"PK"` and `"SK"` are compiler-interned. The `types.AttributeValueMemberS` values escape to heap due to the interface boxing (`types.AttributeValue` is an interface).

**No practical optimization:** These allocations are inherent to the AWS SDK's type system. The DynamoDB client requires `map[string]types.AttributeValue` input.

---

## GC Tuning for Lambda

### GOGC Setting

Lambda functions have limited memory. The default `GOGC=100` triggers GC when the heap doubles. For a redirect Lambda with ~5 MB live heap:

| GOGC | GC trigger | GC frequency at 10K RPS | Pause impact |
|---|---|---|---|
| 100 (default) | ~10 MB | Every ~500 ms | ~0.5 ms pauses, acceptable |
| 200 | ~15 MB | Every ~1 s | Fewer pauses, more memory |
| 50 | ~7.5 MB | Every ~250 ms | More pauses, less memory |

**Recommendation:** Keep `GOGC=100` (default). The allocation rate on the redirect path (~11 MB/s at 10K RPS) is modest. If profiling shows GC pauses > 2 ms, increase to `GOGC=200`.

Set via Lambda environment variable:
```
GOGC=100
```

### GOMEMLIMIT

Go 1.19+ supports `GOMEMLIMIT` as a soft memory limit. For a 512 MB Lambda:
- Runtime overhead: ~50 MB
- Application heap target: ~400 MB
- Set `GOMEMLIMIT=400MiB`

This prevents OOM kills while allowing the GC to use available memory efficiently.

---

## Allocation Reduction Priority

| Priority | Optimization | Allocs Saved | Effort |
|---|---|---|---|
| HIGH | O1: Replace fmt.Sprintf with concat | 1/req | Trivial (line change) |
| HIGH | O2: Optimize HashIP | 2/req | Small (function rewrite) |
| MEDIUM | O3: Replace UUID in rate limiter | 2/req | Small (ratelimit.go change) |
| LOW | O4: sync.Pool for JSON buffers | 0-1/req | Medium (interface change) |
| LOW | O5: Return Link by value | 1/req | Large (interface change) |
| LOW | Struct field reordering | 7 B/struct | Trivial |

**Quick wins (O1 + O2 + O3):** Save ~5 allocations and ~320 bytes per request with minimal code changes. Implement first.
