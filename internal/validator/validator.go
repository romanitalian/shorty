package validator

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"
)

// MaxURLLength is the maximum allowed URL length.
const MaxURLLength = 2048

// Sentinel errors for each validation failure type.
var (
	ErrURLEmpty        = fmt.Errorf("URL is required")
	ErrURLTooLong      = fmt.Errorf("URL exceeds maximum length of %d characters", MaxURLLength)
	ErrBlockedScheme   = fmt.Errorf("URL scheme is not allowed; only http and https are permitted")
	ErrInvalidURL      = fmt.Errorf("URL is not valid")
	ErrMissingHost     = fmt.Errorf("URL must have a hostname")
	ErrPrivateIP       = fmt.Errorf("URL host resolves to a private/reserved IP address")
	ErrDNSResolution   = fmt.Errorf("cannot resolve hostname")
	ErrMixedScriptIDN  = fmt.Errorf("internationalized domain uses mixed scripts (potential homograph attack)")
)

// blockedSchemes lists URI schemes that must never be stored.
var blockedSchemes = map[string]bool{
	"javascript": true,
	"data":       true,
	"vbscript":   true,
	"file":       true,
	"ftp":        true,
	"gopher":     true,
	"telnet":     true,
	"ssh":        true,
}

// privateRanges defines CIDR blocks that must be blocked.
var privateRanges = []string{
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.0.0/24",
	"198.18.0.0/15",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
	// NB: The IPv4-mapped IPv6 range "::ffff:0:0/96" cannot be listed here:
	// net.ParseCIDR reduces it to the IPv4 form 0.0.0.0/0, which matches
	// every public IPv4 address. The individual IPv4 ranges above already
	// cover that range in its IPv4 form.
}

var parsedPrivateRanges []*net.IPNet

func init() {
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %s: %v", cidr, err))
		}
		parsedPrivateRanges = append(parsedPrivateRanges, network)
	}
}

// Option configures a URLValidator.
type Option func(*urlValidator)

// WithDNSCheck enables DNS resolution checking (SSRF prevention).
// When enabled, hostnames are resolved and checked against private IP ranges.
func WithDNSCheck() Option {
	return func(v *urlValidator) {
		v.dnsCheck = true
	}
}

// Validator defines the interface for URL validation.
type Validator interface {
	// ValidateURL runs the full validation pipeline on the given URL.
	ValidateURL(ctx context.Context, rawURL string) error
}

// urlValidator implements Validator.
type urlValidator struct {
	dnsCheck bool
}

// New creates a new Validator with the given options.
func New(opts ...Option) Validator {
	v := &urlValidator{}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// ValidateURL runs the full validation pipeline.
func (v *urlValidator) ValidateURL(_ context.Context, rawURL string) error {
	// Step 1: Empty check.
	if len(rawURL) == 0 {
		return ErrURLEmpty
	}

	// Step 2: Length check.
	if len(rawURL) > MaxURLLength {
		return ErrURLTooLong
	}

	// Step 3: Scheme validation (blocks javascript:, data:, etc.).
	if err := validateScheme(rawURL); err != nil {
		return err
	}

	// Step 4: Parse URL.
	u, err := url.Parse(rawURL)
	if err != nil {
		return ErrInvalidURL
	}

	// Step 5: Host must be present.
	if u.Host == "" {
		return ErrMissingHost
	}

	hostname := u.Hostname()

	// Step 6: IDN homograph check.
	if err := validateIDN(hostname); err != nil {
		return err
	}

	// Step 7: Private IP check (direct IP in URL).
	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateIP(ip) {
			return ErrPrivateIP
		}
		return nil
	}

	// Step 8: DNS resolution check (optional).
	if v.dnsCheck {
		if err := validateHostDNS(hostname); err != nil {
			return err
		}
	}

	return nil
}

// validateScheme checks that the URL uses http or https.
// Strips control characters and whitespace first to catch obfuscation tricks.
func validateScheme(rawURL string) error {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, rawURL)

	u, err := url.Parse(cleaned)
	if err != nil {
		return ErrInvalidURL
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrBlockedScheme
	}

	if blockedSchemes[scheme] {
		return ErrBlockedScheme
	}

	return nil
}

// isPrivateIP checks whether an IP falls within any blocked range.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true // fail closed
	}
	for _, network := range parsedPrivateRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// validateHostDNS resolves the hostname and checks all returned IPs against private ranges.
func validateHostDNS(hostname string) error {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrDNSResolution, hostname, err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return ErrPrivateIP
		}
	}

	return nil
}

// validateIDN checks for IDN homograph attacks by detecting mixed-script hostnames.
func validateIDN(hostname string) error {
	// Only check hostnames that contain non-ASCII characters.
	hasNonASCII := false
	for _, r := range hostname {
		if r > 127 {
			hasNonASCII = true
			break
		}
	}
	if !hasNonASCII {
		return nil
	}

	if hasMixedScripts(hostname) {
		return ErrMixedScriptIDN
	}

	return nil
}

// hasMixedScripts detects domains using characters from multiple Unicode scripts.
func hasMixedScripts(domain string) bool {
	scripts := make(map[string]bool)
	for _, r := range domain {
		if r == '.' || r == '-' {
			continue
		}
		switch {
		case unicode.Is(unicode.Latin, r):
			scripts["Latin"] = true
		case unicode.Is(unicode.Cyrillic, r):
			scripts["Cyrillic"] = true
		case unicode.Is(unicode.Greek, r):
			scripts["Greek"] = true
		case unicode.Is(unicode.Han, r):
			scripts["Han"] = true
		case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			scripts["Japanese"] = true
		case unicode.Is(unicode.Hangul, r):
			scripts["Korean"] = true
		case unicode.Is(unicode.Arabic, r):
			scripts["Arabic"] = true
		case unicode.Is(unicode.Common, r) || unicode.Is(unicode.Number, r):
			continue
		default:
			scripts["Other"] = true
		}
	}
	return len(scripts) > 1
}
