package mocks

import (
	"context"
)

// MockGenerator implements shortener.Generator for testing.
type MockGenerator struct {
	GenerateFn       func(ctx context.Context) (string, error)
	GenerateCustomFn func(ctx context.Context, code string) error
}

func (m *MockGenerator) Generate(ctx context.Context) (string, error) {
	if m.GenerateFn != nil {
		return m.GenerateFn(ctx)
	}
	return "abc1234", nil
}

func (m *MockGenerator) GenerateCustom(ctx context.Context, code string) error {
	if m.GenerateCustomFn != nil {
		return m.GenerateCustomFn(ctx, code)
	}
	return nil
}
