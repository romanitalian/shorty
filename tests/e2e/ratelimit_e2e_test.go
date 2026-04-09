//go:build e2e

package e2e

import "testing"

func TestRateLimitE2E_RedirectExceedsLimit(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Send 201 redirect requests in rapid succession from the same IP
	// Assert that at least one response is 429
	// Assert Retry-After header is present on 429 response
}

func TestRateLimitE2E_CreationExceedsGuestQuota(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Send 6 anonymous creation requests in quick succession
	// Assert that the 6th request returns 429
}

func TestRateLimitE2E_HeadersPresent(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Send a single redirect request
	// Assert X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset headers present
}

func TestRateLimitE2E_IndependentIPCounters(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Exhaust rate limit from IP A, verify IP B still gets 302
}
