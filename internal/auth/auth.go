// Package auth provides JWT validation and AWS Cognito integration for Lambda authorization.
package auth

import (
	"context"
	"time"
)

// Claims represents the decoded JWT claims.
type Claims struct {
	Subject   string // Cognito user sub (UUID)
	Email     string
	Groups    []string // Cognito user groups
	ExpiresAt time.Time
	IssuedAt  time.Time
}

// Authenticator validates tokens and extracts claims.
type Authenticator interface {
	ValidateToken(ctx context.Context, tokenString string) (*Claims, error)
}

// contextKey is an unexported type for context keys in this package.
type contextKey int

const claimsKey contextKey = iota
