package store

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// --- Mock DynamoDB Client ---

type mockDynamoDBClient struct {
	putItemFn        func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	getItemFn        func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	updateItemFn     func(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	deleteItemFn     func(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	queryFn          func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	batchWriteItemFn func(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

func (m *mockDynamoDBClient) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, params, optFns...)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDynamoDBClient) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.getItemFn != nil {
		return m.getItemFn(ctx, params, optFns...)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *mockDynamoDBClient) UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if m.updateItemFn != nil {
		return m.updateItemFn(ctx, params, optFns...)
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

func (m *mockDynamoDBClient) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, params, optFns...)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockDynamoDBClient) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, params, optFns...)
	}
	return &dynamodb.QueryOutput{}, nil
}

func (m *mockDynamoDBClient) BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	if m.batchWriteItemFn != nil {
		return m.batchWriteItemFn(ctx, params, optFns...)
	}
	return &dynamodb.BatchWriteItemOutput{}, nil
}

// --- Tests ---

func TestCreateLink_Success(t *testing.T) {
	mock := &mockDynamoDBClient{
		putItemFn: func(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
			if aws.ToString(params.TableName) != "links" {
				t.Errorf("expected table 'links', got %q", aws.ToString(params.TableName))
			}
			if aws.ToString(params.ConditionExpression) != "attribute_not_exists(PK)" {
				t.Errorf("expected condition expression 'attribute_not_exists(PK)', got %q", aws.ToString(params.ConditionExpression))
			}
			return &dynamodb.PutItemOutput{}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	link := &Link{
		Code:        "abc1234",
		OriginalURL: "https://example.com",
		OwnerID:     "USER#test",
		IsActive:    true,
		CreatedAt:   1000000,
		UpdatedAt:   1000000,
	}

	err := s.CreateLink(context.Background(), link)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if link.PK != "LINK#abc1234" {
		t.Errorf("expected PK 'LINK#abc1234', got %q", link.PK)
	}
	if link.SK != "META" {
		t.Errorf("expected SK 'META', got %q", link.SK)
	}
}

func TestCreateLink_Collision(t *testing.T) {
	mock := &mockDynamoDBClient{
		putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
			return nil, &types.ConditionalCheckFailedException{
				Message: aws.String("conditional check failed"),
			}
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	link := &Link{Code: "abc1234", OriginalURL: "https://example.com"}

	err := s.CreateLink(context.Background(), link)
	if !errors.Is(err, ErrCodeCollision) {
		t.Fatalf("expected ErrCodeCollision, got %v", err)
	}
}

func TestGetLink_Success(t *testing.T) {
	link := &Link{
		PK:          "LINK#abc1234",
		SK:          "META",
		Code:        "abc1234",
		OriginalURL: "https://example.com",
		IsActive:    true,
	}
	item, _ := attributevalue.MarshalMap(link)

	mock := &mockDynamoDBClient{
		getItemFn: func(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			pk := params.Key["PK"].(*types.AttributeValueMemberS).Value
			if pk != "LINK#abc1234" {
				t.Errorf("expected PK 'LINK#abc1234', got %q", pk)
			}
			return &dynamodb.GetItemOutput{Item: item}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	result, err := s.GetLink(context.Background(), "abc1234")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Code != "abc1234" {
		t.Errorf("expected code 'abc1234', got %q", result.Code)
	}
	if result.OriginalURL != "https://example.com" {
		t.Errorf("expected URL 'https://example.com', got %q", result.OriginalURL)
	}
}

func TestGetLink_NotFound(t *testing.T) {
	mock := &mockDynamoDBClient{
		getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: nil}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	_, err := s.GetLink(context.Background(), "nonexistent")
	if !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("expected ErrLinkNotFound, got %v", err)
	}
}

func TestDeleteLink_Success(t *testing.T) {
	mock := &mockDynamoDBClient{
		updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			pk := params.Key["PK"].(*types.AttributeValueMemberS).Value
			if pk != "LINK#abc1234" {
				t.Errorf("expected PK 'LINK#abc1234', got %q", pk)
			}
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	err := s.DeleteLink(context.Background(), "abc1234", "USER#owner1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDeleteLink_NotFound(t *testing.T) {
	mock := &mockDynamoDBClient{
		updateItemFn: func(_ context.Context, _ *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			return nil, &types.ConditionalCheckFailedException{
				Message: aws.String("conditional check failed"),
			}
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	err := s.DeleteLink(context.Background(), "abc1234", "USER#wrong")
	if !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("expected ErrLinkNotFound, got %v", err)
	}
}

func TestIncrementClickCount_Success(t *testing.T) {
	mock := &mockDynamoDBClient{
		updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			condExpr := aws.ToString(params.ConditionExpression)
			if condExpr != "is_active = :true_val AND (attribute_not_exists(max_clicks) OR click_count < max_clicks)" {
				t.Errorf("unexpected condition expression: %q", condExpr)
			}
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	ok, err := s.IncrementClickCount(context.Background(), "abc1234", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
}

func TestIncrementClickCount_LimitReached(t *testing.T) {
	mock := &mockDynamoDBClient{
		updateItemFn: func(_ context.Context, _ *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			return nil, &types.ConditionalCheckFailedException{
				Message: aws.String("conditional check failed"),
			}
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	maxClicks := int64(100)
	ok, err := s.IncrementClickCount(context.Background(), "abc1234", &maxClicks)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ok {
		t.Error("expected ok=false when limit reached")
	}
}

func TestBatchWriteClicks_Success(t *testing.T) {
	mock := &mockDynamoDBClient{
		batchWriteItemFn: func(_ context.Context, params *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
			reqs := params.RequestItems["clicks"]
			if len(reqs) != 2 {
				t.Errorf("expected 2 write requests, got %d", len(reqs))
			}
			return &dynamodb.BatchWriteItemOutput{}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	events := []*ClickEvent{
		{PK: "LINK#abc", SK: "CLICK#123#uuid1", IPHash: "hash1", Country: "US", DeviceType: "desktop", UserAgentHash: "ua1", CreatedAt: 123, TTL: 456},
		{PK: "LINK#abc", SK: "CLICK#124#uuid2", IPHash: "hash2", Country: "DE", DeviceType: "mobile", UserAgentHash: "ua2", CreatedAt: 124, TTL: 457},
	}

	err := s.BatchWriteClicks(context.Background(), events)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestBatchWriteClicks_Empty(t *testing.T) {
	s := NewDynamoStore(&mockDynamoDBClient{}, "links", "clicks", "users")
	err := s.BatchWriteClicks(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error for empty batch, got %v", err)
	}
}

func TestBatchWriteClicks_ExceedsLimit(t *testing.T) {
	s := NewDynamoStore(&mockDynamoDBClient{}, "links", "clicks", "users")
	events := make([]*ClickEvent, 26)
	for i := range events {
		events[i] = &ClickEvent{PK: "LINK#x", SK: "CLICK#1#uuid"}
	}
	err := s.BatchWriteClicks(context.Background(), events)
	if err == nil {
		t.Fatal("expected error for batch > 25, got nil")
	}
}

func TestListLinksByOwner_Success(t *testing.T) {
	link := &Link{
		PK:          "LINK#abc1234",
		SK:          "META",
		OwnerID:     "USER#owner1",
		Code:        "abc1234",
		OriginalURL: "https://example.com",
		IsActive:    true,
		CreatedAt:   1000000,
	}
	item, _ := attributevalue.MarshalMap(link)

	mock := &mockDynamoDBClient{
		queryFn: func(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
			if aws.ToString(params.IndexName) != "owner_id-created_at-index" {
				t.Errorf("expected GSI 'owner_id-created_at-index', got %q", aws.ToString(params.IndexName))
			}
			if aws.ToBool(params.ScanIndexForward) {
				t.Error("expected ScanIndexForward=false")
			}
			return &dynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{item},
			}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	links, cursor, err := s.ListLinksByOwner(context.Background(), "USER#owner1", "", 20)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Code != "abc1234" {
		t.Errorf("expected code 'abc1234', got %q", links[0].Code)
	}
	if cursor != "" {
		t.Errorf("expected empty cursor, got %q", cursor)
	}
}

func TestGetUser_Success(t *testing.T) {
	user := &User{
		PK:    "USER#sub123",
		SK:    "PROFILE",
		Email: "test@example.com",
		Plan:  "free",
	}
	item, _ := attributevalue.MarshalMap(user)

	mock := &mockDynamoDBClient{
		getItemFn: func(_ context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			pk := params.Key["PK"].(*types.AttributeValueMemberS).Value
			if pk != "USER#sub123" {
				t.Errorf("expected PK 'USER#sub123', got %q", pk)
			}
			return &dynamodb.GetItemOutput{Item: item}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	result, err := s.GetUser(context.Background(), "sub123")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %q", result.Email)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	mock := &mockDynamoDBClient{
		getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: nil}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	_, err := s.GetUser(context.Background(), "nonexistent")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUpdateLink_Success(t *testing.T) {
	mock := &mockDynamoDBClient{
		updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
			condExpr := aws.ToString(params.ConditionExpression)
			if condExpr != "attribute_exists(PK) AND owner_id = :caller_id" {
				t.Errorf("unexpected condition: %q", condExpr)
			}
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}

	s := NewDynamoStore(mock, "links", "clicks", "users")
	err := s.UpdateLink(context.Background(), "abc1234", "USER#owner1", map[string]interface{}{
		"title": "Updated",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestUpdateLink_EmptyUpdates(t *testing.T) {
	s := NewDynamoStore(&mockDynamoDBClient{}, "links", "clicks", "users")
	err := s.UpdateLink(context.Background(), "abc1234", "USER#owner1", map[string]interface{}{})
	if err != nil {
		t.Fatalf("expected no error for empty updates, got %v", err)
	}
}
