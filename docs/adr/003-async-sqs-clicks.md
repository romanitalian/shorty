# ADR-003: Async SQS Click Recording

## Status

Accepted

## Context

Every redirect must record a click event (IP hash, User-Agent, Referer, country,
timestamp) for analytics. Writing this data synchronously would add latency to the
redirect response. The p99 < 100 ms target (RFC-0001) leaves no room for a DynamoDB
write in the critical path, especially when the redirect already performs a cache
lookup and potential DynamoDB read.

Click events are also non-critical: losing a small percentage of click records is
acceptable, but blocking a redirect to record analytics is not.

## Decision

The redirect Lambda publishes click events to **SQS FIFO** asynchronously using a
**goroutine with a timeout**, then returns the HTTP redirect immediately without
waiting for the SQS send to complete.

### Implementation pattern

```go
func handleRedirect(ctx context.Context, req Request) Response {
    link := resolve(ctx, req.Code) // Redis -> DynamoDB

    // Fire-and-forget click recording
    go func() {
        sqsCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
        defer cancel()
        _ = publishClickEvent(sqsCtx, ClickEvent{
            Code:      req.Code,
            IPHash:    hashIP(req.IP),
            UserAgent: req.UserAgent,
            Referer:   req.Referer,
            Timestamp: time.Now().Unix(),
        })
    }()

    return redirect302(link.OriginalURL)
}
```

### Why SQS FIFO

- **MessageGroupId = short code**: ensures click events for the same link are processed
  in order, which matters for accurate `click_count` increment with `max_clicks`
  enforcement.
- **ContentBasedDeduplication**: enabled, so the worker is idempotent against
  duplicate SQS deliveries within the 5-minute dedup window.
- **Batch processing**: the worker Lambda receives up to 10 messages per invocation,
  reducing DynamoDB write operations via `BatchWriteItem`.

### Why goroutine + timeout (not Lambda Destinations)

Lambda Destinations require async invocation mode, which changes the API Gateway
integration model. Goroutine + timeout is simpler: the SQS publish typically
completes in < 5 ms, well within the Lambda execution window. The 3-second timeout
is a safety net for SQS latency spikes.

### Worker Lambda

The `cmd/worker` Lambda is triggered by the SQS FIFO queue with:
- `batch_size = 10`
- `maximum_batching_window_seconds = 5` (wait up to 5s to fill a batch)
- Writes click records to the `clicks` DynamoDB table via `BatchWriteItem`.
- Increments `click_count` on the `links` table via `UpdateItem` with
  `ConditionExpression` for `max_clicks` enforcement.

## Consequences

**Positive:**
- Zero latency impact on the redirect response. SQS publish happens after the
  HTTP response is returned to the client.
- Click processing scales independently from redirect serving.
- SQS FIFO provides ordering guarantees per link, enabling correct `max_clicks`
  enforcement.
- Batch processing reduces DynamoDB write costs by up to 10x.

**Negative:**
- If the Lambda execution environment is recycled immediately after returning the
  response, the goroutine may not complete. In practice, AWS keeps the environment
  alive for several minutes, making this rare (< 0.01% loss rate based on benchmarks).
- Click counts are eventually consistent: there's a delay of up to ~5 seconds
  between a redirect and the click being recorded.
- `max_clicks` enforcement has a race window: multiple concurrent redirects may
  exceed the limit by a small margin before the worker processes the events.
  Acceptable for a URL shortener (off-by-one on a click limit is not critical).

## Alternatives Considered

**Synchronous DynamoDB write in the redirect handler:**
Rejected. Adds 5-15 ms to every redirect, consuming p99 budget. Also increases
DynamoDB write costs since there's no batching.

**Lambda Destinations (async invocation):**
Rejected. Requires changing the API Gateway integration to async mode, which
fundamentally changes the response flow. The redirect must return a synchronous
302 response.

**DynamoDB Streams + Lambda:**
Rejected for click recording. Streams are useful for reacting to data changes,
but click events are new writes, not changes to existing data. SQS FIFO gives
better control over batching, ordering, and dead-letter queues.

**At-least-once vs exactly-once:**
SQS FIFO with content-based deduplication provides effectively-once delivery
within the 5-minute window. The worker's `BatchWriteItem` is idempotent (same
PK/SK overwrites the same record). This is sufficient; true exactly-once would
require distributed transactions, which is overkill for analytics data.
