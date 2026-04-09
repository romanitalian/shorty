package ratelimit

import "time"

// Tier defines a rate limit tier with a name, request limit, and time window.
type Tier struct {
	Name   string
	Limit  int64
	Window time.Duration
}

// Pre-defined rate limit tiers matching requirements-init.md Section 7.
var (
	// AnonymousRedirect limits anonymous redirect requests per IP.
	// 200 requests per minute sliding window.
	AnonymousRedirect = Tier{"anon_redirect", 200, time.Minute}

	// AnonymousCreate limits anonymous link creation per IP.
	// 5 links per hour sliding window.
	AnonymousCreate = Tier{"anon_create", 5, time.Hour}

	// FreeCreate limits link creation for free-tier authenticated users.
	// 50 links per 24 hours sliding window.
	FreeCreate = Tier{"free_create", 50, 24 * time.Hour}

	// ProCreate limits link creation for pro-tier authenticated users.
	// 500 links per 24 hours sliding window.
	ProCreate = Tier{"pro_create", 500, 24 * time.Hour}

	// PasswordAttempt limits password attempts per code+IP.
	// 5 attempts per 15 minutes sliding window.
	PasswordAttempt = Tier{"password", 5, 15 * time.Minute}
)
