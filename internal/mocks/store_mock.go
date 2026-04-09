package mocks

import (
	"context"
	"time"

	"github.com/romanitalian/shorty/internal/store"
)

// MockStore implements store.Store for testing.
type MockStore struct {
	CreateLinkFn         func(ctx context.Context, link *store.Link) error
	GetLinkFn            func(ctx context.Context, code string) (*store.Link, error)
	UpdateLinkFn         func(ctx context.Context, code string, callerID string, updates map[string]interface{}) error
	DeleteLinkFn         func(ctx context.Context, code string, callerID string) error
	ListLinksByOwnerFn   func(ctx context.Context, ownerID string, cursor string, limit int) ([]*store.Link, string, error)
	IncrementClickCountFn func(ctx context.Context, code string, maxClicks *int64) (bool, error)
	BatchWriteClicksFn   func(ctx context.Context, events []*store.ClickEvent) error
	GetLinkStatsFn       func(ctx context.Context, code string) (*store.LinkStats, error)
	GetLinkTimelineFn    func(ctx context.Context, code string, from, to time.Time, granularity string) ([]store.TimelineBucket, error)
	GetLinkGeoFn         func(ctx context.Context, code string) ([]store.GeoStat, error)
	GetLinkReferrersFn   func(ctx context.Context, code string) ([]store.ReferrerStat, error)
	GetUserFn            func(ctx context.Context, userID string) (*store.User, error)
	UpdateUserQuotaFn    func(ctx context.Context, userID string) error
}

func (m *MockStore) CreateLink(ctx context.Context, link *store.Link) error {
	if m.CreateLinkFn != nil {
		return m.CreateLinkFn(ctx, link)
	}
	return nil
}

func (m *MockStore) GetLink(ctx context.Context, code string) (*store.Link, error) {
	if m.GetLinkFn != nil {
		return m.GetLinkFn(ctx, code)
	}
	return nil, store.ErrLinkNotFound
}

func (m *MockStore) UpdateLink(ctx context.Context, code string, callerID string, updates map[string]interface{}) error {
	if m.UpdateLinkFn != nil {
		return m.UpdateLinkFn(ctx, code, callerID, updates)
	}
	return nil
}

func (m *MockStore) DeleteLink(ctx context.Context, code string, callerID string) error {
	if m.DeleteLinkFn != nil {
		return m.DeleteLinkFn(ctx, code, callerID)
	}
	return nil
}

func (m *MockStore) ListLinksByOwner(ctx context.Context, ownerID string, cursor string, limit int) ([]*store.Link, string, error) {
	if m.ListLinksByOwnerFn != nil {
		return m.ListLinksByOwnerFn(ctx, ownerID, cursor, limit)
	}
	return nil, "", nil
}

func (m *MockStore) IncrementClickCount(ctx context.Context, code string, maxClicks *int64) (bool, error) {
	if m.IncrementClickCountFn != nil {
		return m.IncrementClickCountFn(ctx, code, maxClicks)
	}
	return true, nil
}

func (m *MockStore) BatchWriteClicks(ctx context.Context, events []*store.ClickEvent) error {
	if m.BatchWriteClicksFn != nil {
		return m.BatchWriteClicksFn(ctx, events)
	}
	return nil
}

func (m *MockStore) GetLinkStats(ctx context.Context, code string) (*store.LinkStats, error) {
	if m.GetLinkStatsFn != nil {
		return m.GetLinkStatsFn(ctx, code)
	}
	return &store.LinkStats{}, nil
}

func (m *MockStore) GetLinkTimeline(ctx context.Context, code string, from, to time.Time, granularity string) ([]store.TimelineBucket, error) {
	if m.GetLinkTimelineFn != nil {
		return m.GetLinkTimelineFn(ctx, code, from, to, granularity)
	}
	return nil, nil
}

func (m *MockStore) GetLinkGeo(ctx context.Context, code string) ([]store.GeoStat, error) {
	if m.GetLinkGeoFn != nil {
		return m.GetLinkGeoFn(ctx, code)
	}
	return nil, nil
}

func (m *MockStore) GetLinkReferrers(ctx context.Context, code string) ([]store.ReferrerStat, error) {
	if m.GetLinkReferrersFn != nil {
		return m.GetLinkReferrersFn(ctx, code)
	}
	return nil, nil
}

func (m *MockStore) GetUser(ctx context.Context, userID string) (*store.User, error) {
	if m.GetUserFn != nil {
		return m.GetUserFn(ctx, userID)
	}
	return nil, store.ErrUserNotFound
}

func (m *MockStore) UpdateUserQuota(ctx context.Context, userID string) error {
	if m.UpdateUserQuotaFn != nil {
		return m.UpdateUserQuotaFn(ctx, userID)
	}
	return nil
}
