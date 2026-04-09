package mocks

import (
	"context"
)

// MockValidator implements validator.Validator for testing.
type MockValidator struct {
	ValidateURLFn func(ctx context.Context, rawURL string) error
}

func (m *MockValidator) ValidateURL(ctx context.Context, rawURL string) error {
	if m.ValidateURLFn != nil {
		return m.ValidateURLFn(ctx, rawURL)
	}
	return nil
}
