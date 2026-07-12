# web/fingerprint — design

Date: 2026-07-12
Status: approved for planning

## Purpose

Turn an HTTP request into a **versioned identity + a set of structured
anti-fraud signals**, from as little as the request headers up to a full TLS
+ JS device probe. A layered **fingerprinting brick**: every layer is opt-in,
so the same package serves session-hijack detection (headers ± IP) and
finance/iGaming fraud screening (headers + IP + TLS + JS) without a consumer
paying for layers they don't use.

Output is **identity + facts, never a score**. The package computes a stable
fingerprint and emits boolean/enum signals (`datacenter-asn`, `headless`,
`tls-ua-mismatch`, …); weighting those into an allow/challenge/deny decision
is business-specific policy that stays in the consumer.

This **relocates and supersedes the planned `auth/fingerprint`** catalog
entry (which scoped only "UA + Accept headers ± IP, sha256"). It lives in
`web/` because every layer is a web-boundary concern — it reads HTTP headers,
serves a script, ingests a browser POST, and hangs off the TLS handshake.
`auth/session`'s `WithFingerprint(Warn|Strict)` hijack detection becomes a
consumer of it: session stores a `Digest`, compares on each request, reacts to
`Drift`.

## Placement

`web/fingerprint` — beside `web/clientip`, `web/geoip`, `web/useragent`, whose
outputs it composes. One driver subpackage isolates the one genuinely fiddly
piece of code (raw TLS ClientHello parsing):

- `web/fingerprint` — `Collector` seam, `Component`/`Fingerprint`/`Digest`
  types, keyed hashing, `Drift`, the header/IP/JS collectors, `Signal` +
  inspectors, the JS `ScriptHandler`/`IngestHandler`, middleware, context
  accessors, presets. **stdlib + cheap forge-internal deps only.**
- `web/fingerprint/tlsprint` — TLS fingerprint collectors: trusted-proxy
  header sources (Cloudflare/CloudFront/generic) **and** a local
  raw-ClientHello→JA4 computation with a `net.Listener` wrapper, for the case
  where the Go server terminates TLS. Imports the parent only for the
  `Collector`/`Component` types (child→parent, no cycle — like `cache/redis` →
  `cache`).

Core imports only cheap, resource-free forge-internal packages it genuinely
needs: `crypto/sign` (signed JS-ingest token), `web/cookie` (payload carry),
`web/clientip` (IP collector + trusted-proxy predicate), `ctxkey`,
`middleware`. It deliberately does **not** import `web/geoip` (needs a runtime
mmdb resource) or `web/useragent` — those reach the signal inspectors through
wired function seams (see §Dependency boundary). When this ships, delete the
`auth/fingerprint` entry from docs/packages.md.

## Decisions (resolved during brainstorming)

1. **Scope = all four layers, opt-in (option D).** headers → +IP → +TLS → +JS
   probe. Composability is the headline requirement: a consumer wires exactly
   the layers they need.

2. **Output = identity + structured signals, no scoring (option B).** The
   cross-layer *mismatch* signals — which require seeing every layer at once,
   and which no single existing package can produce — are the package's edge.
   Scoring is consumer policy (iGaming ≠ SaaS); a generic score invites false
   confidence.

3. **JS binding = stateless signed token (A) + cookie carry (C); store
   (B) available.** The server issues a short-lived `crypto/sign` token bound
   to a fresh nonce, an expiry, and an IP-hash (no session-id claim); the
   probe echoes it with its payload; the ingest handler verifies it, making
   the payload tamper-evident (client data is only accepted with a valid
   unexpired token). Per-poster isolation comes from the per-nonce signed
   cookie the poster holds (or, with `WithStore`, the nonce-keyed store row) —
   not from a session binding. The merged payload is carried in a signed
   cookie so subsequent requests need no re-probe. A `cache.Store` nonce path
   is offered for consumers who need cross-request correlation without
   cookies.

4. **Composition = `Collector` seam + preset helpers (A+C).** The `Collector`
   interface makes each layer independently black-box-testable (design.md §31)
   and pulls each layer's dependency in only when that collector is added —
   mirroring how `web/geoip` composes `Source`s via `Chain`. Presets
   (`Session`, `Antifraud`) give the one-liner without hiding the à la carte
   model.

5. **Dependency direction = stdlib core + seam-wired lookups.** Signals that
   need a runtime resource or a heavier package (`datacenter-asn` /
   `geo-tz-mismatch` → geoip; `bot-ua` → useragent) take a function seam the
   consumer wires. Absent seam → that signal is simply not emitted. The "just
   headers" tier stays pure stdlib.

6. **TLS = a trust-gated `Source` chain, header-first, local-fallback.** In
   most real deployments (Caddy/Envoy/Traefik/Cloudflare/CloudFront/ALB) the
   Go server never sees the ClientHello, so local JA4 is the *minority* path.
   The primary path reads a JA3/JA4 hash from a **trusted upstream header**
   (identical pattern to geoip's CDN header sources); local raw-ClientHello
   computation is the fallback when Go terminates TLS. Every header source is
   gated on trusted-proxy validation — an untrusted header is dropped, never
   believed (the `X-Forwarded-For` spoofing problem `web/clientip` already
   solves).

7. **JS probe = minimal, high-signal, readable (A); canvas/WebGL opt-in.**
   The default probe collects the cheap, stable, fraud-relevant set (screen,
   timezone, languages, platform, `hardwareConcurrency`, touch, and headless/
   automation markers). Maximal per-device uniqueness (canvas/audio/font
   enumeration) is an arms race forge won't maintain and a GDPR liability in
   the regulated target domain — so canvas + WebGL-renderer are **opt-in
   config flags**, off by default, and the ingest payload schema is public so a
   consumer needing FingerprintJS-grade entropy POSTs that library's output to
   the same endpoint. (A 1px image can't read hardware; the GPU "hardware
   info" comes from JS reading `WebGL UNMASKED_RENDERER`, gated behind the
   WebGL flag.)

## API surface — `web/fingerprint`

### Collector seam & assembly

```go
// Collector contributes zero or more named components from a request.
type Collector interface {
    Collect(r *http.Request) ([]Component, error)
}

type Component struct {
    Name  string // "ua", "accept", "accept-lang-order", "tls", "js-canvas"…
    Value string // normalized raw value (in-request; may be PII)
}

func New(cfg Config, cs ...Collector) *Fingerprinter
func (fp *Fingerprinter) FromRequest(r *http.Request) (Fingerprint, error)
```

Each collector reads from its own source: `headers` from `r.Header`; `clientip`
from the trusted-proxy-aware client IP; `tlsprint` from a trusted header or the
listener's per-connection JA4; the `js` collector from the signed cookie/store
that `IngestHandler` populated.

### Built-in collectors (core, stdlib)

```go
func Headers(opts ...HeadersOption) Collector // UA, Accept, Accept-Language
                                              // (+ its q-order), Accept-Encoding,
                                              // and header-name ordering
func ClientIP(opts ...IPOption) Collector     // client IP via web/clientip
func JS() Collector                           // merges the ingested probe payload
```

### Identity

```go
type Fingerprint struct {
    Version    int
    Components []Component // in-request; carries raw values
    Hash       string      // hex keyed HMAC-SHA256 over canonical components
}

// Digest is the persistable form: per-component HMACs, no raw PII.
type Digest struct {
    Version int
    Parts   map[string]string // component name → hex HMAC
    Hash    string            // combined
}
func (f Fingerprint) Digest() Digest
func Drift(old, new Digest) []string // names of components whose hash changed
```

**Keyed HMAC, not bare sha256:** a consumer-supplied secret (pepper) prevents
cross-site correlation, permits rotation, and is GDPR-friendlier. Consumers
**persist the `Digest`** (hashes only) and compare with `Drift`, which reports
*which* layer moved — a UA version bump is benign; a simultaneous `tls` + `asn`
flip is not. Canonical encoding sorts components by `Name` for a stable hash.

### Signals

```go
type Signal struct {
    Name   string // "datacenter-asn", "bot-ua", "headless", …
    Value  bool
    Detail string // human-readable why
}
func (fp *Fingerprinter) Signals(r *http.Request, f Fingerprint) []Signal
```

v1 inspector set (lookup-dependent ones stay dark unless their seam is wired):

| Signal | Needs | Wired from |
|---|---|---|
| `datacenter-asn` | `GeoLookup` seam | web/geoip |
| `bot-ua` | `UAFamily` seam | web/useragent |
| `headless` | JS payload | probe markers (`navigator.webdriver`, API contradictions) |
| `tls-ua-mismatch` | tls + ua components | UA claims a browser, JA4 matches a known automation tool |
| `geo-tz-mismatch` | `GeoLookup` seam + JS tz | JS timezone continent ≠ IP continent |
| `lang-mismatch` | Accept-Language + JS | header languages ≠ `navigator.languages` |
| `header-anomaly` | headers + ua | claimed browser missing its expected header markers |

`tls-ua-mismatch` v1 matches the `tls` component against a small **embedded set
of well-known automation JA4s** (curl, python-requests, Go `net/http`,
headless-Chrome) rather than a comprehensive JA4→browser map — a bounded,
maintainable table, not an arms race.

`header-anomaly` is **value/presence-based, not wire-order-based**: `net/http`
parses request headers into an unordered `map`, so true header-ordering
fingerprinting (a JA4H-style signal) would need raw-request capture and is out
of v1 scope. v1 fires when the UA claims a modern browser but the headers that
browser always sends are absent/inconsistent — e.g. a Chrome ≥ 90 UA with no
`sec-ch-ua` / `sec-fetch-*`, or an `Accept` that doesn't match the claimed
browser's canonical navigation `Accept`.

The JS payload is treated as **claimed, not trusted** — its anti-fraud value is
feeding the *mismatch* inspectors (JS says platform=Windows, UA says macOS →
contradiction), not raw entropy.

### Dependency boundary — wired seams

```go
// One seam over web/geoip's merged Location — covers datacenter-asn + geo-tz-mismatch.
type GeoInfo struct {
    Continent string // for geo-tz-mismatch
    Timezone  string // IANA, for geo-tz-mismatch
    ASN       uint
    Hosting   bool   // datacenter/hosting ASN, for datacenter-asn
}
type GeoLookup func(netip.Addr) (GeoInfo, bool) // consumer wires web/geoip
type Family    int                              // browser/tool class
type UAFamily  func(ua string) (Family, bool)   // consumer wires web/useragent
```

Passed as options: `WithGeoLookup(fn)`, `WithUAFamily(fn)`. An inspector whose
seam is unset never emits — so `Session()` needs no wiring; `Antifraud()` is
fully wired.

### JS probe

```go
func (fp *Fingerprinter) ScriptHandler() http.Handler // serves go:embed probe.js,
                                                       // versioned path, long-cache,
                                                       // exposes SRI hash for CSP
func (fp *Fingerprinter) IngestHandler() http.Handler  // POST {token,data}: verify
                                                        // crypto/sign token, whitelist
                                                        // + clamp fields, store payload
func (fp *Fingerprinter) IssueToken(r *http.Request) (string, error) // for the page
```

Binding is stateless-signed by default (token bound to nonce + IP-hash +
expiry, `TokenTTL`; no session-id claim — per-poster isolation comes from the
per-nonce signed cookie / nonce-keyed store row the poster holds),
cookie-carried for convenience (`web/cookie`, signed), store-backed optional
(`WithStore(cache.Store)`). Ingest **never trusts client data blindly**: every
field is whitelisted, size-clamped, and enum-checked; the parser is fuzzed.

`probe.js` (opt-in flags gate the heavier collectors):
- always: screen/pixel-ratio, timezone + offset, `navigator` languages /
  platform / `hardwareConcurrency` / `deviceMemory`, touch points, headless
  markers.
- `ProbeCanvas`: a small offscreen canvas render hash.
- `ProbeWebGL`: `UNMASKED_VENDOR/RENDERER` GPU string + render hash.

### Middleware & context

```go
type Result struct { Fingerprint Fingerprint; Signals []Signal }
func (fp *Fingerprinter) Middleware() middleware.Middleware // compute once, stash
func FromContext(ctx context.Context) (Result, bool)
```

### Presets

```go
func Session(cfg Config) *Fingerprinter // Headers (+ optional ClientIP); pure stdlib
func Antifraud(cfg Config, geo GeoLookup, ua UAFamily, tls Collector) *Fingerprinter
```

### Config (env-loadable)

```go
type Config struct {
    Secret      string        `env:"FINGERPRINT_SECRET"`      // HMAC pepper (required)
    Version     int           `env:"FINGERPRINT_VERSION"`     // schema version, default 1
    TokenTTL    time.Duration `env:"FINGERPRINT_TOKEN_TTL"`   // JS-ingest token validity
    ProbeCanvas bool          `env:"FINGERPRINT_PROBE_CANVAS"`
    ProbeWebGL  bool          `env:"FINGERPRINT_PROBE_WEBGL"`
}
func DefaultConfig() Config
func (c Config) Validate() error // Secret required when hashing/JS layer used
```

## API surface — `web/fingerprint/tlsprint`

```go
type TrustFunc func(*http.Request) bool // wired from web/clientip trusted-proxy check

// Upstream-terminated (the common case): read a JA hash from a trusted header.
func CloudflareJA3(trusted TrustFunc) fingerprint.Collector // Cf-Ja3-Hash
func CloudFrontJA4(trusted TrustFunc) fingerprint.Collector // CloudFront-Viewer-JA4-Fingerprint
func Header(name string, trusted TrustFunc) fingerprint.Collector // Envoy/Caddy/Traefik custom

// Go-terminated (fallback): compute JA4 from the raw ClientHello.
func Listener(inner net.Listener) net.Listener // peeks ClientHello, stashes JA4 by conn
func Local() fingerprint.Collector             // reads the stashed JA4 for r's conn

// First non-empty component wins.
func Chain(cs ...fingerprint.Collector) fingerprint.Collector
```

Typical wiring: `Chain(CloudflareJA3(trusted), CloudFrontJA4(trusted),
Header("X-JA4", trusted), Local())` — works whether TLS is terminated at the
CDN, at a self-managed proxy, or in-process; if none applies, the `tls`
component is simply absent and the other layers still fingerprint. The raw
ClientHello→JA4 parser is confined to this subpackage with golden test vectors,
and is fuzzed.

## Deployment matrix (why the TLS layer is a source chain)

| Terminator | TLS fingerprint available as | Wiring |
|---|---|---|
| Cloudflare | `Cf-Ja3-Hash` / JA4, `Cf-Bot-Score` (managed transforms) | `CloudflareJA3(trusted)` |
| AWS CloudFront | `CloudFront-Viewer-JA3/JA4-Fingerprint` (managed headers) | `CloudFrontJA4(trusted)` |
| AWS ALB (plain) | none | header/UA/JS layers only |
| Envoy | JA3/JA4 from TLS inspector → custom header | `Header("X-JA4", trusted)` |
| Caddy | JA3/JA4 via plugin/placeholder → `header_up` | `Header(…, trusted)` |
| Traefik | JA3 via plugin → custom header | `Header(…, trusted)` |
| DigitalOcean LB | none (client IP via PROXY protocol) | header/UA/JS layers only |
| Go terminates TLS | computed locally | `Listener(...)` + `Local()` |

Upstream bot scores (Cloudflare `Cf-Bot-Score`, CloudFront verified-bot)
become ingestible signals through the same trust-gated header mechanism — the
package surfaces them as facts without computing anything.

## Anti-scope (stays in the consumer)

- **Scoring / verdict** — weighting signals into allow/challenge/deny is
  business policy.
- **Maximal-uniqueness JS** — BYO FingerprintJS to the public ingest schema;
  forge ships a stable minimal probe, not an entropy arms race.
- **Bot-score computation** — ingest the upstream CDN's; don't recompute.
- **Fingerprint storage** — consumers persist the `Digest`; the package holds
  no database.
- **Device graph / cross-request identity resolution** — consumer domain over
  the persisted `Digest`s.

## Testing policy

- Black-box, table-driven per collector and per inspector.
- Golden JA4 vectors in `tlsprint` (known ClientHello bytes → known JA4).
- Signed-token tamper tests: expired, bad signature, IP-hash mismatch,
  replay.
- **Fuzz** the ingest payload parser and the ClientHello parser — the
  `useragent`/`geoip` precedent proved fuzzing catches real
  unbounded-recursion / overflow DoS in exactly this kind of untrusted-input
  parsing.
- Signal inspectors tested against fake `GeoLookup`/`UAFamily` seams (test
  doubles live with the seam owner — design.md §32).
- `probe.js` kept small enough to review by eye; the Go side asserts the
  ingest contract (schema, clamping, token binding) rather than running a JS
  engine.

## Consumer tiers (the composability payoff)

```go
// General app — session-hijack detection. Pure stdlib.
fp := fingerprint.Session(cfg)

// Middle — UA + IP + TLS, no JS.
fp := fingerprint.New(cfg,
    fingerprint.Headers(), fingerprint.ClientIP(),
    tlsprint.Chain(tlsprint.CloudflareJA3(trusted), tlsprint.Local()))

// Finance / iGaming — full stack.
fp := fingerprint.Antifraud(cfg, geoipLookup, uaFamily,
    tlsprint.Chain(tlsprint.CloudflareJA3(trusted), tlsprint.Local()))
mux.Handle("/_fp/probe.js", fp.ScriptHandler())
mux.Handle("/_fp/ingest",   fp.IngestHandler())
```
