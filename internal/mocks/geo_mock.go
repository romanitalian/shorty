package mocks

import (
	"context"
)

// MockGeoResolver implements geo.Resolver for testing.
type MockGeoResolver struct {
	CountryFn    func(ctx context.Context, ip string) string
	DeviceTypeFn func(ctx context.Context, userAgent string) string
}

func (m *MockGeoResolver) Country(ctx context.Context, ip string) string {
	if m.CountryFn != nil {
		return m.CountryFn(ctx, ip)
	}
	return "XX"
}

func (m *MockGeoResolver) DeviceType(ctx context.Context, userAgent string) string {
	if m.DeviceTypeFn != nil {
		return m.DeviceTypeFn(ctx, userAgent)
	}
	return "desktop"
}
