# ADR-004: Go ARM64 Lambda Without SnapStart

## Status

Accepted

## Context

Shorty runs on AWS Lambda. We need to choose the runtime, architecture, and cold
start mitigation strategy. The redirect Lambda has the strictest latency requirement:
p99 < 100 ms at 10,000 RPS (RFC-0001). Cold starts must be kept under 500 ms.

AWS offers two CPU architectures for Lambda (x86_64 and arm64) and a cold start
optimization called SnapStart (currently available for Java and .NET managed runtimes
only -- not Go/provided.al2023).

## Decision

We deploy all Lambda functions as **statically compiled Go binaries** on **ARM64
(Graviton2)** using the `provided.al2023` runtime, with **provisioned concurrency**
on the redirect Lambda instead of SnapStart.

### Why Go

- **Fast cold starts**: Go compiles to a single static binary. Lambda cold start for
  a Go binary is typically 50-150 ms (init duration), compared to 500-2000 ms for
  Java or 200-400 ms for Node.js.
- **Low memory footprint**: Go Lambda functions run comfortably at 128-256 MB, reducing
  cost.
- **Excellent concurrency model**: goroutines for the async SQS publish pattern
  (see ADR-003) are natural in Go.
- **Strong standard library**: `net/http`, `crypto/sha256`, `encoding/json` -- no
  heavy framework dependencies that bloat binary size.

### Why ARM64 (Graviton2)

- **20% cheaper** than x86_64 at the same memory configuration.
- **Better performance/watt**: Graviton2 provides ~20% better price-performance for
  compute-bound workloads.
- **No code changes**: Go cross-compiles to ARM64 with a build flag:

  ```makefile
  build:
      GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bootstrap cmd/redirect/main.go
      zip redirect.zip bootstrap
  ```

### Why provisioned concurrency (not SnapStart)

- **SnapStart is not available for Go**: SnapStart supports Java (Corretto) and .NET
  managed runtimes. Go uses the `provided.al2023` custom runtime, which is ineligible.
- **Provisioned concurrency = 2** for the redirect Lambda ensures at least two warm
  execution environments are always available. This eliminates cold starts for the
  first two concurrent requests and covers the baseline traffic pattern.
- **Scaling beyond provisioned capacity**: on-demand Lambda instances still have fast
  cold starts (50-150 ms) because Go initialization is inherently quick. The DynamoDB
  and Redis clients are initialized outside the handler function and survive across
  warm invocations.

### Lambda initialization pattern

```go
var (
    redisClient *redis.Client
    dynamoClient *dynamodb.Client
    sqsClient   *sqs.Client
)

func init() {
    // Runs once per cold start, persists across warm invocations
    cfg, _ := config.LoadDefaultConfig(context.Background())
    dynamoClient = dynamodb.NewFromConfig(cfg)
    sqsClient = sqs.NewFromConfig(cfg)
    redisClient = redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})
}

func main() {
    lambda.Start(handler)
}
```

## Consequences

**Positive:**
- 20% cost reduction from ARM64 pricing.
- Cold starts typically 50-150 ms, well within the 500 ms target.
- Provisioned concurrency eliminates cold starts for baseline traffic.
- Simple deployment: single `bootstrap` binary in a zip file.
- No runtime version management -- Go binary is self-contained.

**Negative:**
- Provisioned concurrency has a fixed cost (~$0.015/hr per provisioned instance)
  regardless of traffic. At 2 instances, this is ~$22/month.
- VPC-attached Lambda (required for Redis) adds ~200-300 ms to cold starts on
  non-provisioned instances. This is acceptable because Go init is fast and
  provisioned concurrency covers the baseline.
- ARM64 requires cross-compilation in CI. Standard Go toolchain handles this
  natively, but any CGO dependencies would be problematic (hence `CGO_ENABLED=0`).

## Alternatives Considered

**Node.js Lambda:**
Rejected. While Node.js has reasonable cold starts (~200 ms), it lacks Go's type safety
and performance characteristics. The async SQS publish pattern is more naturally
expressed with goroutines than with Node.js Promises in a Lambda context.

**Java with SnapStart:**
Rejected. SnapStart reduces Java cold starts from ~2s to ~200ms, but Java Lambda
functions require significantly more memory (512 MB+), have larger deployment packages,
and SnapStart has limitations (no VPC support at launch, requires careful handling
of unique state). Go's natural cold start is faster than Java + SnapStart.

**x86_64 architecture:**
Rejected. No performance advantage for this workload, and 20% more expensive. Go
cross-compilation to ARM64 is trivial.

**No provisioned concurrency (rely on Go's fast cold start):**
Considered but rejected for the redirect Lambda. While 50-150 ms cold starts are fast,
VPC attachment adds 200-300 ms. Combined with the 100 ms p99 target, even rare cold
starts would violate the SLO. Provisioned concurrency at 2 instances is a small cost
for guaranteed warm starts.
