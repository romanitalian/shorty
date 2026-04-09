package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Store defines the interface for all DynamoDB operations across the
// links, clicks, and users tables.
type Store interface {
	// Links
	CreateLink(ctx context.Context, link *Link) error
	GetLink(ctx context.Context, code string) (*Link, error)
	UpdateLink(ctx context.Context, code string, callerID string, updates map[string]interface{}) error
	DeleteLink(ctx context.Context, code string, callerID string) error
	ListLinksByOwner(ctx context.Context, ownerID string, cursor string, limit int) ([]*Link, string, error)
	IncrementClickCount(ctx context.Context, code string, maxClicks *int64) (bool, error)

	// Clicks
	BatchWriteClicks(ctx context.Context, events []*ClickEvent) error

	// Stats
	GetLinkStats(ctx context.Context, code string) (*LinkStats, error)
	GetLinkTimeline(ctx context.Context, code string, from, to time.Time, granularity string) ([]TimelineBucket, error)
	GetLinkGeo(ctx context.Context, code string) ([]GeoStat, error)
	GetLinkReferrers(ctx context.Context, code string) ([]ReferrerStat, error)

	// Users
	GetUser(ctx context.Context, userID string) (*User, error)
	UpdateUserQuota(ctx context.Context, userID string) error
}

// DynamoDBClient is the subset of the DynamoDB client used by the store.
// Defined here for testability.
type DynamoDBClient interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

// DynamoStore implements Store against DynamoDB.
type DynamoStore struct {
	client      DynamoDBClient
	linksTable  string
	clicksTable string
	usersTable  string
}

// NewDynamoStore creates a new DynamoDB-backed Store.
func NewDynamoStore(client DynamoDBClient, linksTable, clicksTable, usersTable string) *DynamoStore {
	return &DynamoStore{
		client:      client,
		linksTable:  linksTable,
		clicksTable: clicksTable,
		usersTable:  usersTable,
	}
}

// ---------- Links ----------

// CreateLink inserts a new link with collision-safe conditional write.
// Uses ConditionExpression: attribute_not_exists(PK) to prevent code collisions.
// Cost: 1 WCU (item < 1 KB).
func (s *DynamoStore) CreateLink(ctx context.Context, link *Link) error {
	link.PK = "LINK#" + link.Code
	link.SK = "META"

	item, err := attributevalue.MarshalMap(link)
	if err != nil {
		return fmt.Errorf("store.CreateLink: marshal: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.linksTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(PK)"),
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return ErrCodeCollision
		}
		return fmt.Errorf("store.CreateLink: %w", err)
	}

	return nil
}

// GetLink retrieves a link by its short code.
// Key: PK = "LINK#{code}", SK = "META". Uses eventually consistent reads (0.5 RCU).
func (s *DynamoStore) GetLink(ctx context.Context, code string) (*Link, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.linksTable),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
			"SK": &types.AttributeValueMemberS{Value: "META"},
		},
		ConsistentRead: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("store.GetLink: %w", err)
	}

	if result.Item == nil {
		return nil, ErrLinkNotFound
	}

	var link Link
	if err := attributevalue.UnmarshalMap(result.Item, &link); err != nil {
		return nil, fmt.Errorf("store.GetLink: unmarshal: %w", err)
	}

	return &link, nil
}

// UpdateLink applies partial updates to a link.
// ConditionExpression: attribute_exists(PK) AND owner_id = :caller_id.
// Cost: 1 WCU.
func (s *DynamoStore) UpdateLink(ctx context.Context, code string, callerID string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	now := time.Now().Unix()
	setExpr := "SET updated_at = :now"
	exprValues := map[string]types.AttributeValue{
		":now":       &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
		":caller_id": &types.AttributeValueMemberS{Value: callerID},
	}
	exprNames := map[string]string{}

	i := 0
	for key, val := range updates {
		placeholder := fmt.Sprintf(":val%d", i)
		nameAlias := fmt.Sprintf("#attr%d", i)

		av, err := attributevalue.Marshal(val)
		if err != nil {
			return fmt.Errorf("store.UpdateLink: marshal field %s: %w", key, err)
		}

		setExpr += fmt.Sprintf(", %s = %s", nameAlias, placeholder)
		exprValues[placeholder] = av
		exprNames[nameAlias] = key
		i++
	}

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.linksTable),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "LINK#" + code},
			"SK": &types.AttributeValueMemberS{Value: "META"},
		},
		UpdateExpression:          aws.String(setExpr),
		ConditionExpression:       aws.String("attribute_exists(PK) AND owner_id = :caller_id"),
		ExpressionAttributeValues: exprValues,
		ExpressionAttributeNames:  exprNames,
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return ErrLinkNotFound
		}
		return fmt.Errorf("store.UpdateLink: %w", err)
	}

	return nil
}

// DeleteLink performs a soft-delete by setting is_active = false.
// ConditionExpression: attribute_exists(PK) AND owner_id = :caller_id.
// Idempotent -- calling on an already-deactivated link succeeds.
// Cost: 1 WCU.
func (s *DynamoStore) DeleteLink(ctx context.Context, code string, callerID string) error {
	now := time.Now().Unix()

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.linksTable),
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
			return ErrLinkNotFound
		}
		return fmt.Errorf("store.DeleteLink: %w", err)
	}

	return nil
}

// paginationToken is the internal representation of a cursor for ListLinksByOwner.
type paginationToken struct {
	OwnerID   string `json:"owner_id"`
	CreatedAt int64  `json:"created_at"`
	PK        string `json:"pk"`
	SK        string `json:"sk"`
}

// ListLinksByOwner queries the owner_id-created_at-index GSI with cursor-based pagination.
// Returns links, a next cursor (empty string if no more), and error.
// ScanIndexForward = false for newest-first ordering.
func (s *DynamoStore) ListLinksByOwner(ctx context.Context, ownerID string, cursor string, limit int) ([]*Link, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.linksTable),
		IndexName:              aws.String("owner_id-created_at-index"),
		KeyConditionExpression: aws.String("owner_id = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: ownerID},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(int32(limit)),
	}

	// Decode cursor into ExclusiveStartKey
	if cursor != "" {
		decoded, err := base64.URLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("store.ListLinksByOwner: invalid cursor: %w", err)
		}
		var token paginationToken
		if err := json.Unmarshal(decoded, &token); err != nil {
			return nil, "", fmt.Errorf("store.ListLinksByOwner: invalid cursor payload: %w", err)
		}
		input.ExclusiveStartKey = map[string]types.AttributeValue{
			"owner_id":   &types.AttributeValueMemberS{Value: token.OwnerID},
			"created_at": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", token.CreatedAt)},
			"PK":         &types.AttributeValueMemberS{Value: token.PK},
			"SK":         &types.AttributeValueMemberS{Value: token.SK},
		}
	}

	result, err := s.client.Query(ctx, input)
	if err != nil {
		return nil, "", fmt.Errorf("store.ListLinksByOwner: %w", err)
	}

	links := make([]*Link, 0, len(result.Items))
	for _, item := range result.Items {
		var link Link
		if err := attributevalue.UnmarshalMap(item, &link); err != nil {
			return nil, "", fmt.Errorf("store.ListLinksByOwner: unmarshal: %w", err)
		}
		links = append(links, &link)
	}

	// Build next cursor from LastEvaluatedKey
	var nextCursor string
	if result.LastEvaluatedKey != nil {
		var ownerVal, pkVal, skVal string
		var createdAtVal int64

		if v, ok := result.LastEvaluatedKey["owner_id"].(*types.AttributeValueMemberS); ok {
			ownerVal = v.Value
		}
		if v, ok := result.LastEvaluatedKey["PK"].(*types.AttributeValueMemberS); ok {
			pkVal = v.Value
		}
		if v, ok := result.LastEvaluatedKey["SK"].(*types.AttributeValueMemberS); ok {
			skVal = v.Value
		}
		if v, ok := result.LastEvaluatedKey["created_at"].(*types.AttributeValueMemberN); ok {
			fmt.Sscanf(v.Value, "%d", &createdAtVal)
		}

		token := paginationToken{
			OwnerID:   ownerVal,
			CreatedAt: createdAtVal,
			PK:        pkVal,
			SK:        skVal,
		}
		tokenJSON, err := json.Marshal(token)
		if err != nil {
			return nil, "", fmt.Errorf("store.ListLinksByOwner: encode cursor: %w", err)
		}
		nextCursor = base64.URLEncoding.EncodeToString(tokenJSON)
	}

	return links, nextCursor, nil
}

// IncrementClickCount atomically increments click_count with max_clicks enforcement.
// Returns (true, nil) if the increment succeeded, (false, nil) if the link is inactive
// or the click limit has been reached.
//
// DynamoDB expressions:
//
//	UpdateExpression: SET click_count = click_count + :one, updated_at = :now
//	ConditionExpression: is_active = :true_val
//	                     AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)
//
// Cost: 1 WCU.
func (s *DynamoStore) IncrementClickCount(ctx context.Context, code string, maxClicks *int64) (bool, error) {
	now := time.Now().Unix()

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.linksTable),
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
			return false, nil
		}
		return false, fmt.Errorf("store.IncrementClickCount: %w", err)
	}

	return true, nil
}

// ---------- Clicks ----------

// BatchWriteClicks writes up to 25 click events to the clicks table.
// Retries UnprocessedItems with exponential backoff + jitter.
// Cost: 1 WCU per item. Max 25 WCU per batch.
func (s *DynamoStore) BatchWriteClicks(ctx context.Context, events []*ClickEvent) error {
	if len(events) == 0 {
		return nil
	}
	if len(events) > 25 {
		return fmt.Errorf("store.BatchWriteClicks: batch size %d exceeds DynamoDB limit of 25", len(events))
	}

	writeRequests := make([]types.WriteRequest, 0, len(events))
	for _, event := range events {
		item, err := attributevalue.MarshalMap(event)
		if err != nil {
			return fmt.Errorf("store.BatchWriteClicks: marshal: %w", err)
		}
		writeRequests = append(writeRequests, types.WriteRequest{
			PutRequest: &types.PutRequest{Item: item},
		})
	}

	input := &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{
			s.clicksTable: writeRequests,
		},
	}

	const maxRetries = 5

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := s.client.BatchWriteItem(ctx, input)
		if err != nil {
			return fmt.Errorf("store.BatchWriteClicks (attempt %d): %w", attempt, err)
		}

		unprocessed, ok := result.UnprocessedItems[s.clicksTable]
		if !ok || len(unprocessed) == 0 {
			return nil
		}

		if attempt == maxRetries {
			return fmt.Errorf("store.BatchWriteClicks: %d items still unprocessed after %d retries",
				len(unprocessed), maxRetries)
		}

		// Exponential backoff: 50ms, 100ms, 200ms, 400ms, 800ms + jitter
		backoff := time.Duration(math.Pow(2, float64(attempt))) * 50 * time.Millisecond
		jitter := time.Duration(rand.Int63n(int64(backoff) / 2)) //nolint:gosec // jitter does not need crypto rand
		timer := time.NewTimer(backoff + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		input = &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				s.clicksTable: unprocessed,
			},
		}
	}

	return nil
}

// ---------- Stats ----------

// queryAllClicks fetches all click events for a given link code from the clicks table.
// PK = "LINK#{code}", SK begins with "CLICK#".
func (s *DynamoStore) queryAllClicks(ctx context.Context, code string) ([]*ClickEvent, error) {
	var allEvents []*ClickEvent
	var lastKey map[string]types.AttributeValue

	for {
		input := &dynamodb.QueryInput{
			TableName:              aws.String(s.clicksTable),
			KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk":     &types.AttributeValueMemberS{Value: "LINK#" + code},
				":prefix": &types.AttributeValueMemberS{Value: "CLICK#"},
			},
		}
		if lastKey != nil {
			input.ExclusiveStartKey = lastKey
		}

		result, err := s.client.Query(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("store.queryAllClicks: %w", err)
		}

		for _, item := range result.Items {
			var event ClickEvent
			if err := attributevalue.UnmarshalMap(item, &event); err != nil {
				return nil, fmt.Errorf("store.queryAllClicks: unmarshal: %w", err)
			}
			allEvents = append(allEvents, &event)
		}

		if result.LastEvaluatedKey == nil {
			break
		}
		lastKey = result.LastEvaluatedKey
	}

	return allEvents, nil
}

// GetLinkStats returns aggregate stats for a link by querying the clicks table.
func (s *DynamoStore) GetLinkStats(ctx context.Context, code string) (*LinkStats, error) {
	events, err := s.queryAllClicks(ctx, code)
	if err != nil {
		return nil, err
	}

	stats := &LinkStats{
		TotalClicks: int64(len(events)),
	}

	uniqueIPs := make(map[string]struct{})
	var maxTS int64
	for _, e := range events {
		uniqueIPs[e.IPHash] = struct{}{}
		if e.CreatedAt > maxTS {
			maxTS = e.CreatedAt
		}
	}
	stats.UniqueClicks = int64(len(uniqueIPs))
	if len(events) > 0 {
		stats.LastClickAt = &maxTS
	}

	return stats, nil
}

// GetLinkTimeline returns click counts grouped by time period (hour/day/week).
func (s *DynamoStore) GetLinkTimeline(ctx context.Context, code string, from, to time.Time, granularity string) ([]TimelineBucket, error) {
	events, err := s.queryAllClicks(ctx, code)
	if err != nil {
		return nil, err
	}

	fromUnix := from.Unix()
	toUnix := to.Unix()

	buckets := make(map[int64]int64)
	for _, e := range events {
		if e.CreatedAt < fromUnix || e.CreatedAt > toUnix {
			continue
		}
		t := time.Unix(e.CreatedAt, 0).UTC()
		var key time.Time
		switch granularity {
		case "hour":
			key = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
		case "week":
			// Truncate to Monday of the week.
			weekday := int(t.Weekday())
			if weekday == 0 {
				weekday = 7
			}
			key = time.Date(t.Year(), t.Month(), t.Day()-(weekday-1), 0, 0, 0, 0, time.UTC)
		default: // "day"
			key = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
		buckets[key.Unix()]++
	}

	result := make([]TimelineBucket, 0, len(buckets))
	for ts, clicks := range buckets {
		result = append(result, TimelineBucket{Timestamp: ts, Clicks: clicks})
	}
	// Sort by timestamp ascending.
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Timestamp < result[i].Timestamp {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// GetLinkGeo returns click counts grouped by country.
func (s *DynamoStore) GetLinkGeo(ctx context.Context, code string) ([]GeoStat, error) {
	events, err := s.queryAllClicks(ctx, code)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int64)
	for _, e := range events {
		country := e.Country
		if country == "" {
			country = "unknown"
		}
		counts[country]++
	}

	result := make([]GeoStat, 0, len(counts))
	for country, clicks := range counts {
		result = append(result, GeoStat{Country: country, Clicks: clicks})
	}
	// Sort by clicks descending.
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Clicks > result[i].Clicks {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// GetLinkReferrers returns click counts grouped by referrer domain.
func (s *DynamoStore) GetLinkReferrers(ctx context.Context, code string) ([]ReferrerStat, error) {
	events, err := s.queryAllClicks(ctx, code)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int64)
	for _, e := range events {
		domain := e.RefererDomain
		if domain == "" {
			domain = "direct"
		}
		counts[domain]++
	}

	result := make([]ReferrerStat, 0, len(counts))
	for domain, clicks := range counts {
		result = append(result, ReferrerStat{Domain: domain, Clicks: clicks})
	}
	// Sort by clicks descending.
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Clicks > result[i].Clicks {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// ---------- Users ----------

// GetUser retrieves a user profile by cognito sub.
// Key: PK = "USER#{userID}", SK = "PROFILE".
func (s *DynamoStore) GetUser(ctx context.Context, userID string) (*User, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.usersTable),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + userID},
			"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
		},
		ConsistentRead: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("store.GetUser: %w", err)
	}

	if result.Item == nil {
		return nil, ErrUserNotFound
	}

	var user User
	if err := attributevalue.UnmarshalMap(result.Item, &user); err != nil {
		return nil, fmt.Errorf("store.GetUser: unmarshal: %w", err)
	}

	return &user, nil
}

// UpdateUserQuota increments the user's daily link counter.
// ConditionExpression: links_created_today < daily_link_quota AND last_reset_date = :today.
// Cost: 1 WCU.
func (s *DynamoStore) UpdateUserQuota(ctx context.Context, userID string) error {
	today := time.Now().UTC().Format("2006-01-02")

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.usersTable),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + userID},
			"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
		},
		UpdateExpression: aws.String(
			"SET links_created_today = links_created_today + :one, total_active_links = total_active_links + :one",
		),
		ConditionExpression: aws.String(
			"links_created_today < daily_link_quota AND last_reset_date = :today",
		),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one":   &types.AttributeValueMemberN{Value: "1"},
			":today": &types.AttributeValueMemberS{Value: today},
		},
	})
	if err != nil {
		var condErr *types.ConditionalCheckFailedException
		if errors.As(err, &condErr) {
			return fmt.Errorf("store.UpdateUserQuota: quota exceeded or date mismatch: %w", ErrLinkInactiveOrLimitReached)
		}
		return fmt.Errorf("store.UpdateUserQuota: %w", err)
	}

	return nil
}
