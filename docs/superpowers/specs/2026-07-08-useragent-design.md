# web/useragent — design

Date: 2026-07-08
Status: approved for planning

## Purpose

Parse User-Agent strings and UA Client Hints headers into structured
browser / OS / device / bot facts, with maximum practical detection breadth.
Primary consumers: session device lists and auditlog display lines; secondary:
any boundary code that needs bot classification or device-type routing.

## Placement

`web/useragent`. The icebox entry scoped this as `core/` ("string-in
primitive"), but with Client Hints enrichment the primary entry points read
HTTP request headers — a web-boundary concern, same shelf as `web/clientip`.
Zero forge deps, stdlib only. When this ships, move the `useragent` icebox
entry in docs/packages.md from `core/` icebox to the `web/` shipped list.

## API surface

```go
package useragent

// Core primitive — parses a raw UA string.
func Parse(ua string) UserAgent

// Client Hints-aware variants: parse the UA string, then override
// browser brand/version, platform+version, and device model from
// Sec-CH-UA-* headers when present.
func ParseRequest(r *http.Request) UserAgent // delegates to ParseHeaders
func ParseHeaders(h http.Header) UserAgent

type UserAgent struct {
    Browser Browser // Name ("Chrome"), Version
    OS      OS      // Name ("macOS"), Version
    Device  Device  // Type, Brand ("Samsung"), Model ("SM-G991B")
    Bot     Bot     // zero value unless a bot; Name, Category, Operator
    Raw     string  // original UA string
}

func (ua UserAgent) IsBot() bool
func (ua UserAgent) String() string // "Chrome 138 on macOS 14 (Desktop)"

type Browser struct { Name string; Version Version }
type OS      struct { Name string; Version Version }
type Device  struct { Type DeviceType; Brand, Model string }
type Bot     struct { Name string; Category BotCategory; Operator string }

type Version struct { Major, Minor, Patch int; Full string }
```

- `DeviceType` (typed string constants): `Desktop, Mobile, Tablet, TV,
  Console, Wearable, EReader, Bot, Unknown`.
- `BotCategory` (typed string constants): `SearchEngine, AICrawler, AIAgent,
  SocialPreview, Monitoring, Scraper, SEO, Advertising, Security, Webhook,
  FeedFetcher, Library, Unknown`.
- When a bot is detected, `Device.Type == Bot` and the `Bot` field is
  populated; `IsBot()` is defined as `Device.Type == Bot`. Browser/OS fields
  stay zero for bots.
- Browser/OS names are canonical strings, not enums — the long tail is
  open-ended and consumers display rather than switch on them.
- Never errors, never panics. Unrecognized input → zero values with
  `Device.Type == Unknown`, `Raw` preserved. Empty UA string → zero result,
  NOT auto-bot (empty-UA policy belongs to consumers).
- No options, no state, no configuration.

## Detection matrix

### Browsers (name + version)

Chrome, Safari, Mobile Safari, Firefox, Edge, Opera, Opera Mini,
Samsung Internet, Brave (Client-Hints-only — hides in the UA string),
Vivaldi, Yandex, UC Browser, QQ, WeChat/MiniProgram, DuckDuckGo, Arc,
Whale, MIUI Browser, Huawei Browser; in-app webviews: Facebook, Instagram,
TikTok, Line, GSA (Google app), generic Android WebView, iOS WKWebView
heuristic.

Ordering constraint: Edge/Opera/Vivaldi/Samsung/etc. embed `Chrome/`; nearly
everything embeds `Safari/`. Matchers run distinctive-token-first, Chrome
before Safari, Safari last.

### Operating systems (name + version, marketing-version mapping)

Windows (NT 10.0 → "10/11"; CH platformVersion disambiguates 11), macOS,
iOS, iPadOS (desktop-mode iPad heuristic: Macintosh + touch signals),
Android, ChromeOS, Linux (generic), FreeBSD/OpenBSD/NetBSD, KaiOS, Tizen,
webOS, HarmonyOS, Fire OS (via device), PlayStation/Xbox/Nintendo system
software.

### Device type + brand/model

- Mobile vs Tablet: Android `Mobile` token, iPhone/iPad tokens.
- Brand/model: Android device segment (`; SM-G991B Build/`), Apple hardware
  tokens (model granularity is "iPhone"/"iPad" only — Apple never exposes
  finer detail in UA), token tables for Samsung, Xiaomi/Redmi/POCO,
  Huawei/Honor, OPPO/OnePlus/realme/vivo, Google Pixel, Motorola, Nokia,
  Sony, LG, Lenovo, TCL, Amazon Kindle/Fire.
- TV: Android TV, Apple TV, Tizen/webOS TVs, Roku, Fire TV, Chromecast,
  generic SmartTV/HbbTV tokens.
- Console: PlayStation 4/5, Xbox, Nintendo Switch.
- Wearable: Apple Watch, Wear OS. EReader: Kindle, Kobo.
- `Sec-CH-UA-Model` overrides parsed model when present.

### Bots — two layers

1. **Curated table** (hand-maintained, ~40 entries) where category and
   operator matter for display: Googlebot, Bingbot, DuckDuckBot, Baiduspider,
   YandexBot (SearchEngine); GPTBot, ClaudeBot, PerplexityBot, Bytespider,
   CCBot, Google-Extended (AICrawler); facebookexternalhit, Twitterbot,
   Slackbot, WhatsApp, Discordbot, TelegramBot (SocialPreview); UptimeRobot,
   Pingdom, StatusCake (Monitoring); AhrefsBot, SemrushBot (SEO); Stripe,
   PayPal IPN (Webhook); feed fetchers (FeedFetcher); HTTP-library
   fingerprints — curl, wget, python-requests/httpx, Go-http-client, okhttp,
   Java, axios/node-fetch (Library).
2. **Generated table** (`go:generate` from the public crawler-user-agents
   dataset, vendored as `bot_generated.go`): long-tail patterns →
   `IsBot()=true`, best-effort name, category Unknown unless inferable.
3. Generic heuristics: `bot`, `crawler`, `spider`, `scan` tokens, URL in the
   UA comment.

Precedence: curated > generated > heuristics. Unmatched → not a bot.

## Internals

**Single-pass tokenizer.** Lowercase into a stack buffer (no allocation for
UAs ≤ ~512 bytes), split into tokens (words, `Name/Version` pairs,
parenthesized comment segments), run ordered matchers: bot first
(short-circuits), then browser, OS, device type, brand/model.

**File layout**

```
web/useragent/
├── doc.go              # package doc + usage
├── useragent.go        # types, Parse, ParseHeaders, ParseRequest, String()
├── version.go          # Version parsing
├── tokenizer.go        # UA string → token stream
├── browser.go          # ordered browser matchers
├── os.go               # OS matchers + marketing-version mapping
├── device.go           # device type + brand/model token tables
├── bot.go              # curated bot table + generic heuristics
├── bot_generated.go    # go:generate output (crawler-user-agents dataset)
├── clienthints.go      # Sec-CH-UA-* parsing and merge-over-parse
└── gen/                # generator main (go run ./gen), pinned dataset commit
```

**Client Hints merge** (`ParseHeaders`): `Sec-CH-UA` brand list (pick the
non-"Not?A_Brand", non-"Chromium" brand — how Brave/Arc/Edge surface),
`Sec-CH-UA-Full-Version-List` (unfreezes version), `Sec-CH-UA-Platform` /
`-Platform-Version` (unfreezes Windows 11 / real macOS version),
`Sec-CH-UA-Model`, `Sec-CH-UA-Mobile`. Header values win field-by-field over
the string parse; missing headers leave the string result untouched.
Structured-header parsing is an RFC 8941-lite reader covering only the subset
these headers use. Malformed CH values are ignored.

**Codegen**: `gen/` downloads a pinned commit of the crawler-user-agents
JSON and compiles substring patterns into a sorted table in
`bot_generated.go` (source commit hash in the header comment). Regeneration
is a manual, reviewed action — never a build-time network dependency.

**Bot matching**: single pass over the lowercased UA against a
sorted/grouped substring table — not thousands of `strings.Contains` calls.
Group by leading byte first; upgrade to an Aho-Corasick-lite scan only if
benchmarks demand it.

## Testing (black-box only, `useragent_test`)

- **Corpus table tests**: real UA fixtures → expected results, per concern.
  Every browser/OS/device/bot named above gets ≥1 fixture, including
  ordering traps (Edge-embeds-Chrome, everything-embeds-Safari, desktop-mode
  iPad, OPR vs Opera Mini).
- **Client Hints**: frozen Chrome UA + CH headers → unfrozen
  version/Windows 11/brand; missing headers → untouched; malformed CH →
  ignored, no panic; `ParseRequest` delegation.
- **Adversarial**: empty string, 10KB junk, unmatched parens, UTF-8 garbage,
  null bytes, bare `Mozilla/5.0` — return Unknown, never panic. `FuzzParse`
  asserting "never panics, Raw round-trips".
- **Generated table**: sample dataset patterns report `IsBot()`;
  curated-over-generated precedence verified.
- **`String()` goldens**: "Chrome 138 on macOS 14 (Desktop)",
  "GPTBot (AI crawler)", "Unknown".
- **Benchmarks**: representative UAs (desktop Chrome, Android mobile, bot,
  garbage) with allocation reporting. Acceptance: 0 allocs/op happy path,
  < 1µs/op.

## Non-goals

- No middleware (consumers call `ParseRequest` directly; a context-caching
  middleware can be added later on demand).
- No CPU/GPU/architecture detection, no screen metrics.
- No UA string *generation*.
- No live/remote bot lists at runtime — dataset updates are code changes.
- Apple device model granularity beyond "iPhone"/"iPad" (not present in UA).

## Wrap-up checklist for implementation

- doc.go with usage example.
- docs/packages.md: move `useragent` from core/ icebox to web/ shipped.
- `just fmt ./web/useragent/...`, `just lint` green.
