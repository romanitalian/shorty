package mocks

import (
	"context"
	"time"

	"github.com/romanitalian/shorty/internal/store"
)

// MockCache implements cache.Cache for testing.
type MockCache struct {
	GetLinkFn     func(ctx context.Context, code string) (*store.Link, error)
	SetLinkFn     func(ctx context.Context, code string, link *store.Link, ttl time.Duration) error
	DeleteLinkFn  func(ctx context.Context, code string) error
	SetNegativeFn func(ctx context.Context, code string) error
	IsNegativeFn  func(ctx context.Context, code string) (bool, error)
}

func (m *MockCache) GetLink(ctx context.Context, code string) (*store.Link, error) {
	if m.GetLinkFn != nil {
		return m.GetLinkFn(ctx, code)
	}
	return nil, nil
}

func (m *MockCache) SetLink(ctx context.Context, code string, link *store.Link, ttl time.Duration) error {
	if m.SetLinkFn != nil {
		return m.SetLinkFn(ctx, code, link, ttl)
	}
	return nil
}

func (m *MockCache) DeleteLink(ctx context.Context, code string) error {
	if m.DeleteLinkFn != nil {
		return m.DeleteLinkFn(ctx, code)
	}
	return nil
}

func (m *MockCache) SetNegative(ctx context.Context, code string) error {
	if m.SetNegativeFn != nil {
		return m.SetNegativeFn(ctx, code)
	}
	return nil
}

func (m *MockCache) IsNegative(ctx context.Context, code string) (bool, error) {
	if m.IsNegativeFn != nil {
		return m.IsNegativeFn(ctx, code)
	}
	return false, nil
}
