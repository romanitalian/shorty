package validator

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newTestValidator() Validator {
	return New() // no DNS check for unit tests
}

func TestValidateURL_ValidHTTP(t *testing.T) {
	v := newTestValidator()
	urls := []string{
		"http://example.com",
		"https://example.com",
		"https://example.com/path?q=1&r=2",
		"http://example.com:8080/path",
		"https://sub.domain.example.com",
	}
	for _, u := range urls {
		if err := v.ValidateURL(context.Background(), u); err != nil {
			t.Errorf("expected %q to pass, got: %v", u, err)
		}
	}
}

func TestValidateURL_JavascriptScheme(t *testing.T) {
	v := newTestValidator()
	urls := []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"JAVASCRIPT:alert(1)",
	}
	for _, u := range urls {
		err := v.ValidateURL(context.Background(), u)
		if !errors.Is(err, ErrBlockedScheme) {
			t.Errorf("expected ErrBlockedScheme for %q, got: %v", u, err)
		}
	}
}

func TestValidateURL_DataScheme(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "data:text/html,<script>alert(1)</script>")
	if !errors.Is(err, ErrBlockedScheme) {
		t.Errorf("expected ErrBlockedScheme, got: %v", err)
	}
}

func TestValidateURL_FileScheme(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "file:///etc/passwd")
	if !errors.Is(err, ErrBlockedScheme) {
		t.Errorf("expected ErrBlockedScheme, got: %v", err)
	}
}

func TestValidateURL_FTPScheme(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "ftp://ftp.example.com/file")
	if !errors.Is(err, ErrBlockedScheme) {
		t.Errorf("expected ErrBlockedScheme, got: %v", err)
	}
}

func TestValidateURL_PrivateIPs(t *testing.T) {
	v := newTestValidator()
	urls := []string{
		"http://10.0.0.1",
		"http://127.0.0.1",
		"http://192.168.1.1",
		"http://172.16.0.1",
		"http://169.254.169.254", // AWS metadata
	}
	for _, u := range urls {
		err := v.ValidateURL(context.Background(), u)
		if !errors.Is(err, ErrPrivateIP) {
			t.Errorf("expected ErrPrivateIP for %q, got: %v", u, err)
		}
	}
}

func TestValidateURL_IPv6Loopback(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "http://[::1]")
	if !errors.Is(err, ErrPrivateIP) {
		t.Errorf("expected ErrPrivateIP for IPv6 loopback, got: %v", err)
	}
}

func TestValidateURL_TooLong(t *testing.T) {
	v := newTestValidator()
	longURL := "https://example.com/" + strings.Repeat("a", MaxURLLength)
	err := v.ValidateURL(context.Background(), longURL)
	if !errors.Is(err, ErrURLTooLong) {
		t.Errorf("expected ErrURLTooLong, got: %v", err)
	}
}

func TestValidateURL_Empty(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "")
	if !errors.Is(err, ErrURLEmpty) {
		t.Errorf("expected ErrURLEmpty, got: %v", err)
	}
}

func TestValidateURL_NoHost(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "http://")
	if !errors.Is(err, ErrMissingHost) {
		t.Errorf("expected ErrMissingHost, got: %v", err)
	}
}

func TestValidateURL_ValidInternationalDomain(t *testing.T) {
	v := newTestValidator()
	// Pure Cyrillic domain -- single script, should pass.
	err := v.ValidateURL(context.Background(), "https://\u043f\u0440\u0438\u043c\u0435\u0440.\u0440\u0444")
	if err != nil {
		t.Errorf("expected valid international domain to pass, got: %v", err)
	}
}

func TestValidateURL_MixedScriptIDN(t *testing.T) {
	v := newTestValidator()
	// Mix of Cyrillic 'a' (U+0430) with Latin characters -- homograph attack.
	err := v.ValidateURL(context.Background(), "https://\u0430pple.com")
	if !errors.Is(err, ErrMixedScriptIDN) {
		t.Errorf("expected ErrMixedScriptIDN for mixed-script IDN, got: %v", err)
	}
}

func TestValidateURL_VBScriptScheme(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "vbscript:MsgBox(1)")
	if !errors.Is(err, ErrBlockedScheme) {
		t.Errorf("expected ErrBlockedScheme, got: %v", err)
	}
}

func TestValidateURL_NoScheme(t *testing.T) {
	v := newTestValidator()
	err := v.ValidateURL(context.Background(), "example.com")
	if !errors.Is(err, ErrBlockedScheme) && !errors.Is(err, ErrMissingHost) {
		t.Errorf("expected ErrBlockedScheme or ErrMissingHost for schemeless URL, got: %v", err)
	}
}

func TestIsPrivateIP_NilIP(t *testing.T) {
	if !isPrivateIP(nil) {
		t.Error("expected nil IP to be treated as private (fail closed)")
	}
}

func TestHasMixedScripts_PureLatin(t *testing.T) {
	if hasMixedScripts("example.com") {
		t.Error("pure Latin domain should not be flagged as mixed scripts")
	}
}

func TestHasMixedScripts_PureCyrillic(t *testing.T) {
	if hasMixedScripts("\u043f\u0440\u0438\u043c\u0435\u0440.\u0440\u0444") {
		t.Error("pure Cyrillic domain should not be flagged as mixed scripts")
	}
}

func TestHasMixedScripts_Mixed(t *testing.T) {
	// Cyrillic 'а' + Latin 'pple'
	if !hasMixedScripts("\u0430pple") {
		t.Error("mixed Cyrillic+Latin should be flagged")
	}
}
