//go:build e2e

package e2e

import "testing"

func TestStatsE2E_ClickCountIncrements(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link, redirect through it, wait for SQS processing
	// GET /api/v1/links/{code}/stats, assert total_clicks=1
}

func TestStatsE2E_TimelineData(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link, generate clicks, GET /api/v1/links/{code}/stats/timeline?period=day
	// Assert array of date-count pairs
}

func TestStatsE2E_OwnerOnly(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link as user A, try to access stats as user B
	// Assert 403
}

func TestStatsE2E_AnonymousBlocked(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: GET /api/v1/links/{code}/stats without auth
	// Assert 401
}

func TestStatsE2E_ZeroClicks(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link, immediately GET stats
	// Assert total_clicks=0, unique_clicks=0
}
