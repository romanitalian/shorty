package geo

import (
	"context"
	"testing"
)

func TestStubResolver_Country(t *testing.T) {
	r := NewStubResolver()
	ctx := context.Background()

	tests := []struct {
		ip   string
		want string
	}{
		{"1.2.3.4", "XX"},
		{"192.168.0.1", "XX"},
		{"::1", "XX"},
		{"", "XX"},
	}

	for _, tc := range tests {
		got := r.Country(ctx, tc.ip)
		if got != tc.want {
			t.Errorf("Country(%q) = %q, want %q", tc.ip, got, tc.want)
		}
	}
}

func TestStubResolver_DeviceType(t *testing.T) {
	r := NewStubResolver()
	ctx := context.Background()

	tests := []struct {
		ua   string
		want string
	}{
		// Desktop
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36", "desktop"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", "desktop"},
		{"", "desktop"},

		// Mobile
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)", "mobile"},
		{"Mozilla/5.0 (Linux; Android 13; Pixel 7)", "mobile"},
		{"Mozilla/5.0 (Linux; Android 12; SM-G991B) AppleWebKit/537.36 Mobile", "mobile"},

		// Bot
		{"Googlebot/2.1 (+http://www.google.com/bot.html)", "bot"},
		{"Mozilla/5.0 (compatible; Bingbot/2.0; +http://www.bing.com/bingbot.htm)", "bot"},
		{"Sogou web spider/4.0", "bot"},
		{"Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", "bot"},
		{"CCBot/2.0 (https://commoncrawl.org/faq/)", "bot"},
		{"DataForSeoBot (https://dataforseo.com/dataforseo-bot)", "bot"},

		// Bot with mobile UA (bot wins)
		{"Mozilla/5.0 (Linux; Android 6.0.1) AppleWebKit/537.36 Mobile Googlebot/2.1", "bot"},
	}

	for _, tc := range tests {
		got := r.DeviceType(ctx, tc.ua)
		if got != tc.want {
			t.Errorf("DeviceType(%q) = %q, want %q", tc.ua, got, tc.want)
		}
	}
}
