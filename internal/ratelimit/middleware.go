package ratelimit

import (
	"net/http"
	"strconv"
)

// SetRateLimitHeaders sets standard rate limit response headers from a Result.
// Headers follow the IETF RateLimit fields draft convention:
//
//	X-RateLimit-Limit: maximum requests allowed in the window
//	X-RateLimit-Remaining: requests remaining in the current window
//	X-RateLimit-Reset: Unix timestamp when the window resets
//	Retry-After: seconds to wait before retrying (only set when rate limited)
func SetRateLimitHeaders(w http.ResponseWriter, result *Result) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))
	if !result.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
	}
}
