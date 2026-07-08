package useragent_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestAdversarialInputs(t *testing.T) {
	inputs := []string{
		"",
		"Mozilla/5.0",
		"(((((((((",
		"))))" + strings.Repeat(";", 100),
		strings.Repeat("Chrome/", 200),
		strings.Repeat("\xff", 64),
		"Mozilla/5.0 (\x00\x00) Chrome/1.0",
		"Chrome/99999999999999999999999999999999999999",
		strings.Repeat("a", 100_000),
	}
	for _, ua := range inputs {
		res := useragent.Parse(ua) // must not panic
		assert.Equal(t, ua, res.Raw)
		assert.Equal(t, res.IsBot(), res.Device.Type == useragent.DeviceBot)
	}
}

func FuzzParse(f *testing.F) {
	f.Add("")
	f.Add("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	f.Add("Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1")
	f.Add("Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36")
	f.Add("Googlebot/2.1 (+http://www.google.com/bot.html)")
	f.Add("curl/8.6.0")
	f.Add("\x00\xff bot")
	f.Fuzz(func(t *testing.T, ua string) {
		res := useragent.Parse(ua)
		if res.Raw != ua {
			t.Fatalf("Raw mutated: %q → %q", ua, res.Raw)
		}
		if res.IsBot() != (res.Device.Type == useragent.DeviceBot) {
			t.Fatal("IsBot and Device.Type disagree")
		}
	})
}

func FuzzParseHeaders(f *testing.F) {
	f.Add("Mozilla/5.0 Chrome/138.0.0.0", `"Brave";v="138"`, `"Windows"`, `"15.0.0"`, `"Pixel 8"`)
	f.Add("", "", "", "", "")
	f.Fuzz(func(t *testing.T, ua, chua, platform, pver, model string) {
		h := http.Header{}
		h.Set("User-Agent", ua)
		h.Set("Sec-CH-UA", chua)
		h.Set("Sec-CH-UA-Platform", platform)
		h.Set("Sec-CH-UA-Platform-Version", pver)
		h.Set("Sec-CH-UA-Model", model)
		res := useragent.ParseHeaders(h) // must not panic
		_ = res.String()                 // display must not panic either
	})
}
