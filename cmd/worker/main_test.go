package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/romanitalian/shorty/internal/geo"
	"github.com/romanitalian/shorty/internal/store"
)

// mockStore implements store.Store for testing. Only BatchWriteClicks is used by the worker.
type mockStore struct {
	batchWriteClicksFn func(ctx context.Context, events []*store.ClickEvent) error
	writtenEvents      []*store.ClickEvent
}

func (m *mockStore) CreateLink(ctx context.Context, link *store.Link) error { return nil }
func (m *mockStore) GetLink(ctx context.Context, code string) (*store.Link, error) {
	return nil, nil
}
func (m *mockStore) UpdateLink(ctx context.Context, code string, callerID string, updates map[string]interface{}) error {
	return nil
}
func (m *mockStore) DeleteLink(ctx context.Context, code string, callerID string) error { return nil }
func (m *mockStore) ListLinksByOwner(ctx context.Context, ownerID string, cursor string, limit int) ([]*store.Link, string, error) {
	return nil, "", nil
}
func (m *mockStore) IncrementClickCount(ctx context.Context, code string, maxClicks *int64) (bool, error) {
	return true, nil
}
func (m *mockStore) GetLinkStats(_ context.Context, _ string) (*store.LinkStats, error) {
	return &store.LinkStats{}, nil
}
func (m *mockStore) GetLinkTimeline(_ context.Context, _ string, _, _ time.Time, _ string) ([]store.TimelineBucket, error) {
	return nil, nil
}
func (m *mockStore) GetLinkGeo(_ context.Context, _ string) ([]store.GeoStat, error) {
	return nil, nil
}
func (m *mockStore) GetLinkReferrers(_ context.Context, _ string) ([]store.ReferrerStat, error) {
	return nil, nil
}
func (m *mockStore) GetUser(ctx context.Context, userID string) (*store.User, error) {
	return nil, nil
}
func (m *mockStore) UpdateUserQuota(ctx context.Context, userID string) error { return nil }

func (m *mockStore) BatchWriteClicks(ctx context.Context, evts []*store.ClickEvent) error {
	m.writtenEvents = append(m.writtenEvents, evts...)
	if m.batchWriteClicksFn != nil {
		return m.batchWriteClicksFn(ctx, evts)
	}
	return nil
}

func newTestWorker(ms *mockStore) *Worker {
	w := NewWorker(ms, geo.NewStubResolver(), clicksTTLDays*24*time.Hour)
	w.nowFunc = func() time.Time {
		return time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	}
	return w
}

func makeRecord(messageID string, body interface{}) events.SQSMessage {
	b, _ := json.Marshal(body)
	return events.SQSMessage{
		MessageId: messageID,
		Body:      string(b),
	}
}

func TestProcessClickEvent_ValidMessage(t *testing.T) {
	ms := &mockStore{}
	w := newTestWorker(ms)

	now := w.now()
	msg := sqsClickMessage{
		Code:          "abc123",
		IPHash:        "hashvalue",
		UserAgent:     "Mozilla/5.0 (iPhone; CPU iPhone OS)",
		RefererDomain: "example.com",
		Timestamp:     now.Add(-1 * time.Hour).Unix(),
	}

	event := events.SQSEvent{
		Records: []events.SQSMessage{makeRecord("msg-1", msg)},
	}

	resp, err := w.HandleSQSEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected 0 failures, got %d", len(resp.BatchItemFailures))
	}

	if len(ms.writtenEvents) != 1 {
		t.Fatalf("expected 1 written event, got %d", len(ms.writtenEvents))
	}

	ce := ms.writtenEvents[0]
	if ce.PK != "LINK#abc123" {
		t.Errorf("expected PK LINK#abc123, got %s", ce.PK)
	}
	if ce.Country != "XX" {
		t.Errorf("expected country XX, got %s", ce.Country)
	}
	if ce.DeviceType != "mobile" {
		t.Errorf("expected device type mobile (iPhone UA), got %s", ce.DeviceType)
	}
	if ce.RefererDomain != "example.com" {
		t.Errorf("expected referer_domain example.com, got %s", ce.RefererDomain)
	}
	// TTL should be ~90 days from "now".
	expectedTTL := now.Add(clicksTTLDays * 24 * time.Hour).Unix()
	if ce.TTL != expectedTTL {
		t.Errorf("expected TTL %d, got %d", expectedTTL, ce.TTL)
	}
}

func TestProcessClickEvent_InvalidJSON(t *testing.T) {
	ms := &mockStore{}
	w := newTestWorker(ms)

	event := events.SQSEvent{
		Records: []events.SQSMessage{
			{
				MessageId: "bad-msg",
				Body:      "not valid json{{{",
			},
		},
	}

	resp, err := w.HandleSQSEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.BatchItemFailures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(resp.BatchItemFailures))
	}
	if resp.BatchItemFailures[0].ItemIdentifier != "bad-msg" {
		t.Errorf("expected failure for bad-msg, got %s", resp.BatchItemFailures[0].ItemIdentifier)
	}
	if len(ms.writtenEvents) != 0 {
		t.Errorf("expected no written events, got %d", len(ms.writtenEvents))
	}
}

func TestProcessClickEvent_BatchWriteError(t *testing.T) {
	ms := &mockStore{
		batchWriteClicksFn: func(_ context.Context, _ []*store.ClickEvent) error {
			return errors.New("dynamodb unavailable")
		},
	}
	w := newTestWorker(ms)

	now := w.now()
	msg := sqsClickMessage{
		Code:      "xyz789",
		IPHash:    "hash2",
		UserAgent: "Mozilla/5.0",
		Timestamp: now.Add(-5 * time.Minute).Unix(),
	}

	event := events.SQSEvent{
		Records: []events.SQSMessage{
			makeRecord("msg-a", msg),
			makeRecord("msg-b", sqsClickMessage{
				Code:      "xyz789",
				IPHash:    "hash3",
				UserAgent: "Mozilla/5.0",
				Timestamp: now.Add(-3 * time.Minute).Unix(),
			}),
		},
	}

	resp, err := w.HandleSQSEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both messages should be reported as failures.
	if len(resp.BatchItemFailures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(resp.BatchItemFailures))
	}

	ids := map[string]bool{}
	for _, f := range resp.BatchItemFailures {
		ids[f.ItemIdentifier] = true
	}
	if !ids["msg-a"] || !ids["msg-b"] {
		t.Errorf("expected failures for msg-a and msg-b, got %v", resp.BatchItemFailures)
	}
}

func TestProcessClickEvent_ExpiredTTL(t *testing.T) {
	ms := &mockStore{}
	w := newTestWorker(ms)

	now := w.now()
	// Timestamp more than 90 days ago.
	msg := sqsClickMessage{
		Code:      "old123",
		IPHash:    "hash4",
		UserAgent: "Mozilla/5.0",
		Timestamp: now.Add(-91 * 24 * time.Hour).Unix(),
	}

	event := events.SQSEvent{
		Records: []events.SQSMessage{makeRecord("msg-old", msg)},
	}

	resp, err := w.HandleSQSEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expired events are silently skipped — not reported as failures.
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected 0 failures for expired event, got %d", len(resp.BatchItemFailures))
	}
	// And nothing should be written.
	if len(ms.writtenEvents) != 0 {
		t.Errorf("expected no written events for expired message, got %d", len(ms.writtenEvents))
	}
}

func TestProcessClickEvent_MixedBatch(t *testing.T) {
	ms := &mockStore{}
	w := newTestWorker(ms)

	now := w.now()

	event := events.SQSEvent{
		Records: []events.SQSMessage{
			makeRecord("good-1", sqsClickMessage{
				Code:      "abc",
				IPHash:    "h1",
				UserAgent: "Mozilla/5.0",
				Timestamp: now.Add(-1 * time.Minute).Unix(),
			}),
			{MessageId: "bad-1", Body: "broken json"},
			makeRecord("good-2", sqsClickMessage{
				Code:      "def",
				IPHash:    "h2",
				UserAgent: "Googlebot/2.1",
				Timestamp: now.Add(-2 * time.Minute).Unix(),
			}),
		},
	}

	resp, err := w.HandleSQSEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the bad JSON message should fail.
	if len(resp.BatchItemFailures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(resp.BatchItemFailures))
	}
	if resp.BatchItemFailures[0].ItemIdentifier != "bad-1" {
		t.Errorf("expected failure for bad-1, got %s", resp.BatchItemFailures[0].ItemIdentifier)
	}

	// 2 valid events should be written.
	if len(ms.writtenEvents) != 2 {
		t.Fatalf("expected 2 written events, got %d", len(ms.writtenEvents))
	}

	// Verify bot detection on the second event.
	if ms.writtenEvents[1].DeviceType != "bot" {
		t.Errorf("expected device type bot for Googlebot UA, got %s", ms.writtenEvents[1].DeviceType)
	}
}
