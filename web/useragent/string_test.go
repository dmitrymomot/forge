package useragent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestStringGoldens(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want string
	}{
		{"desktop browser", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36", "Chrome 138 on Windows 10 (Desktop)"},
		{"mobile safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "Mobile Safari 17 on iOS 17 (Mobile)"},
		{"windows 8.1 keeps minor", "Mozilla/5.0 (Windows NT 6.3; Win64; x64; rv:109.0) Gecko/20100101 Firefox/115.0", "Firefox 115 on Windows 8.1 (Desktop)"},
		{"xp non-numeric", "Mozilla/5.0 (Windows NT 5.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/49.0.2623.112 Safari/537.36", "Chrome 49 on Windows XP (Desktop)"},
		{"named bot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.2; +https://openai.com/gptbot", "GPTBot (AI crawler)"},
		{"unnamed bot", "Zzqxbot/1.0", "Bot"},
		{"library", "curl/8.6.0", "curl (HTTP library)"},
		{"unknown", "!!!???", "Unknown"},
		{"os only no browser", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) like Gecko", "Windows 10 (Desktop)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, useragent.Parse(tt.ua).String())
		})
	}
}

func TestStringMacCH(t *testing.T) {
	// spec golden: "Chrome 138 on macOS 14 (Desktop)"
	h := headers("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36", map[string]string{
		"Sec-CH-UA-Platform":         `"macOS"`,
		"Sec-CH-UA-Platform-Version": `"14.5.0"`,
	})
	assert.Equal(t, "Chrome 138 on macOS 14 (Desktop)", useragent.ParseHeaders(h).String())
}
