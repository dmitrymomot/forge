package useragent_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/useragent"
)

func BenchmarkParse(b *testing.B) {
	cases := []struct{ name, ua string }{
		{"chrome-windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"},
		{"android-mobile", "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36"},
		{"iphone-safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"},
		{"bot-curated", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"},
		{"bot-generic", "Zzqxbot/1.0 (+https://zzqx.invalid)"},
		{"garbage", strings.Repeat("x", 300)},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = useragent.Parse(c.ua)
			}
		})
	}
}
