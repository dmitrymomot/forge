package useragent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseOS(t *testing.T) {
	tests := []struct {
		name   string
		ua     string
		wantOS string
		full   string // expected Version.Full ("" = don't care that it's empty)
	}{
		{"windows 10/11 frozen", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36", "Windows", "10"},
		{"windows 8.1", "Mozilla/5.0 (Windows NT 6.3; Win64; x64; rv:109.0) Gecko/20100101 Firefox/115.0", "Windows", "8.1"},
		{"windows 8", "Mozilla/5.0 (Windows NT 6.2; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36", "Windows", "8"},
		{"windows 7", "Mozilla/5.0 (Windows NT 6.1; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36", "Windows", "7"},
		{"windows vista", "Mozilla/5.0 (Windows NT 6.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/49.0.2623.112 Safari/537.36", "Windows", "Vista"},
		{"windows xp", "Mozilla/5.0 (Windows NT 5.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/49.0.2623.112 Safari/537.36", "Windows", "XP"},
		{"windows phone", "Mozilla/5.0 (Windows Phone 8.1; ARM; Trident/7.0; Touch; rv:11.0; IEMobile/11.0; NOKIA; Lumia 640) like Gecko", "Windows Phone", "8.1"},
		{"macos frozen", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "macOS", "10.15.7"},
		{"ios iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "iOS", "17.5"},
		{"ios ipod", "Mozilla/5.0 (iPod touch; CPU iPhone OS 15_8 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.6 Mobile/15E148 Safari/604.1", "iOS", "15.8"},
		{"ipados", "Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1", "iPadOS", "16.6"},
		{"ipad desktop-mode heuristic", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "iPadOS", ""},
		{"android", "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36", "Android", "14"},
		{"harmonyos before android", "Mozilla/5.0 (Linux; Android 10; HarmonyOS; NOH-AN00) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.5735.196 HuaweiBrowser/15.0.4.302 Mobile Safari/537.36", "HarmonyOS", ""},
		{"chromeos", "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "ChromeOS", ""},
		{"linux", "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0", "Linux", ""},
		{"freebsd", "Mozilla/5.0 (X11; FreeBSD amd64; rv:139.0) Gecko/20100101 Firefox/139.0", "FreeBSD", ""},
		{"openbsd", "Mozilla/5.0 (X11; OpenBSD amd64; rv:139.0) Gecko/20100101 Firefox/139.0", "OpenBSD", ""},
		{"netbsd", "Mozilla/5.0 (X11; NetBSD amd64; rv:139.0) Gecko/20100101 Firefox/139.0", "NetBSD", ""},
		{"kaios", "Mozilla/5.0 (Mobile; LYF/F300B/LYF-F300B-001-02-15-130319; Android; rv:48.0) Gecko/48.0 Firefox/48.0 KAIOS/2.5", "KaiOS", "2.5"},
		{"tizen tv", "Mozilla/5.0 (SMART-TV; Linux; Tizen 6.0) AppleWebKit/537.36 (KHTML, like Gecko) 76.0.3809.146/6.0 TV Safari/537.36", "Tizen", "6.0"},
		{"webos tv", "Mozilla/5.0 (Web0S; Linux/SmartTV) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36 WebAppManager", "webOS", ""},
		{"playstation 5", "Mozilla/5.0 (PlayStation; PlayStation 5/2.26) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0 Safari/605.1.15", "PlayStation", "5"},
		{"xbox before windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; Xbox; Xbox One) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/48.0.2564.82 Safari/537.36 Edge/20.02", "Xbox", ""},
		{"nintendo switch", "Mozilla/5.0 (Nintendo Switch; WifiWebAuthApplet) AppleWebKit/609.4 (KHTML, like Gecko) NF/6.0.2.22.4 NintendoBrowser/5.1.0.22433", "Nintendo Switch", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := useragent.Parse(tt.ua)
			assert.Equal(t, tt.wantOS, got.OS.Name)
			if tt.full != "" {
				assert.Equal(t, tt.full, got.OS.Version.Full)
			}
		})
	}
}
