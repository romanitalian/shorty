# DynamoDB Atomic Operation Patterns

Go code snippets using `aws-sdk-go-v2/service/dynamodb`. Every pattern includes the DynamoDB expression, Go implementation, and error handling.

---

## Pattern 1: Conditional PutItem -- Link Creation with Collision Check

Prevents two concurrent requests from creating the same short code.

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// ErrCodeCollision indicates the generated short code already exists.
var ErrCodeCollision = errors.New("short code already exists")

// CreateLink inserts a new link with collision-safe conditional write.
//
// DynamoDB expression:
//   ConditionExpression: attribute_not_exists(PK)
//
// Cost: 1 WCU (item < 1 KB)
func (s *LinkStore) CreateLink(ctx context.Context, link *Link) error {
	item, err := attributevalue.MarshalMap(link)
	if err != nil {
		return fmt.Errorf("marshal link: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.tableName),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(PK)"),
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return ErrCodeCollision
		}
		return fmt.Errorf("put link: %w", err)
	}

	return nil
}

// CreateLinkWithRetry retries code generation on collision.
// Max 3 retries, then extends code length by 1 character.
func (s *LinkStore) CreateLinkWithRetry(ctx context.Context, link *Link, generateCode func(length int) string) error {
	codeLength := 7
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt == maxRetries {
			codeLength++ // extend code length on final retry
		}

		link.Code = generateCode(codeLength)
		link.PK = "LINK#" + link.Code
		link.SK = "META"

		err := s.CreateLink(ctx, link)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrCodeCollision) {
			return err // non-collision error, do not retry
		}
		// collision -- retry with new code
	}

	return fmt.Errorf("failed to create link after %d retries: %w", maxRetries+1, ErrCodeCollision)
}
```

---

## Pattern 2: Atomic Counter Increment -- Click Count

Uses DynamoDB `ADD` (via SET arithmetic) for atomic increment. No read-modify-write race.

```go
// ErrLinkInactiveOrLimitReached indicates the link is deactivated or has
// reached its max_clicks limit.
var ErrLinkInactiveOrLimitReached = errors.New("link inactive or click limit reached")

// IncrementClickCount atomically increments click_count with max_clicks enforcement.
//
// DynamoDB expressions:
//   UpdateExpression: SET click_count = click_count + :one, updated_at = :now
//   ConditionExpression: is_active = :true_val
//                        AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)
//
// Cost: 1 WCU
func (s *LinkStore) IncrementClickCount(ctx context.Context, code string) error {
	now := time.Now().Unix()

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
			"SK": &types.AttributeValueMemberS{Value: "META"},
		},
		UpdateExpression: aws.String("SET click_count = click_count + :one, updated_at = :now"),
		ConditionExpression: aws.String(
			"is_active = :true_val AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)",
		),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one":      &types.AttributeValueMemberN{Value: "1"},
			":true_val": &types.AttributeValueMemberBOOL{Value: true},
			":now":      &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
		},
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return ErrLinkInactiveOrLimitReached
		}
		return fmt.Errorf("increment click count for %s: %w", code, err)
	}

	return nil
}
```

---

## Pattern 3: Conditional Update -- max_clicks Enforcement with Deactivation

When `click_count` reaches `max_clicks`, the next increment attempt fails. The caller returns HTTP 410 Gone. Optionally, the worker can also set `is_active = false` when it detects the condition failure:

```go
// DeactivateOnClickLimit sets is_active=false if click_count >= max_clicks.
// Called by the worker after a ConditionalCheckFailedException on click increment.
//
// DynamoDB expressions:
//   UpdateExpression: SET is_active = :false_val, updated_at = :now
//   ConditionExpression: attribute_exists(max_clicks)
//                        AND click_count >= max_clicks
//                        AND is_active = :true_val
//
// Cost: 1 WCU. Idempotent -- no-op if already deactivated.
func (s *LinkStore) DeactivateOnClickLimit(ctx context.Context, code string) error {
	now := time.Now().Unix()

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
			"SK": &types.AttributeValueMemberS{Value: "META"},
		},
		UpdateExpression: aws.String("SET is_active = :false_val, updated_at = :now"),
		ConditionExpression: aws.String(
			"attribute_exists(max_clicks) AND click_count >= max_clicks AND is_active = :true_val",
		),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":false_val": &types.AttributeValueMemberBOOL{Value: false},
			":true_val":  &types.AttributeValueMemberBOOL{Value: true},
			":now":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
		},
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return nil // already deactivated -- idempotent
		}
		return fmt.Errorf("deactivate link %s on click limit: %w", code, err)
	}
	return nil
}
```

---

## Pattern 4: BatchWriteItem with Partial Failure Handling

SQS worker writes click events in batches of up to 25 items. Partial failures must be retried.

```go
import (
	"math"
	"math/rand"
)

// BatchWriteClicks writes up to 25 click events to the clicks table.
// Retries UnprocessedItems with exponential backoff + jitter.
//
// Cost: 1 WCU per item. Max 25 WCU per batch.
func (s *ClickStore) BatchWriteClicks(ctx context.Context, clicks []ClickEvent) error {
	if len(clicks) == 0 {
		return nil
	}
	if len(clicks) > 25 {
		return fmt.Errorf("batch size %d exceeds DynamoDB limit of 25", len(clicks))
	}

	// Build write requests
	writeRequests := make([]types.WriteRequest, 0, len(clicks))
	for _, click := range clicks {
		item, err := attributevalue.MarshalMap(click)
		if err != nil {
			return fmt.Errorf("marshal click: %w", err)
		}
		writeRequests = append(writeRequests, types.WriteRequest{
			PutRequest: &types.PutRequest{Item: item},
		})
	}

	input := &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{
			s.tableName: writeRequests,
		},
	}

	const maxRetries = 5

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := s.client.BatchWriteItem(ctx, input)
		if err != nil {
			return fmt.Errorf("batch write clicks (attempt %d): %w", attempt, err)
		}

		// Check for unprocessed items
		unprocessed, ok := result.UnprocessedItems[s.tableName]
		if !ok || len(unprocessed) == 0 {
			return nil // all items written successfully
		}

		// Retry unprocessed items with exponential backoff + jitter
		if attempt == maxRetries {
			return fmt.Errorf("batch write: %d items still unprocessed after %d retries",
				len(unprocessed), maxRetries)
		}

		// Exponential backoff: 50ms, 100ms, 200ms, 400ms, 800ms + jitter
		backoff := time.Duration(math.Pow(2, float64(attempt))) * 50 * time.Millisecond
		jitter := time.Duration(rand.Int63n(int64(backoff) / 2))
		timer := time.NewTimer(backoff + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		// Retry only the unprocessed items
		input = &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				s.tableName: unprocessed,
			},
		}
	}

	return nil
}
```

---

## Pattern 5: TransactWriteItems -- Link Creation + Quota Decrement

Atomic multi-item transaction across `links` and `users` tables. Ensures a link is only created if the user has remaining quota.

```go
// ErrQuotaExceeded indicates the user has reached their daily or total link quota.
var ErrQuotaExceeded = errors.New("link quota exceeded")

// CreateLinkWithQuota atomically creates a link and increments the user's quota counters.
// Uses TransactWriteItems for cross-table atomicity.
//
// Transaction:
//   1. PutItem on links table (collision check)
//   2. UpdateItem on users table (increment daily + total counters, enforce quotas)
//
// Cost: 2 x 2 = 4 WCU (transactions cost 2x standard writes)
func (s *LinkStore) CreateLinkWithQuota(ctx context.Context, link *Link, userPK string, today string) error {
	linkItem, err := attributevalue.MarshalMap(link)
	if err != nil {
		return fmt.Errorf("marshal link: %w", err)
	}

	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				// Item 1: Create the link (collision-safe)
				Put: &types.Put{
					TableName:           aws.String(s.linksTableName),
					Item:                linkItem,
					ConditionExpression: aws.String("attribute_not_exists(PK)"),
				},
			},
			{
				// Item 2: Increment user quota counters
				Update: &types.Update{
					TableName: aws.String(s.usersTableName),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: userPK},
						"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
					},
					UpdateExpression: aws.String(
						"SET links_created_today = links_created_today + :one, " +
							"total_active_links = total_active_links + :one",
					),
					ConditionExpression: aws.String(
						"links_created_today < daily_link_quota " +
							"AND total_active_links < total_link_quota " +
							"AND last_reset_date = :today",
					),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":one":   &types.AttributeValueMemberN{Value: "1"},
						":today": &types.AttributeValueMemberS{Value: today},
					},
				},
			},
		},
	})
	if err != nil {
		var txErr *types.TransactionCanceledException
		if errors.As(err, &txErr) {
			// Inspect which condition failed
			for i, reason := range txErr.CancellationReasons {
				if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
					switch i {
					case 0:
						return ErrCodeCollision
					case 1:
						return ErrQuotaExceeded
					}
				}
			}
			return fmt.Errorf("transaction cancelled: %w", err)
		}
		return fmt.Errorf("create link with quota: %w", err)
	}

	return nil
}
```

---

## Pattern 6: Daily Quota Reset -- Lazy Evaluation

DynamoDB has no scheduled counter reset. The daily counter is reset lazily on the first request of each new day.

```go
// ResetDailyQuotaIfNeeded resets the daily link counter if the date has changed.
// This is a lazy evaluation pattern -- no cron job needed.
//
// DynamoDB expressions:
//   UpdateExpression: SET links_created_today = :zero, last_reset_date = :today
//   ConditionExpression: last_reset_date <> :today
//
// Cost: 1 WCU. No-op if already reset today (condition fails).
//
// Flow:
//   1. CreateLinkWithQuota fails with ConditionCheckFailed on last_reset_date = :today
//   2. Call ResetDailyQuotaIfNeeded
//   3. Retry CreateLinkWithQuota
func (s *UserStore) ResetDailyQuotaIfNeeded(ctx context.Context, userPK string, today string) (bool, error) {
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: userPK},
			"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
		},
		UpdateExpression:    aws.String("SET links_created_today = :zero, last_reset_date = :today"),
		ConditionExpression: aws.String("last_reset_date <> :today"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":zero":  &types.AttributeValueMemberN{Value: "0"},
			":today": &types.AttributeValueMemberS{Value: today},
		},
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return false, nil // already reset today -- no action needed
		}
		return false, fmt.Errorf("reset daily quota: %w", err)
	}

	return true, nil // counter was reset
}

// CreateLinkFlow is the complete link creation flow with lazy quota reset.
func (s *LinkService) CreateLinkFlow(ctx context.Context, link *Link, userPK string) error {
	today := time.Now().UTC().Format("2006-01-02")

	err := s.linkStore.CreateLinkWithQuota(ctx, link, userPK, today)
	if err == nil {
		return nil
	}

	// If quota check failed, it might be because last_reset_date != today.
	// Try a lazy reset, then retry.
	if errors.Is(err, ErrQuotaExceeded) {
		reset, resetErr := s.userStore.ResetDailyQuotaIfNeeded(ctx, userPK, today)
		if resetErr != nil {
			return resetErr
		}
		if reset {
			// Counter was reset -- retry the creation
			return s.linkStore.CreateLinkWithQuota(ctx, link, userPK, today)
		}
		// Counter was already reset today -- the user genuinely exceeded their quota
		return ErrQuotaExceeded
	}

	return err
}
```

---

## Pattern 7: Idempotent Deactivation

Soft-delete that succeeds even if the link is already deactivated.

```go
// ErrLinkNotFound indicates the link does not exist.
var ErrLinkNotFound = errors.New("link not found")

// DeactivateLink sets is_active=false. Idempotent -- calling on an already-deactivated
// link is a no-op (updated_at still changes).
//
// DynamoDB expressions:
//   UpdateExpression: SET is_active = :false_val, updated_at = :now
//   ConditionExpression: attribute_exists(PK) AND owner_id = :caller_id
//
// Cost: 1 WCU
func (s *LinkStore) DeactivateLink(ctx context.Context, code string, callerID string) error {
	now := time.Now().Unix()

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
			"SK": &types.AttributeValueMemberS{Value: "META"},
		},
		UpdateExpression: aws.String("SET is_active = :false_val, updated_at = :now"),
		ConditionExpression: aws.String(
			"attribute_exists(PK) AND owner_id = :caller_id",
		),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":false_val": &types.AttributeValueMemberBOOL{Value: false},
			":now":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
			":caller_id": &types.AttributeValueMemberS{Value: callerID},
		},
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			// Either link doesn't exist or caller is not the owner
			return ErrLinkNotFound
		}
		return fmt.Errorf("deactivate link %s: %w", code, err)
	}

	return nil
}
```

---

## Pattern 8: Optimistic Locking with Version Number

For update operations where concurrent writes from the dashboard could conflict (e.g., two tabs editing the same link). Uses a `version` attribute as a monotonically increasing counter.

```go
// ErrConcurrentModification indicates another request modified the item since it was read.
var ErrConcurrentModification = errors.New("concurrent modification detected, please retry")

// UpdateLinkOptimistic updates a link with optimistic locking.
//
// DynamoDB expressions:
//   UpdateExpression: SET original_url = :url, title = :title, updated_at = :now,
//                         version = :new_version
//   ConditionExpression: attribute_exists(PK) AND owner_id = :caller_id
//                        AND version = :current_version
//
// Cost: 1 WCU
func (s *LinkStore) UpdateLinkOptimistic(
	ctx context.Context,
	code string,
	callerID string,
	currentVersion int64,
	updates map[string]types.AttributeValue,
) error {
	now := time.Now().Unix()
	newVersion := currentVersion + 1

	// Build SET expression from updates map
	setExpr := "SET updated_at = :now, version = :new_version"
	exprValues := map[string]types.AttributeValue{
		":now":             &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
		":new_version":     &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", newVersion)},
		":current_version": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", currentVersion)},
		":caller_id":       &types.AttributeValueMemberS{Value: callerID},
	}

	for key, val := range updates {
		placeholder := ":" + key
		setExpr += fmt.Sprintf(", %s = %s", key, placeholder)
		exprValues[placeholder] = val
	}

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
			"SK": &types.AttributeValueMemberS{Value: "META"},
		},
		UpdateExpression: aws.String(setExpr),
		ConditionExpression: aws.String(
			"attribute_exists(PK) AND owner_id = :caller_id AND version = :current_version",
		),
		ExpressionAttributeValues: exprValues,
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return ErrConcurrentModification
		}
		return fmt.Errorf("update link %s: %w", code, err)
	}

	return nil
}
```

---

## Pattern 9: Delete Link + Decrement User Counter (TransactWriteItems)

Atomic cross-table operation that deactivates a link and decrements the user's active link count.

```go
// DeleteLinkWithQuotaUpdate atomically deactivates a link and decrements
// the user's total_active_links counter.
//
// Cost: 2 x 2 = 4 WCU (transaction pricing)
func (s *LinkStore) DeleteLinkWithQuotaUpdate(
	ctx context.Context,
	code string,
	callerID string,
	userPK string,
) error {
	now := time.Now().Unix()

	_, err := s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Update: &types.Update{
					TableName: aws.String(s.linksTableName),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
						"SK": &types.AttributeValueMemberS{Value: "META"},
					},
					UpdateExpression: aws.String("SET is_active = :false_val, updated_at = :now"),
					ConditionExpression: aws.String(
						"attribute_exists(PK) AND owner_id = :caller_id AND is_active = :true_val",
					),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":false_val": &types.AttributeValueMemberBOOL{Value: false},
						":true_val":  &types.AttributeValueMemberBOOL{Value: true},
						":now":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
						":caller_id": &types.AttributeValueMemberS{Value: callerID},
					},
				},
			},
			{
				Update: &types.Update{
					TableName: aws.String(s.usersTableName),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: userPK},
						"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
					},
					UpdateExpression: aws.String(
						"SET total_active_links = total_active_links - :one",
					),
					ConditionExpression: aws.String(
						"attribute_exists(PK) AND total_active_links > :zero",
					),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":one":  &types.AttributeValueMemberN{Value: "1"},
						":zero": &types.AttributeValueMemberN{Value: "0"},
					},
				},
			},
		},
	})
	if err != nil {
		var txErr *types.TransactionCanceledException
		if errors.As(err, &txErr) {
			for i, reason := range txErr.CancellationReasons {
				if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
					if i == 0 {
						return ErrLinkNotFound // link doesn't exist or not owned by caller
					}
				}
			}
			return fmt.Errorf("transaction cancelled: %w", err)
		}
		return fmt.Errorf("delete link with quota update: %w", err)
	}

	return nil
}
```

---

## Error Handling Summary

| DynamoDB Error | Meaning | Application Response |
|----------------|---------|---------------------|
| `ConditionalCheckFailedException` (PutItem) | Short code already exists | Retry with new code |
| `ConditionalCheckFailedException` (click increment) | Link inactive or click limit reached | HTTP 410 Gone |
| `ConditionalCheckFailedException` (quota check) | Daily or total quota exceeded | HTTP 429 Too Many Requests |
| `ConditionalCheckFailedException` (deactivate) | Link not found or not owned by caller | HTTP 404 Not Found |
| `ConditionalCheckFailedException` (optimistic lock) | Concurrent modification | HTTP 409 Conflict |
| `TransactionCanceledException` | One or more conditions in transaction failed | Inspect `CancellationReasons` per item |
| `ProvisionedThroughputExceededException` | Table/GSI throttled | Retry with exponential backoff (SDK auto-retries) |
| `ResourceNotFoundException` | Table does not exist | Fatal -- configuration error |

---

## SDK Configuration Notes

### Retry Configuration

The AWS SDK v2 has built-in retry with exponential backoff for throttling errors. Configure it in the Lambda init phase:

```go
import (
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
)

cfg, err := config.LoadDefaultConfig(ctx,
	config.WithRetryer(func() aws.Retryer {
		return retry.AddWithMaxAttempts(retry.NewStandard(), 5)
	}),
)
```

### Client Initialization (Lambda warm start)

DynamoDB client is initialized **outside** the handler function to survive across warm Lambda invocations:

```go
var (
	ddbClient *dynamodb.Client
	linkStore *store.LinkStore
)

func init() {
	cfg, _ := config.LoadDefaultConfig(context.Background())
	ddbClient = dynamodb.NewFromConfig(cfg)
	linkStore = store.NewLinkStore(ddbClient, os.Getenv("LINKS_TABLE"))
}

func handler(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	// linkStore is already initialized from a previous invocation
	// ...
}
```
