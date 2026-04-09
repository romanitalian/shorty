package geo

import (
	"context"
	"strings"
)

// Resolver provides geographic and device-type information for click events.
type Resolver interface {
	// Country returns the ISO 3166-1 alpha-2 country code for the given IP.
	Country(ctx context.Context, ip string) string
	// DeviceType classifies a User-Agent as "mobile", "bot", or "desktop".
	DeviceType(ctx context.Context, userAgent string) string
}

// StubResolver is a post-MVP stub that returns a default country and
// classifies device type via simple User-Agent string matching.
type StubResolver struct{}

// NewStubResolver creates a new StubResolver.
func NewStubResolver() *StubResolver {
	return &StubResolver{}
}

// Country always returns "XX" (unknown). Real GeoIP integration is post-MVP.
func (s *StubResolver) Country(_ context.Context, _ string) string {
	return "XX"
}

// DeviceType classifies the User-Agent string:
//   - Contains "Mobile", "Android", or "iPhone" -> "mobile"
//   - Contains "bot", "spider", or "crawl" (case-insensitive) -> "bot"
//   - Otherwise -> "desktop"
func (s *StubResolver) DeviceType(_ context.Context, userAgent string) string {
	lower := strings.ToLower(userAgent)

	// Bot detection first (bots can spoof mobile UAs).
	if strings.Contains(lower, "bot") ||
		strings.Contains(lower, "spider") ||
		strings.Contains(lower, "crawl") {
		return "bot"
	}

	// Mobile detection (case-sensitive keywords also caught via lowered check).
	if strings.Contains(lower, "mobile") ||
		strings.Contains(lower, "android") ||
		strings.Contains(lower, "iphone") {
		return "mobile"
	}

	return "desktop"
}
