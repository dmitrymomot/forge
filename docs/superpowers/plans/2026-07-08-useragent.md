# web/useragent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `web/useragent` — a stdlib-only User-Agent + UA Client Hints parser producing structured browser/OS/device/bot facts with maximum practical detection breadth.

**Architecture:** Single-pass matcher over a lowercased byte copy of the UA string; ordered token tables for browsers/OS/devices; three-layer bot detection (curated table → generated table → generic heuristics) gated by a cheap trigger scan; field-by-field Client Hints override on top of the string parse. Spec: `docs/superpowers/specs/2026-07-08-useragent-design.md`.

**Tech Stack:** Go 1.26, stdlib only (net/http for headers). Tests use testify (`assert`/`require`). Codegen tool under `web/useragent/gen`.

## Global Constraints

- Work ONLY in the current branch `claude/relaxed-lalande-afbbf3` (worktree currently at `/Users/dmitrymomot/Dev/claude_worktrees/forge/brave-yonath-8889ee`). Never switch branches.
- Module path: `github.com/dmitrymomot/forge`. Package path: `web/useragent`.
- Zero forge deps, zero third-party deps in the package (testify allowed in tests only).
- Black-box tests ONLY (`package useragent_test`); a small `export_test.go` shim may re-export internal tables for invariant tests (allowed: white-box only to assert unexported state).
- `Parse` never returns an error and never panics. Unrecognized input → zero values with `Device.Type == DeviceUnknown`, `Raw` preserved.
- No options pattern, no builder, no configuration — pure functions.
- After changing files: `just fmt ./web/useragent/...` (package-path form — single-file form trips a betteralign bug). After each task: `just lint`.
- Go 1.26 modernize lint: use `for i := range n` over ints, `new(expr)` where applicable; run before committing.
- Commit after every task with a conventional message. No Claude attribution anywhere.

---

### Task 1: Package scaffold — types, Parse skeleton, matcher input

**Files:**
- Create: `web/useragent/useragent.go`
- Create: `web/useragent/tokenizer.go`
- Create: `web/useragent/browser.go` (stub)
- Create: `web/useragent/os.go` (stub)
- Create: `web/useragent/device.go` (stub)
- Create: `web/useragent/bot.go` (stub)
- Create: `web/useragent/bot_generated.go` (empty placeholder)
- Test: `web/useragent/useragent_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (used by all later tasks):
  - `type input struct { raw string; lower []byte }`, `newInput(raw string, buf []byte) input`
  - `(in input) contains(sub string) bool`, `(in input) index(sub string) int` — `sub` MUST be lowercase
  - `(in input) versionAfter(tok string) Version`, `parseVersion(full string) Version`
  - Exported: `Parse`, `UserAgent`, `Browser`, `OS`, `Device`, `Bot`, `Version`, `DeviceType` + constants, `BotCategory` + constants, `IsBot()`
  - Stubs replaced later: `detectBrowser(in input) Browser`, `detectOS(in input) OS`, `detectDevice(in input, osName string) Device`, `detectBot(in input) (Bot, bool)`, `var generatedBotPatterns []string`

- [ ] **Step 1: Write the failing test**

`web/useragent/useragent_test.go`:

```go
package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseEmptyString(t *testing.T) {
	res := useragent.Parse("")
	assert.Equal(t, "", res.Raw)
	assert.Equal(t, useragent.DeviceUnknown, res.Device.Type)
	assert.False(t, res.IsBot())
	assert.Empty(t, res.Browser.Name)
	assert.Empty(t, res.OS.Name)
}

func TestParseGarbage(t *testing.T) {
	for _, ua := range []string{
		"!!!???",
		"Mozilla/5.0",
		"\x00\x01\x02",
		"ÜmlautüberallÄÖ",
		strings.Repeat("x", 10*1024), // forces the heap fallback past the 512-byte stack buffer
	} {
		res := useragent.Parse(ua)
		assert.Equal(t, ua, res.Raw, "raw must round-trip")
		assert.Equal(t, useragent.DeviceUnknown, res.Device.Type)
		assert.False(t, res.IsBot())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/ -run 'TestParseEmptyString|TestParseGarbage' -v`
Expected: FAIL — package does not compile (`useragent` undefined).

- [ ] **Step 3: Write the types and Parse**

`web/useragent/useragent.go`:

```go
package useragent

// DeviceType classifies the hardware class a user agent runs on.
type DeviceType string

const (
	DeviceUnknown  DeviceType = "unknown"
	DeviceDesktop  DeviceType = "desktop"
	DeviceMobile   DeviceType = "mobile"
	DeviceTablet   DeviceType = "tablet"
	DeviceTV       DeviceType = "tv"
	DeviceConsole  DeviceType = "console"
	DeviceWearable DeviceType = "wearable"
	DeviceEReader  DeviceType = "ereader"
	DeviceBot      DeviceType = "bot"
)

// BotCategory classifies what an automated agent is for.
type BotCategory string

const (
	BotUnknown       BotCategory = "unknown"
	BotSearchEngine  BotCategory = "search_engine"
	BotAICrawler     BotCategory = "ai_crawler"
	BotAIAgent       BotCategory = "ai_agent"
	BotSocialPreview BotCategory = "social_preview"
	BotMonitoring    BotCategory = "monitoring"
	BotScraper       BotCategory = "scraper"
	BotSEO           BotCategory = "seo"
	BotAdvertising   BotCategory = "advertising"
	BotSecurity      BotCategory = "security"
	BotWebhook       BotCategory = "webhook"
	BotFeedFetcher   BotCategory = "feed_fetcher"
	BotLibrary       BotCategory = "library"
)

// Version holds a parsed dotted version. Full preserves everything that was
// matched (e.g. "138.0.7204.97"); Major/Minor/Patch hold the first three
// numeric segments.
type Version struct {
	Major, Minor, Patch int
	Full                string
}

// IsZero reports whether no version was detected.
func (v Version) IsZero() bool { return v.Full == "" }

// Browser identifies the browser or embedded webview.
type Browser struct {
	Name    string
	Version Version
}

// OS identifies the operating system with marketing versioning
// (Windows NT 6.3 parses as "8.1").
type OS struct {
	Name    string
	Version Version
}

// Device identifies the hardware class and, when the UA exposes it,
// the vendor and model.
type Device struct {
	Type         DeviceType
	Brand, Model string
}

// Bot identifies an automated agent. Zero value unless IsBot().
type Bot struct {
	Name     string
	Category BotCategory
	Operator string
}

// UserAgent is the parsed result. Zero fields mean "not detected" —
// parsing never fails.
type UserAgent struct {
	Browser Browser
	OS      OS
	Device  Device
	Bot     Bot
	Raw     string
}

// IsBot reports whether the user agent is an automated client.
func (ua UserAgent) IsBot() bool { return ua.Device.Type == DeviceBot }

// Parse extracts browser, OS, device, and bot facts from a raw User-Agent
// string. It never returns an error and never panics; unrecognized input
// yields zero values with Device.Type == DeviceUnknown.
func Parse(ua string) UserAgent {
	res := UserAgent{Raw: ua, Device: Device{Type: DeviceUnknown}}
	if ua == "" {
		return res
	}
	var buf [512]byte
	in := newInput(ua, buf[:0])
	if bot, ok := detectBot(in); ok {
		res.Bot = bot
		res.Device.Type = DeviceBot
		return res
	}
	res.Browser = detectBrowser(in)
	res.OS = detectOS(in)
	res.Device = detectDevice(in, res.OS.Name)
	if res.Device.Brand == "Amazon" && res.OS.Name == "Android" {
		res.OS = OS{Name: "Fire OS"} // Fire devices run Android but report Fire OS to users
	}
	return res
}
```

`web/useragent/tokenizer.go`:

```go
package useragent

import (
	"bytes"
	"strings"
)

// input is the shared matcher state: the original string for extraction
// (preserves model casing) and an ASCII-lowercased copy for matching.
type input struct {
	raw   string
	lower []byte
}

// newInput lowercases raw into buf (heap fallback when raw exceeds cap).
func newInput(raw string, buf []byte) input {
	b := buf
	if cap(b) < len(raw) {
		b = make([]byte, 0, len(raw))
	}
	for i := range len(raw) {
		c := raw[i]
		if c >= 'A' && c <= 'Z' {
			c |= 0x20
		}
		b = append(b, c)
	}
	return input{raw: raw, lower: b}
}

// index returns the offset of the lowercase needle sub in the lowered UA,
// or -1. Alloc-free: string(b) == s comparisons do not allocate.
func (in input) index(sub string) int {
	n := len(sub)
	if n == 0 || n > len(in.lower) {
		return -1
	}
	off := 0
	h := in.lower
	for {
		i := bytes.IndexByte(h, sub[0])
		if i < 0 || i+n > len(h) {
			return -1
		}
		if string(h[i:i+n]) == sub {
			return off + i
		}
		h = h[i+1:]
		off += i + 1
	}
}

func (in input) contains(sub string) bool { return in.index(sub) >= 0 }

// versionAfter reads the dotted (or underscored, iOS-style) version that
// follows the first occurrence of tok: versionAfter("chrome/") on
// "... Chrome/138.0.0.0 ..." yields 138.0.0.0.
func (in input) versionAfter(tok string) Version {
	i := in.index(tok)
	if i < 0 {
		return Version{}
	}
	return versionAt(in.raw, i+len(tok))
}

func versionAt(s string, start int) Version {
	end := start
	underscored := false
	for end < len(s) {
		c := s[end]
		switch {
		case c >= '0' && c <= '9' || c == '.':
			end++
		case c == '_':
			underscored = true
			end++
		default:
			goto scanned
		}
	}
scanned:
	for end > start && (s[end-1] == '.' || s[end-1] == '_') {
		end--
	}
	if end == start {
		return Version{}
	}
	full := s[start:end]
	if underscored {
		full = strings.ReplaceAll(full, "_", ".") // iOS "16_6" → "16.6"; allocates only on this path
	}
	return parseVersion(full)
}

// parseVersion fills Major/Minor/Patch from the leading numeric segments;
// segments beyond the third survive only in Full.
func parseVersion(full string) Version {
	v := Version{Full: full}
	part, n := 0, 0
	seen := false
	for i := 0; i <= len(full); i++ {
		if i < len(full) {
			if c := full[i]; c >= '0' && c <= '9' {
				if n < 1<<28 { // clamp: garbage like 40 digits must not overflow
					n = n*10 + int(c-'0')
				}
				seen = true
				continue
			}
		}
		if seen {
			switch part {
			case 0:
				v.Major = n
			case 1:
				v.Minor = n
			case 2:
				v.Patch = n
			}
		}
		part++
		n = 0
		seen = false
		if part > 2 {
			break
		}
	}
	return v
}
```

Stub files so the package compiles — each is fully replaced by its own task:

`web/useragent/browser.go`:

```go
package useragent

func detectBrowser(in input) Browser { return Browser{} }
```

`web/useragent/os.go`:

```go
package useragent

func detectOS(in input) OS { return OS{} }
```

`web/useragent/device.go`:

```go
package useragent

func detectDevice(in input, osName string) Device { return Device{Type: DeviceUnknown} }
```

`web/useragent/bot.go`:

```go
package useragent

func detectBot(in input) (Bot, bool) { return Bot{}, false }
```

`web/useragent/bot_generated.go`:

```go
package useragent

// Replaced by `go run ./web/useragent/gen` in Task 6.
var generatedBotPatterns []string
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/useragent/ -run 'TestParseEmptyString|TestParseGarbage' -v`
Expected: PASS. (`go vet ./web/useragent/` may flag unused params in stubs — it does not; blank them only if it complains.)

- [ ] **Step 5: Format and commit**

```bash
just fmt ./web/useragent/...
git add web/useragent/
git commit -m "feat(useragent): package scaffold with types, Parse skeleton, matcher input"
```

---

### Task 2: Browser detection

**Files:**
- Replace: `web/useragent/browser.go`
- Test: `web/useragent/browser_test.go`

**Interfaces:**
- Consumes: `input.contains/versionAfter`, `Browser`, `Version` from Task 1.
- Produces: `detectBrowser(in input) Browser` returning canonical names: Edge, Opera, Opera Mini, Samsung Internet, Vivaldi, Yandex, UC Browser, WeChat, QQ Browser, DuckDuckGo, Whale, MIUI Browser, Huawei Browser, Facebook, Instagram, TikTok, Line, Google App, Firefox, Chrome, Android WebView, iOS WebView, Safari, Mobile Safari.

- [ ] **Step 1: Write the failing test**

`web/useragent/browser_test.go`:

```go
package useragent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseBrowsers(t *testing.T) {
	tests := []struct {
		name  string
		ua    string
		want  string
		major int
	}{
		{"chrome windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36", "Chrome", 138},
		{"edge embeds chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36 Edg/138.0.3351.65", "Edge", 138},
		{"edge ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 EdgiOS/125.2535.60 Mobile/15E148 Safari/605.1.15", "Edge", 125},
		{"opera embeds chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36 OPR/120.0.0.0", "Opera", 120},
		{"opera mini", "Opera/9.80 (Android; Opera Mini/12.0.1987/37.7327; U; en) Presto/2.12.423 Version/12.16", "Opera Mini", 12},
		{"legacy opera presto", "Opera/9.80 (Windows NT 6.1; WOW64) Presto/2.12.388 Version/12.18", "Opera", 12},
		{"samsung internet", "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/27.0 Chrome/125.0.0.0 Mobile Safari/537.36", "Samsung Internet", 27},
		{"vivaldi", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36 Vivaldi/7.4.3684.52", "Vivaldi", 7},
		{"yandex", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 YaBrowser/25.6.0.0 Safari/537.36", "Yandex", 25},
		{"uc browser", "Mozilla/5.0 (Linux; U; Android 12; en-US; RMX3085 Build/SP1A.210812.016) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/100.0.4896.58 UCBrowser/13.4.0.1306 Mobile Safari/537.36", "UC Browser", 13},
		{"wechat", "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/122.0.0.0 Mobile Safari/537.36 MicroMessenger/8.0.49.2600(0x28003133)", "WeChat", 8},
		{"qq browser", "Mozilla/5.0 (Linux; U; Android 13; zh-cn; 2211133C Build/TKQ1.220905.001) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/110.0.5481.63 MQQBrowser/15.1 Mobile Safari/537.36", "QQ Browser", 15},
		{"duckduckgo android", "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/138.0.0.0 Mobile Safari/537.36 DuckDuckGo/5", "DuckDuckGo", 5},
		{"duckduckgo ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Ddg/17.5 Mobile/15E148 Safari/604.1", "DuckDuckGo", 17},
		{"whale", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Whale/4.32.315.22 Safari/537.36", "Whale", 4},
		{"miui browser", "Mozilla/5.0 (Linux; U; Android 13; en-us; Redmi Note 12 Build/TKQ1.221114.001) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/118.0.5993.80 Mobile Safari/537.36 XiaoMi/MiuiBrowser/14.28.0-gn", "MIUI Browser", 14},
		{"huawei browser", "Mozilla/5.0 (Linux; Android 10; HarmonyOS; NOH-AN00; HMSCore 6.13.0.352) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.5735.196 HuaweiBrowser/15.0.4.302 Mobile Safari/537.36", "Huawei Browser", 15},
		{"facebook ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/21F90 [FBAN/FBIOS;FBAV/512.0.0.30.107;FBBV/700399097]", "Facebook", 512},
		{"instagram android before wv", "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 Instagram 361.0.0.35.82 Android", "Instagram", 361},
		{"tiktok", "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 musical_ly_2022803030 JsSdk/1.0 NetType/WIFI Channel/googleplay AppName/musical_ly app_version/28.0.3", "TikTok", 28},
		{"line ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1 Line/13.12.0/IAB", "Line", 13},
		{"google app ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) GSA/329.0.544521661 Mobile/15E148 Safari/604.1", "Google App", 329},
		{"firefox windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0", "Firefox", 140},
		{"firefox ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/141.0 Mobile/15E148 Safari/605.1.15", "Firefox", 141},
		{"chrome ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/138.0.7204.63 Mobile/15E148 Safari/604.1", "Chrome", 138},
		{"android webview", "Mozilla/5.0 (Linux; Android 14; SM-A536B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36", "Android WebView", 125},
		{"safari macos", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "Safari", 17},
		{"mobile safari iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "Mobile Safari", 17},
		{"mobile safari ipad", "Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1", "Mobile Safari", 16},
		{"ios wkwebview heuristic", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148", "iOS WebView", 0},
		{"bare webkit token is not safari", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := useragent.Parse(tt.ua)
			assert.Equal(t, tt.want, got.Browser.Name)
			assert.Equal(t, tt.major, got.Browser.Version.Major)
		})
	}
}

func TestParseBrowserFullVersion(t *testing.T) {
	got := useragent.Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.7204.97 Safari/537.36")
	assert.Equal(t, useragent.Version{Major: 138, Minor: 0, Patch: 7204, Full: "138.0.7204.97"}, got.Browser.Version)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/ -run 'TestParseBrowser' -v`
Expected: FAIL — stub returns empty Browser for every case.

- [ ] **Step 3: Implement browser matching**

Replace `web/useragent/browser.go`:

```go
package useragent

// browserRule maps a distinctive lowercase token to a canonical browser
// name. vtok names the token whose trailing characters carry the version;
// empty vtok means the identifying token itself.
type browserRule struct {
	token string
	name  string
	vtok  string
}

// Order matters: Chromium forks and in-app browsers embed "Chrome/", and
// nearly everything embeds "Safari/", so distinctive tokens come first,
// Chrome second-to-last, Safari last.
var browserRules = []browserRule{
	{token: "edg/", name: "Edge"},
	{token: "edga/", name: "Edge"},
	{token: "edgios/", name: "Edge"},
	{token: "edge/", name: "Edge"},
	{token: "opr/", name: "Opera"},
	{token: "opios/", name: "Opera"},
	{token: "opera mini/", name: "Opera Mini"},
	{token: "opera/", name: "Opera", vtok: "version/"},
	{token: "samsungbrowser/", name: "Samsung Internet"},
	{token: "vivaldi/", name: "Vivaldi"},
	{token: "yabrowser/", name: "Yandex"},
	{token: "ucbrowser/", name: "UC Browser"},
	{token: "micromessenger/", name: "WeChat"},
	{token: "mqqbrowser/", name: "QQ Browser"},
	{token: "qqbrowser/", name: "QQ Browser"},
	{token: "duckduckgo/", name: "DuckDuckGo"},
	{token: "ddg/", name: "DuckDuckGo"},
	{token: "whale/", name: "Whale"},
	{token: "miuibrowser/", name: "MIUI Browser"},
	{token: "huaweibrowser/", name: "Huawei Browser"},
	{token: "fbav/", name: "Facebook"},
	{token: "fban", name: "Facebook", vtok: "fbav/"},
	{token: "instagram ", name: "Instagram"},
	{token: "musical_ly", name: "TikTok", vtok: "app_version/"},
	{token: "bytedancewebview", name: "TikTok", vtok: "app_version/"},
	{token: " line/", name: "Line"},
	{token: "gsa/", name: "Google App"},
	{token: "fxios/", name: "Firefox"},
	{token: "firefox/", name: "Firefox"},
	{token: "crios/", name: "Chrome"},
	{token: "; wv)", name: "Android WebView", vtok: "chrome/"},
	{token: "chrome/", name: "Chrome"},
	{token: "safari/", name: "Safari", vtok: "version/"},
}

func detectBrowser(in input) Browser {
	for _, r := range browserRules {
		if !in.contains(r.token) {
			continue
		}
		vtok := r.vtok
		if vtok == "" {
			vtok = r.token
		}
		b := Browser{Name: r.name, Version: in.versionAfter(vtok)}
		if r.name == "Safari" {
			if b.Version.IsZero() {
				continue // bare "Safari/537.36" WebKit product token, not the Safari browser
			}
			if in.contains("iphone") || in.contains("ipad") || in.contains("ipod") {
				b.Name = "Mobile Safari"
			}
		}
		return b
	}
	// WebKit view on an iOS device with no Safari Version/ token → in-app webview.
	if in.contains("applewebkit/") && (in.contains("iphone") || in.contains("ipad")) {
		return Browser{Name: "iOS WebView"}
	}
	return Browser{}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/useragent/ -run 'TestParseBrowser' -v`
Expected: PASS. Then full package: `go test ./web/useragent/` — PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "feat(useragent): ordered browser detection incl. forks, in-app webviews"
```

---

### Task 3: OS detection

**Files:**
- Replace: `web/useragent/os.go`
- Test: `web/useragent/os_test.go`

**Interfaces:**
- Consumes: `input`, `Version` from Task 1.
- Produces: `detectOS(in input) OS` with canonical names: Windows, Windows Phone, macOS, iOS, iPadOS, Android, ChromeOS, Linux, FreeBSD, OpenBSD, NetBSD, KaiOS, Tizen, webOS, HarmonyOS, PlayStation, Xbox, Nintendo Switch, Nintendo. (Task 4's `detectDevice` receives `OS.Name`; Task 1's Parse maps Amazon+Android → Fire OS.)

- [ ] **Step 1: Write the failing test**

`web/useragent/os_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/ -run TestParseOS -v`
Expected: FAIL — stub returns empty OS.

- [ ] **Step 3: Implement OS matching**

Replace `web/useragent/os.go`:

```go
package useragent

// detectOS is ordered: consoles and phone OSes hide inside generic tokens
// (Xbox UAs contain "Windows NT", HarmonyOS UAs contain "Android"), so the
// specific checks run first.
func detectOS(in input) OS {
	switch {
	case in.contains("playstation"):
		return OS{Name: "PlayStation", Version: in.versionAfter("playstation ")}
	case in.contains("xbox"):
		return OS{Name: "Xbox"}
	case in.contains("nintendo switch"):
		return OS{Name: "Nintendo Switch"}
	case in.contains("nintendo"):
		return OS{Name: "Nintendo"}
	case in.contains("windows phone"):
		v := in.versionAfter("windows phone os ")
		if v.IsZero() {
			v = in.versionAfter("windows phone ")
		}
		return OS{Name: "Windows Phone", Version: v}
	case in.contains("windows nt "):
		return windowsOS(in)
	case in.contains("iphone os "):
		return OS{Name: "iOS", Version: in.versionAfter("iphone os ")}
	case in.contains("ipad"):
		return OS{Name: "iPadOS", Version: in.versionAfter("cpu os ")}
	case in.contains("harmonyos"):
		return OS{Name: "HarmonyOS"}
	case in.contains("kaios/"):
		// before android: KaiOS UAs carry an "Android" token too
		return OS{Name: "KaiOS", Version: in.versionAfter("kaios/")}
	case in.contains("android"):
		return OS{Name: "Android", Version: in.versionAfter("android ")}
	case in.contains("tizen"):
		return OS{Name: "Tizen", Version: in.versionAfter("tizen ")}
	case in.contains("web0s"), in.contains("webos"):
		return OS{Name: "webOS"}
	case in.contains("cros "):
		return OS{Name: "ChromeOS"}
	case in.contains("mac os x"):
		if in.contains("mobile/") {
			// Desktop-mode iPad / iPad in-app view sends a Mac UA plus a
			// Mobile/ build token. Best-effort: true desktop-mode Safari on
			// iPad is indistinguishable from a Mac and stays macOS.
			return OS{Name: "iPadOS"}
		}
		return OS{Name: "macOS", Version: in.versionAfter("mac os x ")}
	case in.contains("freebsd"):
		return OS{Name: "FreeBSD"}
	case in.contains("openbsd"):
		return OS{Name: "OpenBSD"}
	case in.contains("netbsd"):
		return OS{Name: "NetBSD"}
	case in.contains("linux"), in.contains("x11;"):
		return OS{Name: "Linux"}
	}
	return OS{}
}

// windowsOS maps NT kernel versions to marketing versions. NT 10.0 is
// reported as "10" — Windows 11 also sends NT 10.0 and is only
// distinguishable via Client Hints (Task 7).
func windowsOS(in input) OS {
	nt := in.versionAfter("windows nt ")
	switch {
	case nt.Major == 10:
		return OS{Name: "Windows", Version: Version{Major: 10, Full: "10"}}
	case nt.Major == 6 && nt.Minor == 3:
		return OS{Name: "Windows", Version: Version{Major: 8, Minor: 1, Full: "8.1"}}
	case nt.Major == 6 && nt.Minor == 2:
		return OS{Name: "Windows", Version: Version{Major: 8, Full: "8"}}
	case nt.Major == 6 && nt.Minor == 1:
		return OS{Name: "Windows", Version: Version{Major: 7, Full: "7"}}
	case nt.Major == 6 && nt.Minor == 0:
		return OS{Name: "Windows", Version: Version{Full: "Vista"}}
	case nt.Major == 5:
		return OS{Name: "Windows", Version: Version{Full: "XP"}}
	}
	return OS{Name: "Windows", Version: nt}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/useragent/ -run TestParseOS -v` then `go test ./web/useragent/`
Expected: PASS (browser tests must still pass).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "feat(useragent): OS detection with marketing-version mapping"
```

---

### Task 4: Device type, brand, and model

**Files:**
- Replace: `web/useragent/device.go`
- Test: `web/useragent/device_test.go`

**Interfaces:**
- Consumes: `input`, `DeviceType` constants, `detectOS` names from Task 3.
- Produces:
  - `detectDevice(in input, osName string) Device`
  - `brandFromModel(model string) string` (reused by Client Hints in Task 7)
  - `hasPrefixFold(s, prefix string) bool`

- [ ] **Step 1: Write the failing test**

`web/useragent/device_test.go`:

```go
package useragent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseDevices(t *testing.T) {
	tests := []struct {
		name  string
		ua    string
		typ   useragent.DeviceType
		brand string
		model string
	}{
		{"windows desktop", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36", useragent.DeviceDesktop, "", ""},
		{"mac desktop", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", useragent.DeviceDesktop, "", ""},
		{"linux desktop", "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0", useragent.DeviceDesktop, "", ""},
		{"iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", useragent.DeviceMobile, "Apple", "iPhone"},
		{"ipad", "Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1", useragent.DeviceTablet, "Apple", "iPad"},
		{"samsung phone with build", "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "Samsung", "SM-S918B"},
		{"samsung tablet no mobile token", "Mozilla/5.0 (Linux; Android 13; SM-X710) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36", useragent.DeviceTablet, "Samsung", "SM-X710"},
		{"pixel", "Mozilla/5.0 (Linux; Android 15; Pixel 8 Build/AP4A.250105.002) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "Google", "Pixel 8"},
		{"xiaomi redmi", "Mozilla/5.0 (Linux; Android 13; Redmi Note 12 Build/TKQ1.221114.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.5993.80 Mobile Safari/537.36", useragent.DeviceMobile, "Xiaomi", "Redmi Note 12"},
		{"oppo cph", "Mozilla/5.0 (Linux; Android 14; CPH2451 Build/UKQ1.230924.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "OPPO", "CPH2451"},
		{"realme rmx", "Mozilla/5.0 (Linux; U; Android 12; en-US; RMX3085 Build/SP1A.210812.016) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.58 UCBrowser/13.4.0.1306 Mobile Safari/537.36", useragent.DeviceMobile, "realme", "RMX3085"},
		{"reduced ua frozen model K", "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "", ""},
		{"legacy locale segment is not a model", "Mozilla/5.0 (Linux; U; Android 2.3.4; en-us; Sprint APA7373KT) AppleWebKit/533.1 (KHTML, like Gecko) Version/4.0 Mobile Safari/533.1", useragent.DeviceMobile, "", ""},
		{"opera mini is mobile", "Opera/9.80 (Android; Opera Mini/12.0.1987/37.7327; U; en) Presto/2.12.423 Version/12.16", useragent.DeviceMobile, "", ""},
		{"windows phone lumia", "Mozilla/5.0 (Windows Phone 8.1; ARM; Trident/7.0; Touch; rv:11.0; IEMobile/11.0; NOKIA; Lumia 640) like Gecko", useragent.DeviceMobile, "Nokia", ""},
		{"fire tablet", "Mozilla/5.0 (Linux; Android 9; KFMAWI Build/PS7312.3038N) AppleWebKit/537.36 (KHTML, like Gecko) Silk/126.2.7 like Chrome/126.0.6478.71 Safari/537.36", useragent.DeviceTablet, "Amazon", "KFMAWI"},
		{"fire tv", "Mozilla/5.0 (Linux; Android 9; AFTKA Build/PS7285.2877N) AppleWebKit/537.36 (KHTML, like Gecko) Silk/126.2.7 like Chrome/126.0.6478.71 Safari/537.36", useragent.DeviceTV, "Amazon", "AFTKA"},
		{"android tv bravia", "Mozilla/5.0 (Linux; Android 12; BRAVIA 4K VH2 Build/SOF1.231005.007) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36", useragent.DeviceTV, "Sony", "BRAVIA 4K VH2"},
		{"tizen smart tv", "Mozilla/5.0 (SMART-TV; Linux; Tizen 6.0) AppleWebKit/537.36 (KHTML, like Gecko) 76.0.3809.146/6.0 TV Safari/537.36", useragent.DeviceTV, "", ""},
		{"webos tv", "Mozilla/5.0 (Web0S; Linux/SmartTV) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36 WebAppManager", useragent.DeviceTV, "", ""},
		{"chromecast", "Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 CrKey/1.56.500000", useragent.DeviceTV, "Google", "Chromecast"},
		{"roku", "Roku/DVP-12.0 (12.0.0.4182-88)", useragent.DeviceTV, "Roku", ""},
		{"apple tv", "AppleTV11,1/11.1", useragent.DeviceTV, "Apple", "Apple TV"},
		{"playstation 5", "Mozilla/5.0 (PlayStation; PlayStation 5/2.26) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0 Safari/605.1.15", useragent.DeviceConsole, "Sony", "PlayStation 5"},
		{"xbox", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; Xbox; Xbox One) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/48.0.2564.82 Safari/537.36 Edge/20.02", useragent.DeviceConsole, "Microsoft", "Xbox"},
		{"nintendo switch", "Mozilla/5.0 (Nintendo Switch; WifiWebAuthApplet) AppleWebKit/609.4 (KHTML, like Gecko) NF/6.0.2.22.4 NintendoBrowser/5.1.0.22433", useragent.DeviceConsole, "Nintendo", "Switch"},
		{"apple watch", "Mozilla/5.0 (Apple Watch; CPU WatchOS 10_5 like Mac OS X) AppleWebKit/605.1.15", useragent.DeviceWearable, "Apple", "Apple Watch"},
		{"galaxy watch", "Mozilla/5.0 (Linux; Android 13; Galaxy Watch6 Build/TWQ1.230512.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Mobile Safari/537.36", useragent.DeviceWearable, "Samsung", "Galaxy Watch6"},
		{"kindle ereader", "Mozilla/5.0 (X11; U; Linux armv7l like Android; en-us) AppleWebKit/531.2+ (KHTML, like Gecko) Version/5.0 Safari/533.2+ Kindle/3.0+", useragent.DeviceEReader, "Amazon", "Kindle"},
		{"kobo ereader", "Mozilla/5.0 (Linux; U; Android 2.0; en-us;) AppleWebKit/538.1 (KHTML, like Gecko) Kobo Touch/4.38.23171", useragent.DeviceEReader, "Kobo", ""},
		{"cubot phone is not a bot", "Mozilla/5.0 (Linux; Android 13; Cubot KingKong 9 Build/TP1A.220624.014) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "", "Cubot KingKong 9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := useragent.Parse(tt.ua)
			assert.Equal(t, tt.typ, got.Device.Type, "type")
			assert.Equal(t, tt.brand, got.Device.Brand, "brand")
			assert.Equal(t, tt.model, got.Device.Model, "model")
		})
	}
}

func TestFireDeviceReportsFireOS(t *testing.T) {
	got := useragent.Parse("Mozilla/5.0 (Linux; Android 9; KFMAWI Build/PS7312.3038N) AppleWebKit/537.36 (KHTML, like Gecko) Silk/126.2.7 like Chrome/126.0.6478.71 Safari/537.36")
	assert.Equal(t, "Fire OS", got.OS.Name)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/ -run 'TestParseDevices|TestFireDevice' -v`
Expected: FAIL — stub returns DeviceUnknown.

- [ ] **Step 3: Implement device detection**

Replace `web/useragent/device.go`:

```go
package useragent

import "strings"

var desktopOS = map[string]bool{
	"Windows": true, "macOS": true, "Linux": true, "ChromeOS": true,
	"FreeBSD": true, "OpenBSD": true, "NetBSD": true,
}

// detectDevice is ordered most-specific-first: consoles and TVs before the
// mobile/tablet split, desktop as the OS-derived fallback.
func detectDevice(in input, osName string) Device {
	model, brand := deviceModel(in)
	switch {
	case in.contains("playstation 5"):
		return Device{Type: DeviceConsole, Brand: "Sony", Model: "PlayStation 5"}
	case in.contains("playstation 4"):
		return Device{Type: DeviceConsole, Brand: "Sony", Model: "PlayStation 4"}
	case in.contains("playstation"):
		return Device{Type: DeviceConsole, Brand: "Sony", Model: "PlayStation"}
	case in.contains("xbox"):
		return Device{Type: DeviceConsole, Brand: "Microsoft", Model: "Xbox"}
	case in.contains("nintendo switch"):
		return Device{Type: DeviceConsole, Brand: "Nintendo", Model: "Switch"}
	case in.contains("nintendo"):
		return Device{Type: DeviceConsole, Brand: "Nintendo"}
	case in.contains("appletv"), in.contains("apple tv"):
		return Device{Type: DeviceTV, Brand: "Apple", Model: "Apple TV"}
	case in.contains("crkey"):
		return Device{Type: DeviceTV, Brand: "Google", Model: "Chromecast"}
	case in.contains("roku"):
		return Device{Type: DeviceTV, Brand: "Roku"}
	case in.contains("bravia"):
		return Device{Type: DeviceTV, Brand: "Sony", Model: model}
	case hasPrefixFold(model, "aft"):
		return Device{Type: DeviceTV, Brand: "Amazon", Model: model}
	case in.contains("android tv"), in.contains("googletv"), in.contains("smart-tv"),
		in.contains("smarttv"), in.contains("hbbtv"), in.contains("netcast"),
		osName == "webOS", osName == "Tizen" && in.contains("tv"):
		return Device{Type: DeviceTV, Brand: brand, Model: model}
	case in.contains("watchos"), in.contains("apple watch"):
		return Device{Type: DeviceWearable, Brand: "Apple", Model: "Apple Watch"}
	case in.contains("watch"):
		return Device{Type: DeviceWearable, Brand: brand, Model: model}
	case in.contains("kobo"):
		return Device{Type: DeviceEReader, Brand: "Kobo"}
	case in.contains("kindle"):
		return Device{Type: DeviceEReader, Brand: "Amazon", Model: "Kindle"}
	case in.contains("ipad"):
		return Device{Type: DeviceTablet, Brand: "Apple", Model: "iPad"}
	case in.contains("iphone"):
		return Device{Type: DeviceMobile, Brand: "Apple", Model: "iPhone"}
	case in.contains("ipod"):
		return Device{Type: DeviceMobile, Brand: "Apple", Model: "iPod"}
	case osName == "iPadOS":
		return Device{Type: DeviceTablet, Brand: "Apple", Model: "iPad"}
	case osName == "Android":
		if hasPrefixFold(model, "kf") {
			return Device{Type: DeviceTablet, Brand: "Amazon", Model: model}
		}
		if in.contains("mobile") {
			return Device{Type: DeviceMobile, Brand: brand, Model: model}
		}
		return Device{Type: DeviceTablet, Brand: brand, Model: model}
	case in.contains("opera mini"), in.contains("windows phone"), in.contains("mobile"):
		return Device{Type: DeviceMobile, Brand: brand, Model: model}
	case in.contains("tablet"):
		return Device{Type: DeviceTablet, Brand: brand, Model: model}
	case desktopOS[osName]:
		return Device{Type: DeviceDesktop}
	}
	return Device{Type: DeviceUnknown}
}

// deviceModel extracts the Android-style device segment with original
// casing: "(Linux; Android 14; SM-S918B Build/UP1A)" → "SM-S918B".
// The frozen reduced-UA placeholder "K" maps to no model.
func deviceModel(in input) (model, brand string) {
	var seg string
	if end := in.index(" build/"); end >= 0 {
		start := end
		for start > 0 && in.lower[start-1] != ';' && in.lower[start-1] != '(' {
			start--
		}
		seg = strings.TrimSpace(in.raw[start:end])
	} else if i := in.index("android "); i >= 0 {
		// reduced UA without Build/: model is the next comment segment,
		// e.g. "(Linux; Android 13; SM-X710)"
		j := i
		for j < len(in.lower) && in.lower[j] != ';' && in.lower[j] != ')' {
			j++
		}
		if j < len(in.lower) && in.lower[j] == ';' {
			k := j + 1
			for k < len(in.lower) && in.lower[k] != ';' && in.lower[k] != ')' {
				k++
			}
			seg = strings.TrimSpace(in.raw[j+1 : k])
		}
	}
	if seg == "" || seg == "K" || seg == "k" || isLocale(seg) {
		return "", brandToken(in)
	}
	if b := brandFromModel(seg); b != "" {
		return seg, b
	}
	return seg, brandToken(in)
}

// isLocale filters legacy "(...; en-us; Model)" segments that would
// otherwise be mistaken for a model in the reduced-UA fallback.
func isLocale(s string) bool {
	if len(s) != 2 && len(s) != 5 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c == '-') {
			return false
		}
	}
	return len(s) == 2 || s[2] == '-'
}

var modelBrandPrefixes = []struct{ prefix, brand string }{
	{"sm-", "Samsung"}, {"gt-", "Samsung"}, {"galaxy", "Samsung"},
	{"pixel", "Google"}, {"nexus", "Google"},
	{"redmi", "Xiaomi"}, {"poco", "Xiaomi"}, {"mi ", "Xiaomi"},
	{"kf", "Amazon"}, {"aft", "Amazon"},
	{"moto", "Motorola"}, {"xperia", "Sony"},
	{"nokia", "Nokia"}, {"lumia", "Nokia"},
	{"lm-", "LG"}, {"lg-", "LG"},
	{"lenovo", "Lenovo"}, {"tb-", "Lenovo"}, {"tcl", "TCL"},
	{"cph", "OPPO"}, {"rmx", "realme"}, {"oneplus", "OnePlus"},
	{"vivo", "vivo"}, {"huawei", "Huawei"}, {"honor", "Honor"},
}

// brandFromModel infers the vendor from well-known model prefixes.
// Shared with the Client Hints path (Sec-CH-UA-Model).
func brandFromModel(model string) string {
	for _, p := range modelBrandPrefixes {
		if hasPrefixFold(model, p.prefix) {
			return p.brand
		}
	}
	return ""
}

var uaBrandTokens = []struct{ token, brand string }{
	{"huawei", "Huawei"}, {"honor", "Honor"}, {"xiaomi", "Xiaomi"},
	{"oneplus", "OnePlus"}, {"oppo", "OPPO"}, {"vivo", "vivo"},
	{"samsung", "Samsung"}, {"nokia", "Nokia"},
}

func brandToken(in input) string {
	for _, t := range uaBrandTokens {
		if in.contains(t.token) {
			return t.brand
		}
	}
	return ""
}

// hasPrefixFold is an ASCII case-insensitive strings.HasPrefix.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := range len(prefix) {
		a, b := s[i], prefix[i]
		if a >= 'A' && a <= 'Z' {
			a |= 0x20
		}
		if b >= 'A' && b <= 'Z' {
			b |= 0x20
		}
		if a != b {
			return false
		}
	}
	return true
}
```

Note: the "cubot" fixture passes with empty brand (Cubot is deliberately not in the brand tables — it exists in the corpus to guard Task 5's bot heuristic). The "legacy locale" fixture expects empty model because the segment after the Android version is `en-us`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/useragent/ -run 'TestParseDevices|TestFireDevice' -v` then `go test ./web/useragent/`
Expected: PASS, including earlier tasks' tests.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "feat(useragent): device type, brand and model detection"
```

---

### Task 5: Bot detection — triggers, curated table, generic heuristics

**Files:**
- Replace: `web/useragent/bot.go`
- Create: `web/useragent/export_test.go`
- Test: `web/useragent/bot_test.go`

**Interfaces:**
- Consumes: `input`, `Bot`, `BotCategory` constants, `generatedBotPatterns` placeholder.
- Produces:
  - `detectBot(in input) (Bot, bool)`
  - `var botTriggers []string` — the cheap gate; EVERY curated token and (Task 6) every generated pattern must contain at least one trigger, or it is unreachable. Invariant tests enforce this.
  - `botNameFromPattern(p string) string` (used for generated matches)
  - export_test shims: `CuratedBots`, `BotTriggers`, `GeneratedBotPatterns`, `type BotEntry = botEntry` (exported fields).

- [ ] **Step 1: Write the failing test**

`web/useragent/export_test.go`:

```go
package useragent

// Test-only shims: invariant tests assert internal table state (the one
// permitted white-box use). Behavior tests stay black-box in useragent_test.
type BotEntry = botEntry

var (
	CuratedBots          = curatedBots
	BotTriggers          = botTriggers
	GeneratedBotPatterns = generatedBotPatterns
)
```

`web/useragent/bot_test.go`:

```go
package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseBots(t *testing.T) {
	tests := []struct {
		name     string
		ua       string
		botName  string
		category useragent.BotCategory
		operator string
	}{
		{"googlebot short-circuits chrome", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/126.0.0.0 Safari/537.36", "Googlebot", useragent.BotSearchEngine, "Google"},
		{"gptbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.2; +https://openai.com/gptbot", "GPTBot", useragent.BotAICrawler, "OpenAI"},
		{"claudebot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)", "ClaudeBot", useragent.BotAICrawler, "Anthropic"},
		{"chatgpt agent", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot", "ChatGPT-User", useragent.BotAIAgent, "OpenAI"},
		{"slack preview", "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", "Slackbot", useragent.BotSocialPreview, "Slack"},
		{"whatsapp preview", "WhatsApp/2.23.24.76 A", "WhatsApp Preview", useragent.BotSocialPreview, "Meta"},
		{"uptimerobot", "Mozilla/5.0+(compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)", "UptimeRobot", useragent.BotMonitoring, "UptimeRobot"},
		{"ahrefs", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", "AhrefsBot", useragent.BotSEO, "Ahrefs"},
		{"adsbot", "AdsBot-Google (+http://www.google.com/adsbot.html)", "AdsBot", useragent.BotAdvertising, "Google"},
		{"censys", "Mozilla/5.0 (compatible; CensysInspect/1.1; +https://about.censys.io/)", "CensysInspect", useragent.BotSecurity, "Censys"},
		{"stripe webhook", "Stripe/1.0 (+https://stripe.com/docs/webhooks)", "Stripe Webhook", useragent.BotWebhook, "Stripe"},
		{"feedly", "Feedly/1.0 (+http://www.feedly.com/fetcher.html; 16 subscribers)", "Feedly", useragent.BotFeedFetcher, "Feedly"},
		{"scrapy", "Scrapy/2.11.2 (+https://scrapy.org)", "Scrapy", useragent.BotScraper, ""},
		{"curl", "curl/8.6.0", "curl", useragent.BotLibrary, ""},
		{"python requests", "python-requests/2.31.0", "python-requests", useragent.BotLibrary, ""},
		{"go http client", "Go-http-client/2.0", "Go-http-client", useragent.BotLibrary, ""},
		{"generic via url", "Zzqx/1.0 (+https://zzqx.invalid/info)", "", useragent.BotUnknown, ""},
		{"generic via bot suffix", "Zzqxbot/1.0", "", useragent.BotUnknown, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := useragent.Parse(tt.ua)
			require.True(t, got.IsBot(), "must be a bot")
			assert.Equal(t, useragent.DeviceBot, got.Device.Type)
			assert.Equal(t, tt.botName, got.Bot.Name)
			assert.Equal(t, tt.category, got.Bot.Category)
			assert.Equal(t, tt.operator, got.Bot.Operator)
			assert.Empty(t, got.Browser.Name, "browser stays zero for bots")
			assert.Empty(t, got.OS.Name, "os stays zero for bots")
		})
	}
}

func TestNotBots(t *testing.T) {
	for name, ua := range map[string]string{
		"chrome":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
		"cubot phone":    "Mozilla/5.0 (Linux; Android 13; Cubot KingKong 9 Build/TP1A.220624.014) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36",
		"empty":          "",
		"abbott not bot": "Mozilla/5.0 (compatible; Abbott Healthcare App) AppleWebKit/537.36",
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, useragent.Parse(ua).IsBot())
		})
	}
}

// Every curated entry must be detectable and correctly attributed via a
// synthetic UA containing its token.
func TestCuratedBotsAllDetected(t *testing.T) {
	require.NotEmpty(t, useragent.CuratedBots)
	for _, e := range useragent.CuratedBots {
		ua := "Mozilla/5.0 (compatible) " + e.Token + "1.0"
		got := useragent.Parse(ua)
		require.True(t, got.IsBot(), "token %q", e.Token)
		assert.Equal(t, e.Name, got.Bot.Name, "token %q", e.Token)
		assert.Equal(t, e.Category, got.Bot.Category, "token %q", e.Token)
		assert.Equal(t, e.Operator, got.Bot.Operator, "token %q", e.Token)
	}
}

// The trigger gate must make every curated token reachable.
func TestCuratedBotsReachableViaTriggers(t *testing.T) {
	for _, e := range useragent.CuratedBots {
		reachable := false
		for _, trig := range useragent.BotTriggers {
			if strings.Contains(e.Token, trig) {
				reachable = true
				break
			}
		}
		assert.True(t, reachable, "curated token %q contains no trigger — detectBot can never reach it", e.Token)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/ -run 'TestParseBots|TestNotBots|TestCurated' -v`
Expected: FAIL — compile error (`botEntry`, `curatedBots`, `botTriggers` undefined).

- [ ] **Step 3: Implement bot detection**

Replace `web/useragent/bot.go`:

```go
package useragent

import "strings"

// botTriggers is a cheap gate: the full bot tables are scanned only when the
// UA contains one of these substrings. Every curated token and every
// generated pattern MUST contain at least one trigger (invariant-tested and
// enforced by the generator), otherwise the entry is unreachable.
var botTriggers = []string{
	"bot", "crawl", "spider", "scan", "search", "slurp", "monitor",
	"archive", "fetch", "http", "index", "feed", "agent", "headless",
	"preview", "validator", "python", "java", "curl", "wget", "node",
	"axios", "okhttp", "go-http", "scrapy", "phantom", "gpt", "claude",
	"perplexity", "whatsapp", "facebookexternalhit", "pingdom",
	"statuscake", "censys", "expanse", "mediapartners", "stripe/",
	"paypal", "libwww",
}

// botEntry fields are exported so export_test.go can re-export the table
// for invariant tests; the type itself stays unexported.
type botEntry struct {
	Token    string // lowercase substring
	Name     string
	Category BotCategory
	Operator string
}

var curatedBots = []botEntry{
	// search engines
	{"googlebot", "Googlebot", BotSearchEngine, "Google"},
	{"bingbot", "Bingbot", BotSearchEngine, "Microsoft"},
	{"bingpreview", "BingPreview", BotSearchEngine, "Microsoft"},
	{"duckduckbot", "DuckDuckBot", BotSearchEngine, "DuckDuckGo"},
	{"baiduspider", "Baiduspider", BotSearchEngine, "Baidu"},
	{"yandexbot", "YandexBot", BotSearchEngine, "Yandex"},
	{"applebot", "Applebot", BotSearchEngine, "Apple"},
	{"slurp", "Yahoo Slurp", BotSearchEngine, "Yahoo"},
	// AI crawlers
	{"gptbot", "GPTBot", BotAICrawler, "OpenAI"},
	{"oai-searchbot", "OAI-SearchBot", BotAICrawler, "OpenAI"},
	{"claudebot", "ClaudeBot", BotAICrawler, "Anthropic"},
	{"perplexitybot", "PerplexityBot", BotAICrawler, "Perplexity"},
	{"bytespider", "Bytespider", BotAICrawler, "ByteDance"},
	{"ccbot", "CCBot", BotAICrawler, "Common Crawl"},
	{"meta-externalagent", "Meta-ExternalAgent", BotAICrawler, "Meta"},
	// AI agents (user-triggered fetches)
	{"chatgpt-user", "ChatGPT-User", BotAIAgent, "OpenAI"},
	{"claude-user", "Claude-User", BotAIAgent, "Anthropic"},
	{"perplexity-user", "Perplexity-User", BotAIAgent, "Perplexity"},
	// social link previews
	{"facebookexternalhit", "Facebook Preview", BotSocialPreview, "Meta"},
	{"twitterbot", "Twitterbot", BotSocialPreview, "X"},
	{"slackbot", "Slackbot", BotSocialPreview, "Slack"},
	{"whatsapp", "WhatsApp Preview", BotSocialPreview, "Meta"},
	{"discordbot", "Discordbot", BotSocialPreview, "Discord"},
	{"telegrambot", "TelegramBot", BotSocialPreview, "Telegram"},
	{"linkedinbot", "LinkedInBot", BotSocialPreview, "LinkedIn"},
	// monitoring
	{"uptimerobot", "UptimeRobot", BotMonitoring, "UptimeRobot"},
	{"pingdom", "Pingdom", BotMonitoring, "Pingdom"},
	{"statuscake", "StatusCake", BotMonitoring, "StatusCake"},
	// SEO
	{"ahrefsbot", "AhrefsBot", BotSEO, "Ahrefs"},
	{"semrushbot", "SemrushBot", BotSEO, "Semrush"},
	{"mj12bot", "MJ12bot", BotSEO, "Majestic"},
	{"dotbot", "DotBot", BotSEO, "Moz"},
	{"screaming frog seo spider", "Screaming Frog", BotSEO, "Screaming Frog"},
	// advertising
	{"adsbot-google", "AdsBot", BotAdvertising, "Google"},
	{"mediapartners-google", "Mediapartners", BotAdvertising, "Google"},
	{"adidxbot", "AdIdxBot", BotAdvertising, "Microsoft"},
	// security scanners
	{"censysinspect", "CensysInspect", BotSecurity, "Censys"},
	{"expanse", "Expanse", BotSecurity, "Palo Alto Networks"},
	// webhooks
	{"stripe/", "Stripe Webhook", BotWebhook, "Stripe"},
	{"paypal ipn", "PayPal IPN", BotWebhook, "PayPal"},
	// feed fetchers
	{"feedfetcher", "FeedFetcher", BotFeedFetcher, "Google"},
	{"feedly", "Feedly", BotFeedFetcher, "Feedly"},
	// scrapers
	{"scrapy", "Scrapy", BotScraper, ""},
	{"httrack", "HTTrack", BotScraper, ""},
	// HTTP libraries
	{"curl/", "curl", BotLibrary, ""},
	{"wget/", "Wget", BotLibrary, ""},
	{"python-requests", "python-requests", BotLibrary, ""},
	{"python-urllib", "python-urllib", BotLibrary, ""},
	{"python-httpx", "python-httpx", BotLibrary, ""},
	{"go-http-client", "Go-http-client", BotLibrary, ""},
	{"okhttp", "okhttp", BotLibrary, ""},
	{"java/", "Java", BotLibrary, ""},
	{"axios/", "axios", BotLibrary, ""},
	{"node-fetch", "node-fetch", BotLibrary, ""},
	{"libwww-perl", "libwww-perl", BotLibrary, ""},
	{"guzzlehttp", "Guzzle", BotLibrary, ""},
}

// notBotTokens are device names that end in "bot" and would otherwise trip
// the generic suffix heuristic (Cubot phones).
var notBotTokens = []string{"cubot"}

// detectBot layers: trigger gate → curated table → generated table →
// generic heuristics. Curated wins over generated on conflict because it is
// checked first.
func detectBot(in input) (Bot, bool) {
	triggered := false
	for _, t := range botTriggers {
		if in.contains(t) {
			triggered = true
			break
		}
	}
	if !triggered {
		return Bot{}, false
	}
	for _, e := range curatedBots {
		if in.contains(e.Token) {
			return Bot{Name: e.Name, Category: e.Category, Operator: e.Operator}, true
		}
	}
	for _, p := range generatedBotPatterns {
		if in.contains(p) {
			return Bot{Name: botNameFromPattern(p), Category: BotUnknown}, true
		}
	}
	if genericBot(in) {
		return Bot{Category: BotUnknown}, true
	}
	return Bot{}, false
}

func genericBot(in input) bool {
	if in.contains("+http://") || in.contains("+https://") {
		return true
	}
	if in.contains("crawler") || in.contains("spider") || in.contains("scanner") {
		return true
	}
	if hasBotSuffix(in.lower) {
		for _, nb := range notBotTokens {
			if in.contains(nb) {
				return false
			}
		}
		return true
	}
	return false
}

// hasBotSuffix reports a "bot" occurrence followed by a non-letter or end of
// string ("Googlebot/", "Zzqxbot") without matching mid-word hits
// ("Abbott" → followed by 't').
func hasBotSuffix(b []byte) bool {
	for i := 0; i+3 <= len(b); i++ {
		if b[i] != 'b' || b[i+1] != 'o' || b[i+2] != 't' {
			continue
		}
		if i+3 == len(b) {
			return true
		}
		if c := b[i+3]; c < 'a' || c > 'z' {
			return true
		}
	}
	return false
}

// botNameFromPattern turns a generated dataset pattern into a display name.
func botNameFromPattern(p string) string {
	return strings.Trim(p, "/-. ")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./web/useragent/ -v`
Expected: ALL PASS — including Task 4's cubot fixture and Task 2/3 corpus (bot short-circuit must not misfire on browser UAs).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "feat(useragent): three-layer bot detection with trigger gate and curated taxonomy"
```

---

### Task 6: Bot table generator (`gen/`) + generated table

**Files:**
- Create: `web/useragent/gen/main.go`
- Create: `web/useragent/gen/main_test.go`
- Create: `web/useragent/gen/testdata/crawler-user-agents.json`
- Replace: `web/useragent/bot_generated.go` (by running the generator)
- Test: `web/useragent/generated_test.go`

**Interfaces:**
- Consumes: trigger semantics from Task 5 (gen keeps its own copy of the trigger list; drift is caught by `TestGeneratedPatternsReachable`).
- Produces: populated `var generatedBotPatterns = []string{...}` in `bot_generated.go`.

- [ ] **Step 1: Write the failing generator test**

`web/useragent/gen/main_test.go`:

```go
package main

import (
	"os"
	"strings"
	"testing"
)

func TestProcessPatterns(t *testing.T) {
	data, err := os.ReadFile("testdata/crawler-user-agents.json")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := decode(data)
	if err != nil {
		t.Fatal(err)
	}
	got := processPatterns(entries)

	want := []string{"headlesschrome", "netcraftsurveyagent", "petalbot", "sogou web spider"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	// sorted, lowercase, deduped, no regex leftovers
	for _, p := range got {
		if p != strings.ToLower(p) || strings.ContainsAny(p, `\^$|?*+()[]{}`) {
			t.Fatalf("malformed pattern %q", p)
		}
	}
}

func TestRender(t *testing.T) {
	out := string(render([]string{"petalbot"}, "test-src"))
	for _, want := range []string{
		"// Code generated by gen; DO NOT EDIT.",
		"// Source: test-src",
		"package useragent",
		"var generatedBotPatterns = []string{",
		"\t\"petalbot\",",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render output missing %q:\n%s", want, out)
		}
	}
}
```

`web/useragent/gen/testdata/crawler-user-agents.json` (exercises: regex de-escape, regex skip, short skip, trigger-less skip, dedupe, case folding):

```json
[
  {"pattern": "PetalBot", "url": "https://webmaster.petalsearch.com/site/petalbot"},
  {"pattern": "petalbot", "url": ""},
  {"pattern": "Sogou\\ web\\ spider", "url": "http://www.sogou.com/docs/help/webmasters.htm"},
  {"pattern": "HeadlessChrome", "instances": ["Mozilla/5.0 HeadlessChrome/126.0.0.0"]},
  {"pattern": "NetcraftSurveyAgent", "url": ""},
  {"pattern": "^Mozilla.*compatible$", "url": ""},
  {"pattern": "grub", "url": ""},
  {"pattern": "dataprovider", "url": ""}
]
```

(`grub` is skipped for length < 5, `dataprovider` for containing no trigger, `^Mozilla.*compatible$` for regex metacharacters, `petalbot` deduped with `PetalBot`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/gen/ -v`
Expected: FAIL — `decode`, `processPatterns`, `render` undefined.

- [ ] **Step 3: Implement the generator**

`web/useragent/gen/main.go`:

```go
// Command gen compiles the crawler-user-agents dataset into
// bot_generated.go. Regeneration is a manual, reviewed action — never a
// build-time network dependency:
//
//	go run ./web/useragent/gen -out web/useragent/bot_generated.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
)

// defaultSrc pins the dataset to a reviewed commit. To bump: pick a new
// commit of https://github.com/monperrus/crawler-user-agents, replace the
// hash, rerun, and review the table diff.
const defaultSrc = "https://raw.githubusercontent.com/monperrus/crawler-user-agents/PINNED_COMMIT/crawler-user-agents.json"

// triggers mirrors botTriggers in web/useragent/bot.go. Patterns containing
// none of these are skipped: the runtime trigger gate would never reach
// them. Drift between the two lists is caught by
// TestGeneratedPatternsReachable in the parent package.
var triggers = []string{
	"bot", "crawl", "spider", "scan", "search", "slurp", "monitor",
	"archive", "fetch", "http", "index", "feed", "agent", "headless",
	"preview", "validator", "python", "java", "curl", "wget", "node",
	"axios", "okhttp", "go-http", "scrapy", "phantom", "gpt", "claude",
	"perplexity", "whatsapp", "facebookexternalhit", "pingdom",
	"statuscake", "censys", "expanse", "mediapartners", "stripe/",
	"paypal", "libwww",
}

type entry struct {
	Pattern string `json:"pattern"`
}

func main() {
	src := flag.String("src", defaultSrc, "dataset URL or local file path")
	out := flag.String("out", "bot_generated.go", "output file")
	flag.Parse()

	data, err := load(*src)
	if err != nil {
		log.Fatalf("load dataset: %v", err)
	}
	entries, err := decode(data)
	if err != nil {
		log.Fatalf("decode dataset: %v", err)
	}
	patterns := processPatterns(entries)
	if err := os.WriteFile(*out, render(patterns, *src), 0o644); err != nil {
		log.Fatalf("write %s: %v", *out, err)
	}
	log.Printf("wrote %d patterns (of %d dataset entries) to %s", len(patterns), len(entries), *out)
}

func load(src string) ([]byte, error) {
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return os.ReadFile(src)
	}
	resp, err := http.Get(src)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", src, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func decode(data []byte) ([]entry, error) {
	var entries []entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// processPatterns lowercases, de-escapes literal regex escapes, and keeps
// only plain-substring patterns that the runtime trigger gate can reach.
// Skipped patterns are a deliberate, logged coverage trade-off.
func processPatterns(entries []entry) []string {
	seen := make(map[string]bool, len(entries))
	var out []string
	skipped := 0
	for _, e := range entries {
		p := strings.ToLower(e.Pattern)
		for _, esc := range [...][2]string{{`\/`, "/"}, {`\.`, "."}, {`\-`, "-"}, {`\ `, " "}} {
			p = strings.ReplaceAll(p, esc[0], esc[1])
		}
		if len(p) < 5 || strings.ContainsAny(p, `\^$|?*+()[]{}`) || !containsTrigger(p) {
			skipped++
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	log.Printf("skipped %d patterns (regex-shaped, too short, or trigger-unreachable)", skipped)
	return out
}

func containsTrigger(p string) bool {
	for _, t := range triggers {
		if strings.Contains(p, t) {
			return true
		}
	}
	return false
}

func render(patterns []string, src string) []byte {
	var b strings.Builder
	b.WriteString("// Code generated by gen; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s\n", src)
	fmt.Fprintf(&b, "// Patterns: %d\n\n", len(patterns))
	b.WriteString("package useragent\n\nvar generatedBotPatterns = []string{\n")
	for _, p := range patterns {
		fmt.Fprintf(&b, "\t%q,\n", p)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}
```

- [ ] **Step 4: Run generator tests**

Run: `go test ./web/useragent/gen/ -v`
Expected: PASS.

- [ ] **Step 5: Pin the dataset commit and generate the real table**

```bash
COMMIT=$(gh api repos/monperrus/crawler-user-agents/commits/HEAD --jq .sha)
# substitute into defaultSrc in web/useragent/gen/main.go (replace PINNED_COMMIT with $COMMIT)
go run ./web/useragent/gen -src "https://raw.githubusercontent.com/monperrus/crawler-user-agents/$COMMIT/crawler-user-agents.json" -out web/useragent/bot_generated.go
```

Expected: log line `wrote N patterns ...` with N in the hundreds; `bot_generated.go` header carries the pinned URL. If the environment has no network access, STOP and report a blocker — do not fabricate the table.

- [ ] **Step 6: Write invariant tests over the generated table**

`web/useragent/generated_test.go`:

```go
package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestGeneratedPatternsWellFormed(t *testing.T) {
	require.NotEmpty(t, useragent.GeneratedBotPatterns, "run go run ./web/useragent/gen first")
	prev := ""
	for _, p := range useragent.GeneratedBotPatterns {
		assert.Equal(t, strings.ToLower(p), p, "patterns must be lowercase")
		assert.GreaterOrEqual(t, len(p), 5, "pattern %q too short", p)
		assert.False(t, strings.ContainsAny(p, `\^$|?*+()[]{}`), "regex leftovers in %q", p)
		assert.Less(t, prev, p, "table must be sorted and deduped")
		prev = p
	}
}

// Every generated pattern must be reachable through the runtime trigger
// gate — this catches drift between gen's trigger copy and bot.go's.
func TestGeneratedPatternsReachable(t *testing.T) {
	for _, p := range useragent.GeneratedBotPatterns {
		reachable := false
		for _, trig := range useragent.BotTriggers {
			if strings.Contains(p, trig) {
				reachable = true
				break
			}
		}
		assert.True(t, reachable, "generated pattern %q contains no bot.go trigger", p)
	}
}

// End-to-end: a UA embedding any generated pattern parses as a bot.
func TestGeneratedPatternsDetected(t *testing.T) {
	for _, p := range useragent.GeneratedBotPatterns {
		got := useragent.Parse("Mozilla/5.0 (compatible) " + p)
		assert.True(t, got.IsBot(), "pattern %q", p)
	}
}

// Curated attribution wins over the generated table.
func TestCuratedPrecedesGenerated(t *testing.T) {
	got := useragent.Parse("Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Equal(t, "Googlebot", got.Bot.Name)
	assert.Equal(t, useragent.BotSearchEngine, got.Bot.Category)
}
```

- [ ] **Step 7: Run all tests**

Run: `go test ./web/useragent/... -v`
Expected: PASS. If `TestGeneratedPatternsDetected` fails on a specific pattern, that pattern collides with a curated entry returning a different name — that is fine only if IsBot() is still true; the test asserts IsBot only, so investigate real failures (they indicate the trigger-skip logic and bot.go disagree).

- [ ] **Step 8: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "feat(useragent): bot table generator with pinned crawler-user-agents dataset"
```

---

### Task 7: Client Hints — ParseHeaders / ParseRequest

**Files:**
- Create: `web/useragent/clienthints.go`
- Test: `web/useragent/clienthints_test.go`

**Interfaces:**
- Consumes: `Parse`, `parseVersion`, `brandFromModel` (Task 4), types from Task 1.
- Produces (public API): `ParseHeaders(h http.Header) UserAgent`, `ParseRequest(r *http.Request) UserAgent`.

- [ ] **Step 1: Write the failing test**

`web/useragent/clienthints_test.go`:

```go
package useragent_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

const frozenChromeWin = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

func headers(ua string, kv map[string]string) http.Header {
	h := http.Header{}
	h.Set("User-Agent", ua)
	for k, v := range kv {
		h.Set(k, v)
	}
	return h
}

func TestClientHintsBrave(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA": `"Brave";v="138", "Chromium";v="138", "Not?A_Brand";v="99"`,
	}))
	assert.Equal(t, "Brave", got.Browser.Name)
	assert.Equal(t, 138, got.Browser.Version.Major)
}

func TestClientHintsEdgeAndArc(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA": `"Microsoft Edge";v="138", "Chromium";v="138", "Not/A)Brand";v="24"`,
	}))
	assert.Equal(t, "Edge", got.Browser.Name)

	got = useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA": `"Arc";v="1", "Chromium";v="138", "Not A;Brand";v="99"`,
	}))
	assert.Equal(t, "Arc", got.Browser.Name)
}

func TestClientHintsFullVersionUnfreezes(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Full-Version-List": `"Chromium";v="138.0.7204.97", "Google Chrome";v="138.0.7204.97", "Not/A)Brand";v="99.0.0.0"`,
	}))
	assert.Equal(t, "Chrome", got.Browser.Name)
	assert.Equal(t, useragent.Version{Major: 138, Minor: 0, Patch: 7204, Full: "138.0.7204.97"}, got.Browser.Version)
}

func TestClientHintsWindows11(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"15.0.0"`,
	}))
	assert.Equal(t, "Windows", got.OS.Name)
	assert.Equal(t, "11", got.OS.Version.Full)

	got = useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"10.0.0"`,
	}))
	assert.Equal(t, "10", got.OS.Version.Full)
}

func TestClientHintsMacRealVersion(t *testing.T) {
	frozenMac := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	got := useragent.ParseHeaders(headers(frozenMac, map[string]string{
		"Sec-CH-UA-Platform":         `"macOS"`,
		"Sec-CH-UA-Platform-Version": `"14.5.0"`,
	}))
	assert.Equal(t, "macOS", got.OS.Name)
	assert.Equal(t, 14, got.OS.Version.Major)
}

func TestClientHintsModelAndMobile(t *testing.T) {
	reduced := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36"
	got := useragent.ParseHeaders(headers(reduced, map[string]string{
		"Sec-CH-UA-Model": `"Pixel 8 Pro"`,
	}))
	assert.Equal(t, "Pixel 8 Pro", got.Device.Model)
	assert.Equal(t, "Google", got.Device.Brand)

	got = useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Mobile": "?1",
	}))
	assert.Equal(t, useragent.DeviceMobile, got.Device.Type)
}

func TestClientHintsMissingHeadersLeaveParseUntouched(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, nil))
	assert.Equal(t, useragent.Parse(frozenChromeWin), got)
}

func TestClientHintsMalformedIgnored(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA":                  `;;;"""not even close`,
		"Sec-CH-UA-Platform":         `"`,
		"Sec-CH-UA-Platform-Version": `"garbage"`,
		"Sec-CH-UA-Model":            `""`,
	}))
	assert.Equal(t, "Chrome", got.Browser.Name)
	assert.Equal(t, "Windows", got.OS.Name)
}

func TestClientHintsSkippedForBots(t *testing.T) {
	got := useragent.ParseHeaders(headers("Googlebot/2.1 (+http://www.google.com/bot.html)", map[string]string{
		"Sec-CH-UA": `"Brave";v="138"`,
	}))
	assert.True(t, got.IsBot())
	assert.Empty(t, got.Browser.Name)
}

func TestParseRequestDelegates(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("User-Agent", frozenChromeWin)
	r.Header.Set("Sec-CH-UA", `"Brave";v="138", "Chromium";v="138", "Not?A_Brand";v="99"`)
	got := useragent.ParseRequest(r)
	assert.Equal(t, "Brave", got.Browser.Name)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/ -run 'TestClientHints|TestParseRequest' -v`
Expected: FAIL — `ParseHeaders`/`ParseRequest` undefined.

- [ ] **Step 3: Implement Client Hints**

`web/useragent/clienthints.go`:

```go
package useragent

import (
	"net/http"
	"strings"
)

// ParseRequest parses r's User-Agent string enriched with UA Client Hints.
func ParseRequest(r *http.Request) UserAgent { return ParseHeaders(r.Header) }

// ParseHeaders parses the User-Agent header, then overrides browser
// brand/version, platform, device model, and mobile flag from Sec-CH-UA-*
// headers when present. Modern Chromium browsers freeze the UA string
// (capped version, Windows always "10", macOS always "10.15.7") and expose
// real values only through Client Hints. Missing headers leave the string
// parse untouched; malformed values are ignored. Bots skip enrichment.
func ParseHeaders(h http.Header) UserAgent {
	res := Parse(h.Get("User-Agent"))
	if res.IsBot() {
		return res
	}
	applyClientHints(&res, h)
	return res
}

func applyClientHints(res *UserAgent, h http.Header) {
	if name, ver, ok := pickBrand(h.Get("Sec-CH-UA-Full-Version-List")); ok {
		res.Browser.Name = name
		if v := parseVersion(ver); v.Major > 0 {
			res.Browser.Version = v
		}
	} else if name, ver, ok := pickBrand(h.Get("Sec-CH-UA")); ok {
		if name != res.Browser.Name {
			res.Browser.Name = name
			res.Browser.Version = Version{}
		}
		// Sec-CH-UA carries major-only versions; the string-parsed full
		// version is richer, so only override on disagreement or absence.
		if v := parseVersion(ver); v.Major > 0 && v.Major != res.Browser.Version.Major {
			res.Browser.Version = v
		}
	}
	if p := unquote(h.Get("Sec-CH-UA-Platform")); p != "" {
		applyPlatform(res, p, parseVersion(unquote(h.Get("Sec-CH-UA-Platform-Version"))))
	}
	if m := unquote(h.Get("Sec-CH-UA-Model")); m != "" && !strings.EqualFold(m, "k") {
		res.Device.Model = m
		if b := brandFromModel(m); b != "" {
			res.Device.Brand = b
		}
	}
	if h.Get("Sec-CH-UA-Mobile") == "?1" &&
		(res.Device.Type == DeviceUnknown || res.Device.Type == DeviceDesktop) {
		res.Device.Type = DeviceMobile
	}
}

// pickBrand parses a structured-header brand list like
//
//	"Chromium";v="138", "Brave";v="138.0.7204.97", "Not?A_Brand";v="99"
//
// preferring the most specific brand: any non-GREASE, non-Chromium,
// non-"Google Chrome" brand first (that is how Brave, Arc, Edge and Opera
// surface), then Google Chrome, then Chromium.
func pickBrand(list string) (name, version string, ok bool) {
	var chromium, chrome, other [2]string
	for item := range strings.SplitSeq(list, ",") {
		brand, ver := parseBrandItem(item)
		if brand == "" || isGrease(brand) {
			continue
		}
		switch {
		case strings.EqualFold(brand, "chromium"):
			chromium = [2]string{"Chromium", ver}
		case strings.EqualFold(brand, "google chrome"):
			chrome = [2]string{"Chrome", ver}
		case other[0] == "":
			other = [2]string{canonicalBrand(brand), ver}
		}
	}
	switch {
	case other[0] != "":
		return other[0], other[1], true
	case chrome[0] != "":
		return chrome[0], chrome[1], true
	case chromium[0] != "":
		return chromium[0], chromium[1], true
	}
	return "", "", false
}

// parseBrandItem reads one `"Brand";v="1.2.3"` structured-header item.
func parseBrandItem(item string) (brand, ver string) {
	i := strings.IndexByte(item, '"')
	if i < 0 {
		return "", ""
	}
	j := strings.IndexByte(item[i+1:], '"')
	if j < 0 {
		return "", ""
	}
	brand = item[i+1 : i+1+j]
	if k := strings.Index(item[i+1+j:], `;v="`); k >= 0 {
		rest := item[i+1+j+k+4:]
		if q := strings.IndexByte(rest, '"'); q >= 0 {
			ver = rest[:q]
		}
	}
	return brand, ver
}

// isGrease detects GREASE brands ("Not?A_Brand", "Not/A)Brand",
// "Not A;Brand", ...) by comparing letters only.
func isGrease(brand string) bool {
	var b strings.Builder
	for i := range len(brand) {
		c := brand[i]
		if c >= 'A' && c <= 'Z' {
			c |= 0x20
		}
		if c >= 'a' && c <= 'z' {
			b.WriteByte(c)
		}
	}
	return b.String() == "notabrand"
}

// canonicalBrand maps CH brand strings onto the same canonical names the
// string parser produces.
func canonicalBrand(brand string) string {
	switch strings.ToLower(brand) {
	case "microsoft edge":
		return "Edge"
	case "samsung internet":
		return "Samsung Internet"
	case "opera gx":
		return "Opera"
	case "yandex":
		return "Yandex"
	default:
		return brand // "Brave", "Arc", "Opera", "Vivaldi", ... already canonical
	}
}

// applyPlatform maps Sec-CH-UA-Platform (+ Platform-Version) onto OS.
// Chromium reports platformVersion >= 13 on Windows 11 and 1..12 on
// Windows 10.
func applyPlatform(res *UserAgent, platform string, pv Version) {
	switch platform {
	case "Windows":
		res.OS.Name = "Windows"
		switch {
		case pv.Major >= 13:
			res.OS.Version = Version{Major: 11, Full: "11"}
		case pv.Major > 0:
			res.OS.Version = Version{Major: 10, Full: "10"}
		}
	case "macOS":
		res.OS.Name = "macOS"
		if pv.Major > 0 {
			res.OS.Version = pv
		}
	case "Android", "Linux", "iOS":
		res.OS.Name = platform
		if pv.Major > 0 {
			res.OS.Version = pv
		}
	case "Chrome OS", "Chromium OS":
		res.OS.Name = "ChromeOS"
		if pv.Major > 0 {
			res.OS.Version = pv
		}
	}
	// Unknown platform values leave the string-parsed OS untouched.
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/useragent/ -run 'TestClientHints|TestParseRequest' -v` then `go test ./web/useragent/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "feat(useragent): Client Hints enrichment via ParseHeaders/ParseRequest"
```

---

### Task 8: Display line — String()

**Files:**
- Modify: `web/useragent/useragent.go` (add `String`, labels, imports)
- Test: `web/useragent/string_test.go`

**Interfaces:**
- Consumes: everything prior.
- Produces: `func (ua UserAgent) String() string` — the session-list/auditlog display line.

- [ ] **Step 1: Write the failing test**

`web/useragent/string_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/useragent/ -run TestString -v`
Expected: FAIL — Stringer prints struct dump / method missing (assert.Equal fails).

- [ ] **Step 3: Implement String**

Append to `web/useragent/useragent.go` (add `strconv` and `strings` to imports):

```go
var botCategoryLabels = map[BotCategory]string{
	BotSearchEngine:  "search engine",
	BotAICrawler:     "AI crawler",
	BotAIAgent:       "AI agent",
	BotSocialPreview: "link preview",
	BotMonitoring:    "monitoring",
	BotScraper:       "scraper",
	BotSEO:           "SEO",
	BotAdvertising:   "advertising",
	BotSecurity:      "security scanner",
	BotWebhook:       "webhook",
	BotFeedFetcher:   "feed fetcher",
	BotLibrary:       "HTTP library",
}

var deviceTypeLabels = map[DeviceType]string{
	DeviceDesktop:  "Desktop",
	DeviceMobile:   "Mobile",
	DeviceTablet:   "Tablet",
	DeviceTV:       "TV",
	DeviceConsole:  "Console",
	DeviceWearable: "Wearable",
	DeviceEReader:  "E-reader",
}

// String renders a human display line for session lists and audit logs:
// "Chrome 138 on macOS 14 (Desktop)", "GPTBot (AI crawler)", "Unknown".
func (ua UserAgent) String() string {
	if ua.IsBot() {
		name := ua.Bot.Name
		if name == "" {
			name = "Bot"
		}
		if label, ok := botCategoryLabels[ua.Bot.Category]; ok {
			return name + " (" + label + ")"
		}
		return name
	}
	var b strings.Builder
	if ua.Browser.Name != "" {
		b.WriteString(ua.Browser.Name)
		if ua.Browser.Version.Major > 0 {
			b.WriteByte(' ')
			b.WriteString(strconv.Itoa(ua.Browser.Version.Major))
		}
	}
	if ua.OS.Name != "" {
		if b.Len() > 0 {
			b.WriteString(" on ")
		}
		b.WriteString(ua.OS.Name)
		if v := osDisplayVersion(ua.OS.Version); v != "" {
			b.WriteByte(' ')
			b.WriteString(v)
		}
	}
	if label, ok := deviceTypeLabels[ua.Device.Type]; ok {
		if b.Len() > 0 {
			b.WriteString(" (")
			b.WriteString(label)
			b.WriteByte(')')
		} else {
			b.WriteString(label)
		}
	}
	if b.Len() == 0 {
		return "Unknown"
	}
	return b.String()
}

// osDisplayVersion shows the marketing major ("14", "10"), keeps "8.1"
// intact, and falls back to Full for non-numeric versions ("XP", "Vista").
func osDisplayVersion(v Version) string {
	if v.Full == "8.1" || v.Major == 0 {
		return v.Full
	}
	return strconv.Itoa(v.Major)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/useragent/ -run TestString -v` then `go test ./web/useragent/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "feat(useragent): String display line for session lists and audit logs"
```

---

### Task 9: Fuzzing, adversarial inputs, benchmarks

**Files:**
- Test: `web/useragent/fuzz_test.go`
- Test: `web/useragent/bench_test.go`

**Interfaces:**
- Consumes: full public API.
- Produces: nothing new — hardening evidence. Acceptance: no panics under fuzz; `BenchmarkParse/chrome-windows` reports **0 allocs/op and < 1µs/op**; UAs with underscored versions (macOS/iOS) may report 1 alloc/op (the underscore→dot normalization).

- [ ] **Step 1: Write fuzz + adversarial tests**

`web/useragent/fuzz_test.go`:

```go
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
```

Note: `h.Set` panics on header values containing invalid bytes only via `httptest`; `http.Header.Set` itself accepts any string — no guard needed.

`web/useragent/bench_test.go`:

```go
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
```

- [ ] **Step 2: Run adversarial + short fuzz**

```bash
go test ./web/useragent/ -run TestAdversarialInputs -v
go test ./web/useragent/ -fuzz FuzzParse -fuzztime 30s
go test ./web/useragent/ -fuzz FuzzParseHeaders -fuzztime 15s
```

Expected: PASS, no crashers. If the fuzzer finds a panic, fix the root cause (bounds check in the tokenizer/extractors) — do not swallow with recover.

- [ ] **Step 3: Run benchmarks and check acceptance**

Run: `go test ./web/useragent/ -bench BenchmarkParse -benchmem -run xxx`
Acceptance: `chrome-windows` and `android-mobile` at 0 allocs/op; target < 1µs/op, hard ceiling 2µs/op (the ~40-entry trigger scan makes sub-1µs borderline — if between 1µs and 2µs, record the numbers in the commit body and move on; above 2µs, reorder the trigger list cheapest-exit-first before resorting to anything clever). `iphone-safari` ≤ 1 alloc/op (underscore normalization). Do not add caching or unsafe.

- [ ] **Step 4: Format, lint, commit**

```bash
just fmt ./web/useragent/...
just lint
git add web/useragent/
git commit -m "test(useragent): fuzzing, adversarial corpus, benchmarks with acceptance results"
```

Include the measured ns/op + allocs/op numbers for all six benchmark cases in the commit message body.

---

### Task 10: doc.go, packages.md, final verification

**Files:**
- Create: `web/useragent/doc.go`
- Modify: `docs/packages.md` (move useragent from core icebox to web shipped)

**Interfaces:** none — documentation and bookkeeping.

- [ ] **Step 1: Write doc.go**

`web/useragent/doc.go`:

```go
// Package useragent parses User-Agent strings and UA Client Hints headers
// into structured browser, OS, device, and bot facts. It targets session
// device lists and audit-log display lines: canonical names, marketing OS
// versions (Windows NT 6.3 → "8.1"), device type/brand/model, and a
// categorized bot taxonomy (search engines, AI crawlers, link previews,
// monitors, HTTP libraries, ...).
//
// Parsing never fails: unrecognized input yields zero values with
// Device.Type == DeviceUnknown and Raw preserved. Modern Chromium browsers
// freeze the UA string (capped version, Windows always "10", macOS always
// "10.15.7", Brave/Arc invisible), so prefer ParseRequest/ParseHeaders at
// the HTTP boundary — they overlay real values from Sec-CH-UA-* headers
// when present.
//
// Bot detection layers a hand-curated table (accurate name, category,
// operator) over a table generated from the crawler-user-agents dataset
// (go:generate, pinned commit — see gen/) plus generic heuristics ("bot"
// suffix, "+http" comment URLs). Curated attribution wins on conflict.
//
// What this is NOT: it does not detect CPU architecture or screen metrics,
// it cannot see Apple hardware beyond "iPhone"/"iPad" (Safari never exposes
// it), it cannot distinguish desktop-mode iPad Safari from a real Mac
// (that requires client-side JS), and it does not fetch remote bot lists at
// runtime — dataset updates are reviewed code changes.
//
// # Usage
//
//	ua := useragent.ParseRequest(r) // Client Hints-aware
//	if ua.IsBot() {
//		log.Info("crawler", "bot", ua.Bot.Name, "category", ua.Bot.Category)
//		return
//	}
//	session.Device = ua.String() // "Chrome 138 on macOS 14 (Desktop)"
//
// For raw strings from storage (session rows, log pipelines) use Parse:
//
//	ua := useragent.Parse(storedUA)
package useragent
```

- [ ] **Step 2: Update docs/packages.md**

Three edits (verify exact current text with `grep -n useragent docs/packages.md` first):

1. In the `core/` tree comment, line `#   # icebox:  useragent qrcode namegen` → `#   # icebox:  qrcode namegen`.
2. In the `web/` tree comment, append `useragent` to the end of the shipped list (the `#   # shipped: ...` lines under `web/`).
3. Delete the core icebox bullet (two lines starting `- \`useragent\` — stdlib User-Agent string parser`) and add a shipped-style entry to the `### web/` section matching its neighbors' format, e.g.:

```
- `useragent` — User-Agent + UA Client Hints parser (browser/OS/device/bot
  taxonomy); feeds session device lists and auditlog display.
```

- [ ] **Step 3: Full verification**

```bash
just fmt ./web/useragent/...
just test ./web/useragent/...
just lint
```

Expected: all green, race-clean.

- [ ] **Step 4: Commit**

```bash
git add web/useragent/ docs/packages.md
git commit -m "docs(useragent): package docs; move useragent to web/ shipped in packages.md"
```

---

## Post-plan notes for the executor

- **Spec deviation (documented):** the spec's curated list named "Google-Extended" — it is a robots.txt product token only and never appears in a User-Agent header, so it is intentionally absent from the curated table. `OAI-SearchBot`, `Meta-ExternalAgent`, and the AI-agent entries (`ChatGPT-User`, `Claude-User`, `Perplexity-User`) are additions in the same spirit as the spec's list.
- **Trigger-gate trade-off:** generated patterns without a trigger substring are skipped by the generator (logged count). This is what keeps browser-UA parses under 1µs; the invariant tests make the trade-off explicit and drift-proof.
- The fixture UA corpus is hand-assembled; if a fixture turns out to mismatch real-world UA syntax, fix the fixture only with a verified real UA string, never by bending a matcher to a wrong fixture.
- PR flow after Task 10 (per CLAUDE.md): create PR → wait for CI → fix failures → read Claude review → fix → repeat. Remember claude-code-review.yml can time out silently on big PRs — run your own review pass.
