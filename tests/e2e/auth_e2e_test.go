//go:build e2e

package e2e

import "testing"

func TestAuthE2E_UnauthenticatedCreateReturns401(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: POST /api/v1/links without auth token
	// Assert 401
}

func TestAuthE2E_ValidTokenAccepted(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Authenticate via Cognito, use token to POST /api/v1/links
	// Assert 201
}

func TestAuthE2E_ExpiredTokenRejected(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Use an expired JWT token
	// Assert 401
}

func TestAuthE2E_TamperedTokenRejected(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Modify a valid JWT payload and send it
	// Assert 401
}
