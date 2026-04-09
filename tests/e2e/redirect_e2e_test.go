//go:build e2e

package e2e

import "testing"

func TestRedirectE2E_ActiveLink(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link via POST /api/v1/links, then GET /{code}
	// Assert 302 and correct Location header
}

func TestRedirectE2E_ExpiredLink(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link with past expires_at, then GET /{code}
	// Assert 410 Gone
}

func TestRedirectE2E_ClickExhaustedLink(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link with max_clicks=1, consume the click, then GET /{code}
	// Assert 410 Gone
}

func TestRedirectE2E_NonexistentCode(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: GET /nonexistent-code
	// Assert 404
}

func TestRedirectE2E_CacheWarmAfterFirstHit(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link, GET /{code} twice
	// Second request should be served from cache (verify via latency or metrics)
}
