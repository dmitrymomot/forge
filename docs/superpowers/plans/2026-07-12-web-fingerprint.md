# web/fingerprint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `web/fingerprint` — a layered, opt-in request-fingerprinting brick that emits a versioned identity plus structured anti-fraud signals — and its `web/fingerprint/tlsprint` subpackage for TLS (JA4) fingerprinting.

**Architecture:** A `Collector` seam assembles named `Component`s from whichever layers a consumer wires (headers → IP → TLS → JS probe). A `Fingerprinter` HMAC-hashes the components into a `Fingerprint`/`Digest` and derives boolean `Signal`s via inspectors. Heavy/resource-bound lookups (geoip ASN+geo, useragent family) enter through wired function seams, so the core stays stdlib + cheap forge-internal deps. TLS fingerprints arrive either from trusted upstream headers or, when Go terminates TLS, from a `net.Listener` wrapper that parses the raw ClientHello into a JA4 string (isolated in the `tlsprint` subpackage).

**Tech Stack:** Go (stdlib: `crypto/hmac`, `crypto/sha256`, `net/http`, `net/netip`, `encoding/json`, `//go:embed`), forge-internal: `crypto/sign`, `crypto/keyset`, `web/cookie`, `web/clientip`, `web/middleware`, `core/ctxkey`, `core/clock`, `core/random`, `resilience/cache`, `ops/logger`. Spec: [docs/superpowers/specs/2026-07-12-web-fingerprint-design.md](../specs/2026-07-12-web-fingerprint-design.md).

## Global Constraints

- **Module path:** `github.com/dmitrymomot/forge`.
- **Design DNA (docs/design.md):** no reflection; one of the three idioms — this package uses `New(cfg Config, ...Option)` with an env-loadable `Config` + `DefaultConfig` + `Validate`. Anatomy: `doc.go` (runnable example) · `config.go` · `options.go` (`type Option func(*Fingerprinter)`, **never** builders) · `errors.go` (`errors.Is`-matchable single-line sentinels) · impl.
- **Black-box tests only:** test files are `package fingerprint_test` / `package tlsprint_test`. White-box only to assert unexported state (avoid here).
- **Dependency boundary:** core `web/fingerprint` must **not** import `web/geoip` or `web/useragent`. Those reach signals through the `GeoLookup` / `UAFamily` function seams, wired by the consumer. Adapter examples that import them live in `example_test.go` (external test package — test deps are not package deps).
- **Env prefixes baked into tags:** every `Config` field carries the `FINGERPRINT_` prefix.
- **Single-line slog errors; no eager formatting.** Best-effort enrichment (middleware) never fails the request.
- **Untrusted input is capped and validated:** the JS ingest body uses `http.MaxBytesReader`; every ingested field is length/enum-clamped; the ClientHello parser and ingest parser are fuzzed.
- **After each file change run** `just fmt ./web/fingerprint/...` **and at the end of every task run** `just lint` and `just test ./web/fingerprint/...`.
- **Commit** after every task with a `feat:`/`test:` conventional message. Do NOT add any Claude attribution to commits.
- **`for i := range N`** for counted loops (modernize enforces this). Keep hot structs pointer-light (betteralign enforces field order via `just fmt`).

## File Structure

```
web/fingerprint/
  doc.go            # T10 — package doc + runnable Example
  config.go         # T1  — Config, DefaultConfig, Validate
  errors.go         # T1  — sentinels
  fingerprint.go    # T1  — Component, Fingerprint, Digest, hashing, Drift
  seams.go          # T2  — GeoInfo, GeoLookup, Family, UAFamily
  collector.go      # T2  — Collector, Fingerprinter, New, FromRequest
  options.go        # T2  — Option + With* options
  headers.go        # T3  — Headers collector
  clientip.go       # T4  — ClientIP collector
  signals.go        # T5  — Signal, Signals(), seam inspectors (datacenter-asn, bot-ua)
  signals_component.go # T6 — component inspectors (headless, tls-ua-mismatch, lang-mismatch, geo-tz-mismatch, header-anomaly)
  middleware.go     # T7  — Middleware, Result, FromContext, LogExtractor
  jsprobe_token.go  # T8  — IssueToken, token sign/verify, clock
  jsprobe.go        # T9  — ScriptHandler, IngestHandler, ProbeSRI, JS collector
  assets/probe.js   # T9  — embedded probe script
  presets.go        # T10 — Session, Antifraud
  example_test.go   # T10 — runnable adapter examples (imports geoip/useragent in _test)

web/fingerprint/tlsprint/
  doc.go            # T13 — package doc
  tlsprint.go       # T11 — TrustFunc, header sources, Chain
  clienthello.go    # T12 — raw ClientHello parser
  ja4.go            # T12 — JA4 assembly
  listener.go       # T13 — Listener, Conn, ConnContext, Local
```

Update `docs/packages.md` (delete the `auth/fingerprint` entry) in T14.

---

### Task 1: Identity types, hashing, Config, errors

**Files:**
- Create: `web/fingerprint/config.go`, `web/fingerprint/errors.go`, `web/fingerprint/fingerprint.go`
- Test: `web/fingerprint/fingerprint_test.go`, `web/fingerprint/config_test.go`

**Interfaces:**
- Produces: `Config`, `DefaultConfig() Config`, `(Config).Validate() error`; `Component{Name, Value string}`; `Fingerprint{Version int; Components []Component; Hash string}` with `(Fingerprint).Digest() Digest`; `Digest{Version int; Parts map[string]string; Hash string}`; `Drift(old, next Digest) []string`; unexported `combineHash(secret []byte, version int, comps []Component) (hash string, parts map[string]string)`; sentinels `ErrNoSecret`, `ErrBadVersion`, `ErrBadTokenTTL`, `ErrBadToken`.

- [ ] **Step 1: Write failing tests**

```go
// config_test.go
package fingerprint_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestConfigValidate(t *testing.T) {
	if err := (fingerprint.Config{Version: 1, TokenTTL: time.Minute}).Validate(); !errors.Is(err, fingerprint.ErrNoSecret) {
		t.Fatalf("missing secret: got %v", err)
	}
	if err := (fingerprint.Config{Secret: "s", Version: 0, TokenTTL: time.Minute}).Validate(); !errors.Is(err, fingerprint.ErrBadVersion) {
		t.Fatalf("bad version: got %v", err)
	}
	if err := (fingerprint.Config{Secret: "s", Version: 1, TokenTTL: 0}).Validate(); !errors.Is(err, fingerprint.ErrBadTokenTTL) {
		t.Fatalf("bad ttl: got %v", err)
	}
	if err := fingerprint.DefaultConfig(); err.Version != 1 || err.TokenTTL <= 0 {
		t.Fatalf("defaults: %+v", err)
	}
	ok := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}
```

```go
// fingerprint_test.go
package fingerprint_test

import (
	"slices"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestDrift(t *testing.T) {
	old := fingerprint.Digest{Version: 1, Parts: map[string]string{"ua": "a", "ip": "b"}}
	next := fingerprint.Digest{Version: 1, Parts: map[string]string{"ua": "a", "ip": "c", "tls": "d"}}
	got := fingerprint.Drift(old, next)
	if want := []string{"ip", "tls"}; !slices.Equal(got, want) {
		t.Fatalf("Drift = %v, want %v", got, want)
	}
	if d := fingerprint.Drift(old, old); len(d) != 0 {
		t.Fatalf("no drift expected, got %v", d)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/fingerprint/ -run 'TestConfigValidate|TestDrift'`
Expected: FAIL — `undefined: fingerprint.Config` etc.

- [ ] **Step 3: Implement**

```go
// config.go
package fingerprint

import "time"

// Config configures a Fingerprinter. Secret peppers the identity HMAC and signs
// JS-probe tokens/cookies; it is required.
type Config struct {
	Secret      string        `env:"FINGERPRINT_SECRET"`
	Version     int           `env:"FINGERPRINT_VERSION"`
	TokenTTL    time.Duration `env:"FINGERPRINT_TOKEN_TTL"`
	ProbeCanvas bool          `env:"FINGERPRINT_PROBE_CANVAS"`
	ProbeWebGL  bool          `env:"FINGERPRINT_PROBE_WEBGL"`
}

// DefaultConfig returns a Config with schema version 1 and a 10-minute probe
// token TTL. Secret is still required (set it before use).
func DefaultConfig() Config { return Config{Version: 1, TokenTTL: 10 * time.Minute} }

// Validate reports whether the Config is usable.
func (c Config) Validate() error {
	switch {
	case c.Secret == "":
		return ErrNoSecret
	case c.Version <= 0:
		return ErrBadVersion
	case c.TokenTTL <= 0:
		return ErrBadTokenTTL
	}
	return nil
}
```

```go
// errors.go
package fingerprint

import "errors"

var (
	ErrNoSecret    = errors.New("fingerprint: config secret is required")
	ErrBadVersion  = errors.New("fingerprint: config version must be positive")
	ErrBadTokenTTL = errors.New("fingerprint: config token TTL must be positive")
	ErrBadToken    = errors.New("fingerprint: invalid or expired probe token")
)
```

```go
// fingerprint.go
package fingerprint

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Component is one named contribution to a fingerprint. Value is the in-request
// normalized raw value and may be PII; persist a Digest, not Components.
type Component struct{ Name, Value string }

// Fingerprint is a versioned, component-tagged identity for one request.
type Fingerprint struct {
	Version    int
	Components []Component
	Hash       string
	parts      map[string]string // name -> hex HMAC, filled at build time
}

// Digest is the persistable form: per-component HMACs and the combined hash, no
// raw PII. Store this and compare with Drift.
type Digest struct {
	Version int
	Parts   map[string]string
	Hash    string
}

// Digest returns the persistable digest of f.
func (f Fingerprint) Digest() Digest {
	return Digest{Version: f.Version, Parts: maps.Clone(f.parts), Hash: f.Hash}
}

// Drift returns the sorted names of components whose per-component hash differs
// between old and next (including components present in only one). Consumers
// weight the result: a "ua" bump is benign; a simultaneous "tls"+"ip" flip is not.
func Drift(old, next Digest) []string {
	changed := map[string]struct{}{}
	for name, h := range next.Parts {
		if old.Parts[name] != h {
			changed[name] = struct{}{}
		}
	}
	for name := range old.Parts {
		if _, ok := next.Parts[name]; !ok {
			changed[name] = struct{}{}
		}
	}
	out := slices.Collect(maps.Keys(changed))
	slices.Sort(out)
	return out
}

// combineHash computes per-component HMACs and a combined HMAC over the
// version + name/parthash pairs, with components sorted by name for stability.
func combineHash(secret []byte, version int, comps []Component) (string, map[string]string) {
	sorted := slices.Clone(comps)
	slices.SortFunc(sorted, func(a, b Component) int { return strings.Compare(a.Name, b.Name) })

	parts := make(map[string]string, len(sorted))
	for _, c := range sorted {
		m := hmac.New(sha256.New, secret)
		m.Write([]byte(c.Name))
		m.Write([]byte{0})
		m.Write([]byte(c.Value))
		parts[c.Name] = hex.EncodeToString(m.Sum(nil))
	}

	m := hmac.New(sha256.New, secret)
	m.Write([]byte(strconv.Itoa(version)))
	m.Write([]byte{0x1e})
	for _, c := range sorted {
		m.Write([]byte(c.Name))
		m.Write([]byte{0x1f})
		m.Write([]byte(parts[c.Name]))
		m.Write([]byte{0x1e})
	}
	return hex.EncodeToString(m.Sum(nil)), parts
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./web/fingerprint/ -run 'TestConfigValidate|TestDrift'`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/config.go web/fingerprint/errors.go web/fingerprint/fingerprint.go web/fingerprint/*_test.go
git commit -m "feat(fingerprint): identity types, keyed hashing, config"
```

---

### Task 2: Collector seam, seams, Fingerprinter, options

**Files:**
- Create: `web/fingerprint/seams.go`, `web/fingerprint/collector.go`, `web/fingerprint/options.go`
- Test: `web/fingerprint/collector_test.go`

**Interfaces:**
- Consumes: `Config`, `Component`, `Fingerprint`, `combineHash` (Task 1).
- Produces:
  - Seams: `GeoInfo{Continent, Timezone string; ASN uint; Hosting bool}`; `GeoLookup func(netip.Addr) (GeoInfo, bool)`; `Family int` with `FamilyUnknown`/`FamilyBrowser`/`FamilyBot`; `UAFamily func(ua string) (Family, bool)`.
  - `Collector interface { Collect(r *http.Request) ([]Component, error) }`.
  - `Fingerprinter` (holds `secret []byte`, `version int`, `cfg Config`, `cols []Collector`, `geo GeoLookup`, `ua UAFamily`, `store cache.Store`, `logger *slog.Logger`, `signer *sign.Signer`, `cookies *cookie.Codec`, `clock clock.Clock`).
  - `New(cfg Config, opts ...Option) (*Fingerprinter, error)`; `(*Fingerprinter).FromRequest(r *http.Request) (Fingerprint, error)`.
  - `Option func(*Fingerprinter)`; `WithCollectors(cs ...Collector) Option`, `WithGeoLookup(GeoLookup) Option`, `WithUAFamily(UAFamily) Option`, `WithStore(cache.Store) Option`, `WithLogger(*slog.Logger) Option`, `WithClock(clock.Clock) Option`.

- [ ] **Step 1: Write failing test**

```go
// collector_test.go
package fingerprint_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

type staticCollector []fingerprint.Component

func (s staticCollector) Collect(_ *interface{ Foo() }) {} // replaced below

func TestFromRequestHashesComponents(t *testing.T) {
	cfg := fingerprint.Config{Secret: "top-secret", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg, fingerprint.WithCollectors(
		fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
			return []fingerprint.Component{{Name: "ua", Value: "curl/8"}}, nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	f, err := fp.FromRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if f.Hash == "" || f.Version != 1 {
		t.Fatalf("bad fingerprint: %+v", f)
	}
	// Stable: same input -> same hash and non-empty digest parts.
	f2, _ := fp.FromRequest(r)
	if f.Hash != f2.Hash {
		t.Fatalf("unstable hash: %s vs %s", f.Hash, f2.Hash)
	}
	if _, ok := f.Digest().Parts["ua"]; !ok {
		t.Fatalf("digest missing ua part: %+v", f.Digest())
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	if _, err := fingerprint.New(fingerprint.Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
}
```

> Note: fix the test's imports (`net/http`) and delete the `staticCollector` stub — use the real `fingerprint.CollectorFunc` adapter defined below. The final test file must compile as black-box.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run TestFromRequest`
Expected: FAIL — `undefined: fingerprint.New` / `CollectorFunc`.

- [ ] **Step 3: Implement**

```go
// seams.go
package fingerprint

import "net/netip"

// GeoInfo is the subset of geoip facts the signal inspectors need. Wire it from
// web/geoip (see example_test.go). Covers datacenter-asn and geo-tz-mismatch.
type GeoInfo struct {
	Continent string // two-letter continent code ("EU", "NA", "AS", ...)
	Timezone  string // IANA zone of the IP ("Europe/Berlin"), optional
	ASN       uint
	Hosting   bool // datacenter/hosting/VPN ASN
}

// GeoLookup resolves a client IP to GeoInfo. ok is false on a clean miss.
type GeoLookup func(netip.Addr) (GeoInfo, bool)

// Family is a coarse client class inferred from the User-Agent.
type Family int

const (
	FamilyUnknown Family = iota
	FamilyBrowser
	FamilyBot
)

// UAFamily classifies a User-Agent string. Wire it from web/useragent.
type UAFamily func(ua string) (Family, bool)
```

```go
// collector.go
package fingerprint

import (
	"net/http"

	"github.com/dmitrymomot/forge/crypto/sign"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/cookie"
	"log/slog"
)

// Collector contributes zero or more named components from a request.
type Collector interface {
	Collect(r *http.Request) ([]Component, error)
}

// CollectorFunc adapts a function to a Collector.
type CollectorFunc func(r *http.Request) ([]Component, error)

func (f CollectorFunc) Collect(r *http.Request) ([]Component, error) { return f(r) }

// Fingerprinter assembles components from its collectors, hashes them into a
// Fingerprint, and derives Signals. Build it with New.
type Fingerprinter struct {
	secret  []byte
	signer  *sign.Signer
	cookies *cookie.Codec
	store   cache.Store
	geo     GeoLookup
	ua      UAFamily
	logger  *slog.Logger
	clock   clock.Clock
	cols    []Collector
	cfg     Config
	version int
}

// New validates cfg, builds the HMAC signer and signed-cookie codec, applies
// options, and returns a ready Fingerprinter.
func New(cfg Config, opts ...Option) (*Fingerprinter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	signer, err := sign.New([]byte(cfg.Secret))
	if err != nil {
		return nil, err
	}
	cookies, err := cookie.FromConfig(cookie.Config{Keys: cfg.Secret, SameSite: "lax", HTTPOnly: true, Secure: true})
	if err != nil {
		return nil, err
	}
	fp := &Fingerprinter{
		secret:  []byte(cfg.Secret),
		signer:  signer,
		cookies: cookies,
		logger:  slog.Default(),
		clock:   clock.System(),
		cfg:     cfg,
		version: cfg.Version,
	}
	for _, o := range opts {
		o(fp)
	}
	return fp, nil
}

// FromRequest runs every collector, concatenates their components, and hashes
// the result. A collector error is remembered (returned) but never aborts the
// others — best-effort fingerprinting.
func (fp *Fingerprinter) FromRequest(r *http.Request) (Fingerprint, error) {
	var comps []Component
	var firstErr error
	for _, c := range fp.cols {
		cc, err := c.Collect(r)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		comps = append(comps, cc...)
	}
	hash, parts := combineHash(fp.secret, fp.version, comps)
	return Fingerprint{Version: fp.version, Components: comps, Hash: hash, parts: parts}, firstErr
}
```

```go
// options.go
package fingerprint

import (
	"log/slog"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// Option configures a Fingerprinter in New.
type Option func(*Fingerprinter)

// WithCollectors appends layers (headers, IP, TLS, JS) to the Fingerprinter.
func WithCollectors(cs ...Collector) Option {
	return func(fp *Fingerprinter) { fp.cols = append(fp.cols, cs...) }
}

// WithGeoLookup wires the geoip seam, enabling datacenter-asn and geo-tz-mismatch.
func WithGeoLookup(fn GeoLookup) Option { return func(fp *Fingerprinter) { fp.geo = fn } }

// WithUAFamily wires the useragent seam, enabling bot-ua, tls-ua-mismatch, header-anomaly.
func WithUAFamily(fn UAFamily) Option { return func(fp *Fingerprinter) { fp.ua = fn } }

// WithStore backs JS-probe payload correlation with a cache.Store instead of a
// payload-carrying cookie (the cookie then carries only the nonce).
func WithStore(s cache.Store) Option { return func(fp *Fingerprinter) { fp.store = s } }

// WithLogger sets the logger for best-effort Debug messages. A nil logger is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(fp *Fingerprinter) {
		if l != nil {
			fp.logger = l
		}
	}
}

// WithClock overrides the clock used for probe-token expiry (tests inject a mock).
func WithClock(c clock.Clock) Option {
	return func(fp *Fingerprinter) {
		if c != nil {
			fp.clock = c
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/ -run 'TestFromRequest|TestNewRejects'`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/seams.go web/fingerprint/collector.go web/fingerprint/options.go web/fingerprint/collector_test.go
git commit -m "feat(fingerprint): collector seam, seams, Fingerprinter, options"
```

---

### Task 3: Headers collector

**Files:**
- Create: `web/fingerprint/headers.go`
- Test: `web/fingerprint/headers_test.go`

**Interfaces:**
- Consumes: `Component`, `Collector`.
- Produces: `Headers() Collector` emitting components `ua`, `accept`, `accept-language`, `accept-encoding` (only for headers that are present and non-empty).

- [ ] **Step 1: Write failing test**

```go
// headers_test.go
package fingerprint_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestHeadersCollector(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// Accept and Accept-Encoding absent.
	comps, err := fingerprint.Headers().Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range comps {
		got[c.Name] = c.Value
	}
	if got["ua"] != "Mozilla/5.0" || got["accept-language"] != "en-US,en;q=0.9" {
		t.Fatalf("unexpected components: %v", got)
	}
	if _, ok := got["accept"]; ok {
		t.Fatalf("absent header must not emit a component: %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run TestHeadersCollector`
Expected: FAIL — `undefined: fingerprint.Headers`.

- [ ] **Step 3: Implement**

```go
// headers.go
package fingerprint

import (
	"net/http"
	"strings"
)

// headerComponent maps a request header to a component name.
type headerPair struct{ header, name string }

var fingerprintHeaders = []headerPair{
	{"User-Agent", "ua"},
	{"Accept", "accept"},
	{"Accept-Language", "accept-language"},
	{"Accept-Encoding", "accept-encoding"},
}

type headersCollector struct{}

// Headers returns a Collector contributing the request's User-Agent and Accept*
// headers as components. Absent or blank headers contribute nothing.
func Headers() Collector { return headersCollector{} }

func (headersCollector) Collect(r *http.Request) ([]Component, error) {
	comps := make([]Component, 0, len(fingerprintHeaders))
	for _, h := range fingerprintHeaders {
		if v := strings.TrimSpace(r.Header.Get(h.header)); v != "" {
			comps = append(comps, Component{Name: h.name, Value: v})
		}
	}
	return comps, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/ -run TestHeadersCollector`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/headers.go web/fingerprint/headers_test.go
git commit -m "feat(fingerprint): headers collector"
```

---

### Task 4: ClientIP collector

**Files:**
- Create: `web/fingerprint/clientip.go`
- Test: `web/fingerprint/clientip_test.go`

**Interfaces:**
- Consumes: `Component`, `Collector`; `github.com/dmitrymomot/forge/web/clientip` (`Get`, `Resolve`, `Option`).
- Produces: `ClientIP(opts ...clientip.Option) Collector` emitting a single `ip` component (the resolved client IP) or nothing when unresolved.

- [ ] **Step 1: Write failing test**

```go
// clientip_test.go
package fingerprint_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestClientIPCollector(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	comps, err := fingerprint.ClientIP(clientip.RemoteAddrOnly()).Collect(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 || comps[0].Name != "ip" || comps[0].Value != "203.0.113.7" {
		t.Fatalf("unexpected: %+v", comps)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run TestClientIPCollector`
Expected: FAIL — `undefined: fingerprint.ClientIP`.

- [ ] **Step 3: Implement**

```go
// clientip.go
package fingerprint

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/clientip"
)

type ipCollector struct{ opts []clientip.Option }

// ClientIP returns a Collector contributing the resolved client IP as the "ip"
// component. With no options it uses clientip.Get (honoring an installed
// clientip.Middleware); with options it resolves via clientip.Resolve. A request
// whose IP cannot be resolved contributes nothing.
func ClientIP(opts ...clientip.Option) Collector { return ipCollector{opts: opts} }

func (c ipCollector) Collect(r *http.Request) ([]Component, error) {
	ip := clientip.Get(r)
	if len(c.opts) > 0 {
		ip = clientip.Resolve(r, c.opts...)
	}
	if ip == "" {
		return nil, nil
	}
	return []Component{{Name: "ip", Value: ip}}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/ -run TestClientIPCollector`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/clientip.go web/fingerprint/clientip_test.go
git commit -m "feat(fingerprint): client IP collector"
```

---

### Task 5: Signal framework + seam inspectors (datacenter-asn, bot-ua)

**Files:**
- Create: `web/fingerprint/signals.go`
- Test: `web/fingerprint/signals_test.go`

**Interfaces:**
- Consumes: `Fingerprint`, `Component`, `GeoLookup`, `UAFamily`, `Fingerprinter` (fields `geo`, `ua`).
- Produces: `Signal{Name string; Value bool; Detail string}`; `(*Fingerprinter).Signals(r *http.Request, f Fingerprint) []Signal`; unexported `componentIndex(comps []Component) map[string]string`. Inspectors emit `datacenter-asn` (needs `ip` component + `geo` seam) and `bot-ua` (needs `ua` component + `ua` seam). A signal is emitted only when its inputs are present, so an unwired layer yields no signal rather than a false negative.

- [ ] **Step 1: Write failing test**

```go
// signals_test.go
package fingerprint_test

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func signalByName(sigs []fingerprint.Signal, name string) (fingerprint.Signal, bool) {
	for _, s := range sigs {
		if s.Name == name {
			return s, true
		}
	}
	return fingerprint.Signal{}, false
}

func TestDatacenterAndBotSignals(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg,
		fingerprint.WithCollectors(fingerprint.Headers(), fingerprint.ClientIP(clientip.RemoteAddrOnly())),
		fingerprint.WithGeoLookup(func(ip netip.Addr) (fingerprint.GeoInfo, bool) {
			return fingerprint.GeoInfo{ASN: 16509, Hosting: true, Continent: "NA"}, true
		}),
		fingerprint.WithUAFamily(func(ua string) (fingerprint.Family, bool) {
			return fingerprint.FamilyBot, true
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("User-Agent", "Googlebot/2.1")
	f, _ := fp.FromRequest(r)
	sigs := fp.Signals(r, f)

	if s, ok := signalByName(sigs, "datacenter-asn"); !ok || !s.Value {
		t.Fatalf("datacenter-asn missing/false: %+v", sigs)
	}
	if s, ok := signalByName(sigs, "bot-ua"); !ok || !s.Value {
		t.Fatalf("bot-ua missing/false: %+v", sigs)
	}
}

func TestUnwiredSeamsEmitNoSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "curl/8")
	f, _ := fp.FromRequest(r)
	if _, ok := signalByName(fp.Signals(r, f), "datacenter-asn"); ok {
		t.Fatal("datacenter-asn should not emit without geo seam + ip")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run 'TestDatacenter|TestUnwired'`
Expected: FAIL — `fp.Signals undefined`.

- [ ] **Step 3: Implement**

```go
// signals.go
package fingerprint

import (
	"net/http"
	"net/netip"
)

// Signal is a structured anti-fraud fact derived from a Fingerprint. Value is
// the finding; Detail is a short human-readable explanation. Scoring is the
// consumer's job — this package only reports facts.
type Signal struct {
	Name   string
	Value  bool
	Detail string
}

func componentIndex(comps []Component) map[string]string {
	m := make(map[string]string, len(comps))
	for _, c := range comps {
		m[c.Name] = c.Value
	}
	return m
}

// Signals derives every signal whose required inputs (components and wired
// seams) are present. An unwired seam or absent layer simply omits its signals.
func (fp *Fingerprinter) Signals(r *http.Request, f Fingerprint) []Signal {
	comp := componentIndex(f.Components)
	var out []Signal
	if s, ok := fp.datacenterASN(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.botUA(comp); ok {
		out = append(out, s)
	}
	out = append(out, fp.componentSignals(r, comp)...) // Task 6
	return out
}

func (fp *Fingerprinter) datacenterASN(comp map[string]string) (Signal, bool) {
	if fp.geo == nil {
		return Signal{}, false
	}
	ip, err := netip.ParseAddr(comp["ip"])
	if err != nil {
		return Signal{}, false
	}
	info, ok := fp.geo(ip)
	if !ok {
		return Signal{}, false
	}
	detail := ""
	if info.Hosting {
		detail = "client IP is on a hosting/datacenter ASN"
	}
	return Signal{Name: "datacenter-asn", Value: info.Hosting, Detail: detail}, true
}

func (fp *Fingerprinter) botUA(comp map[string]string) (Signal, bool) {
	ua, has := comp["ua"]
	if !has || fp.ua == nil {
		return Signal{}, false
	}
	fam, ok := fp.ua(ua)
	if !ok {
		return Signal{}, false
	}
	return Signal{Name: "bot-ua", Value: fam == FamilyBot, Detail: ua}, true
}
```

> The `fp.componentSignals` call is implemented in Task 6. To compile Task 5 standalone, add a temporary stub `func (fp *Fingerprinter) componentSignals(_ *http.Request, _ map[string]string) []Signal { return nil }` in `signals.go`, then MOVE it to `signals_component.go` (replacing the stub) in Task 6.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/ -run 'TestDatacenter|TestUnwired'`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/signals.go web/fingerprint/signals_test.go
git commit -m "feat(fingerprint): signal framework + datacenter-asn/bot-ua inspectors"
```

---

### Task 6: Component inspectors (headless, tls-ua-mismatch, lang-mismatch, geo-tz-mismatch, header-anomaly)

**Files:**
- Create: `web/fingerprint/signals_component.go` (move the stub out of `signals.go`)
- Test: `web/fingerprint/signals_component_test.go`

**Interfaces:**
- Consumes: `Fingerprinter`, `GeoLookup`, `UAFamily`, `Family`.
- Produces: `(*Fingerprinter).componentSignals(r *http.Request, comp map[string]string) []Signal` emitting, when inputs present: `headless` (from `js-webdriver`), `tls-ua-mismatch` (from `tls` + `ua` + `ua` seam + embedded automation-JA4 set), `lang-mismatch` (`accept-language` vs `js-languages`), `geo-tz-mismatch` (`ip`+`geo` seam vs `js-timezone`), `header-anomaly` (`ua` seam Browser + missing modern-browser header markers). Unexported helpers `primaryLang(string) string`, `continentOfTZ(string) string`, package var `automationJA4 map[string]string`.

- [ ] **Step 1: Write failing test**

```go
// signals_component_test.go
package fingerprint_test

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestLangMismatchSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	// Inject JS components via a CollectorFunc simulating an ingested payload.
	js := fingerprint.CollectorFunc(func(_ *interface{}) ([]fingerprint.Component, error) { return nil, nil })
	_ = js
	fp, _ := fingerprint.New(cfg, fingerprint.WithCollectors(
		fingerprint.Headers(),
		fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
			return []fingerprint.Component{{Name: "js-languages", Value: "fr-FR,fr"}}, nil
		}),
	))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "lang-mismatch"); !ok || !s.Value {
		t.Fatalf("expected lang-mismatch=true: %+v", fp.Signals(r, f))
	}
}

func TestGeoTZMismatchSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg,
		fingerprint.WithCollectors(
			fingerprint.ClientIP(clientip.RemoteAddrOnly()),
			fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
				return []fingerprint.Component{{Name: "js-timezone", Value: "Asia/Tokyo"}}, nil
			}),
		),
		fingerprint.WithGeoLookup(func(_ netip.Addr) (fingerprint.GeoInfo, bool) {
			return fingerprint.GeoInfo{Continent: "EU"}, true
		}),
	)
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "geo-tz-mismatch"); !ok || !s.Value {
		t.Fatalf("expected geo-tz-mismatch=true: %+v", fp.Signals(r, f))
	}
}

func TestHeadlessSignal(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg, fingerprint.WithCollectors(
		fingerprint.CollectorFunc(func(_ *http.Request) ([]fingerprint.Component, error) {
			return []fingerprint.Component{{Name: "js-webdriver", Value: "true"}}, nil
		}),
	))
	r := httptest.NewRequest("GET", "/", nil)
	f, _ := fp.FromRequest(r)
	if s, ok := signalByName(fp.Signals(r, f), "headless"); !ok || !s.Value {
		t.Fatalf("expected headless=true")
	}
}
```

> Fix imports (`net/http`) and delete the dead `js` stub before running — it only illustrates the JS-component shape that Task 9's real `JS()` collector produces.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run 'TestLangMismatch|TestGeoTZ|TestHeadless'`
Expected: FAIL — mismatch/headless signals absent.

- [ ] **Step 3: Implement** (replace the Task 5 stub)

```go
// signals_component.go
package fingerprint

import (
	"net/http"
	"net/netip"
	"strings"
)

// automationJA4 maps well-known non-browser JA4 client fingerprints to a label.
// Pin values from the FoxIO JA4 reference vectors (github.com/FoxIO-LLC/ja4);
// bounded on purpose — an unmatched TLS hash simply does not fire the signal.
var automationJA4 = map[string]string{
	// Example placeholders to be pinned during implementation from captured
	// handshakes; keep only verified entries:
	// "t13d1516h2_8daaf6152771_02713d6af862": "chrome-like (allowed)",
}

func (fp *Fingerprinter) componentSignals(r *http.Request, comp map[string]string) []Signal {
	var out []Signal
	if v, ok := comp["js-webdriver"]; ok {
		out = append(out, Signal{Name: "headless", Value: v == "true", Detail: "navigator.webdriver"})
	}
	if s, ok := fp.tlsUAMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := langMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.geoTZMismatch(comp); ok {
		out = append(out, s)
	}
	if s, ok := fp.headerAnomaly(r, comp); ok {
		out = append(out, s)
	}
	return out
}

func (fp *Fingerprinter) tlsUAMismatch(comp map[string]string) (Signal, bool) {
	tls, hasTLS := comp["tls"]
	ua, hasUA := comp["ua"]
	if !hasTLS || !hasUA || fp.ua == nil {
		return Signal{}, false
	}
	fam, ok := fp.ua(ua)
	if !ok || fam != FamilyBrowser {
		return Signal{}, false
	}
	label, flagged := automationJA4[tls]
	return Signal{Name: "tls-ua-mismatch", Value: flagged, Detail: label}, true
}

func langMismatch(comp map[string]string) (Signal, bool) {
	hdr, hasHdr := comp["accept-language"]
	js, hasJS := comp["js-languages"]
	if !hasHdr || !hasJS {
		return Signal{}, false
	}
	mismatch := primaryLang(hdr) != "" && primaryLang(js) != "" && primaryLang(hdr) != primaryLang(js)
	return Signal{Name: "lang-mismatch", Value: mismatch, Detail: hdr + " vs " + js}, true
}

func (fp *Fingerprinter) geoTZMismatch(comp map[string]string) (Signal, bool) {
	if fp.geo == nil {
		return Signal{}, false
	}
	tz, hasTZ := comp["js-timezone"]
	ip, err := netip.ParseAddr(comp["ip"])
	if !hasTZ || err != nil {
		return Signal{}, false
	}
	info, ok := fp.geo(ip)
	if !ok || info.Continent == "" {
		return Signal{}, false
	}
	jsCont := continentOfTZ(tz)
	if jsCont == "" {
		return Signal{}, false
	}
	return Signal{Name: "geo-tz-mismatch", Value: jsCont != info.Continent, Detail: tz + " vs " + info.Continent}, true
}

// headerAnomaly fires when the UA claims a modern Chromium browser but the
// Client-Hints / Fetch-Metadata headers that browser always sends are absent.
func (fp *Fingerprinter) headerAnomaly(r *http.Request, comp map[string]string) (Signal, bool) {
	ua, hasUA := comp["ua"]
	if !hasUA || fp.ua == nil {
		return Signal{}, false
	}
	fam, ok := fp.ua(ua)
	if !ok || fam != FamilyBrowser {
		return Signal{}, false
	}
	if !strings.Contains(ua, "Chrome/") { // Client Hints are Chromium-specific
		return Signal{}, false
	}
	missing := r.Header.Get("Sec-Ch-Ua") == "" || r.Header.Get("Sec-Fetch-Site") == ""
	return Signal{Name: "header-anomaly", Value: missing, Detail: "Chrome UA without Sec-Ch-Ua/Sec-Fetch-*"}, true
}

// primaryLang returns the base language subtag of the first entry ("en-US,..." -> "en").
func primaryLang(v string) string {
	first, _, _ := strings.Cut(v, ",")
	first = strings.TrimSpace(first)
	if i := strings.IndexByte(first, ';'); i >= 0 {
		first = first[:i]
	}
	base, _, _ := strings.Cut(first, "-")
	return strings.ToLower(strings.TrimSpace(base))
}

// continentOfTZ maps an IANA zone's region prefix to a two-letter continent
// code, or "" when unknown or ambiguous (the America/ prefix spans NA and SA,
// so it is treated as unknown to avoid false positives).
func continentOfTZ(tz string) string {
	region, _, ok := strings.Cut(tz, "/")
	if !ok {
		return ""
	}
	switch region {
	case "Europe":
		return "EU"
	case "Africa":
		return "AF"
	case "Asia":
		return "AS"
	case "Australia", "Pacific":
		return "OC"
	case "Antarctica":
		return "AN"
	default: // America (ambiguous NA/SA), Atlantic, Indian, Etc, UTC...
		return ""
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/ -run 'TestLangMismatch|TestGeoTZ|TestHeadless'`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/signals.go web/fingerprint/signals_component.go web/fingerprint/signals_component_test.go
git commit -m "feat(fingerprint): headless/tls-ua/lang/geo-tz/header-anomaly inspectors"
```

---

### Task 7: Middleware, context accessor, LogExtractor

**Files:**
- Create: `web/fingerprint/middleware.go`
- Test: `web/fingerprint/middleware_test.go`

**Interfaces:**
- Consumes: `Fingerprinter.FromRequest`, `Fingerprinter.Signals`; `web/middleware.Middleware`, `core/ctxkey`, `ops/logger.ContextExtractor`.
- Produces: `Result{Fingerprint Fingerprint; Signals []Signal}`; `(*Fingerprinter).Middleware() middleware.Middleware`; `FromContext(ctx context.Context) (Result, bool)`; `LogExtractor logger.ContextExtractor`.

- [ ] **Step 1: Write failing test**

```go
// middleware_test.go
package fingerprint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestMiddlewareStashesResult(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg, fingerprint.WithCollectors(fingerprint.Headers()))
	var got fingerprint.Result
	var ok bool
	h := fp.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = fingerprint.FromContext(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !ok || got.Fingerprint.Hash == "" {
		t.Fatalf("result not stashed: ok=%v %+v", ok, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run TestMiddlewareStashes`
Expected: FAIL — `fp.Middleware undefined`.

- [ ] **Step 3: Implement**

```go
// middleware.go
package fingerprint

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/web/middleware"
)

// Result is the per-request fingerprint output cached by Middleware.
type Result struct {
	Fingerprint Fingerprint
	Signals     []Signal
}

var resultKey = ctxkey.New[Result]("fingerprint")

// Middleware computes the fingerprint and signals once per request and caches
// the Result in context for FromContext and LogExtractor. A collector error is
// logged at Debug; fingerprinting never fails the request.
func (fp *Fingerprinter) Middleware() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f, err := fp.FromRequest(r)
			if err != nil && fp.logger.Enabled(r.Context(), slog.LevelDebug) {
				fp.logger.DebugContext(r.Context(), "fingerprint: collector error", slog.Any("error", err))
			}
			res := Result{Fingerprint: f, Signals: fp.Signals(r, f)}
			next.ServeHTTP(w, r.WithContext(resultKey.With(r.Context(), res)))
		})
	}
}

// FromContext returns the Result cached by Middleware. ok reports whether the
// middleware ran.
func FromContext(ctx context.Context) (Result, bool) { return resultKey.From(ctx) }

// LogExtractor adds a "fingerprint" group (hash + comma-joined names of signals
// whose Value is true) when Middleware cached a Result. Wire it with
// logger.WithContextExtractors(fingerprint.LogExtractor).
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	res, ok := resultKey.From(ctx)
	if !ok || res.Fingerprint.Hash == "" {
		return slog.Attr{}, false
	}
	attrs := []any{slog.String("hash", res.Fingerprint.Hash)}
	var flagged []string
	for _, s := range res.Signals {
		if s.Value {
			flagged = append(flagged, s.Name)
		}
	}
	if len(flagged) > 0 {
		attrs = append(attrs, slog.String("signals", strings.Join(flagged, ",")))
	}
	return slog.Group("fingerprint", attrs...), true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/ -run TestMiddlewareStashes`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/middleware.go web/fingerprint/middleware_test.go
git commit -m "feat(fingerprint): middleware, context accessor, log extractor"
```

---

### Task 8: JS-probe token (issue + verify)

**Files:**
- Create: `web/fingerprint/jsprobe_token.go`
- Test: `web/fingerprint/jsprobe_token_test.go`

**Interfaces:**
- Consumes: `Fingerprinter` (`signer`, `secret`, `clock`, `cfg.TokenTTL`); `crypto/sign`, `core/clock`, `core/random`, `web/clientip`; `ErrBadToken`.
- Produces: `(*Fingerprinter).IssueToken(r *http.Request) (string, error)`; unexported `tokenClaims{Nonce string; Exp int64; IPHash string}`, `(*Fingerprinter).verifyToken(r *http.Request, token string) (tokenClaims, error)`, `(*Fingerprinter).ipHash(r *http.Request) string`. Token format: `base64url(claimsJSON) + "." + signer.SignString(base64url(claimsJSON))`.

- [ ] **Step 1: Write failing test**

```go
// jsprobe_token_test.go
package fingerprint_test

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestTokenRoundTrip(t *testing.T) {
	mock := clock.NewMock(time.Unix(1_700_000_000, 0))
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg, fingerprint.WithClock(mock))
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"

	tok, err := fp.IssueToken(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := fp.VerifyTokenForTest(r, tok); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	// Tamper: flip a character.
	if err := fp.VerifyTokenForTest(r, tok+"x"); !errors.Is(err, fingerprint.ErrBadToken) {
		t.Fatalf("tamper not rejected: %v", err)
	}
	// Expiry.
	mock.Advance(2 * time.Minute)
	if err := fp.VerifyTokenForTest(r, tok); !errors.Is(err, fingerprint.ErrBadToken) {
		t.Fatalf("expired token accepted: %v", err)
	}
}
```

> `VerifyTokenForTest` is an exported test-only shim over `verifyToken` — add it in an `export_test.go` file (`package fingerprint`) so the black-box test can reach the unexported method:
> ```go
> // export_test.go
> package fingerprint
> import "net/http"
> func (fp *Fingerprinter) VerifyTokenForTest(r *http.Request, tok string) error {
> 	_, err := fp.verifyToken(r, tok)
> 	return err
> }
> ```
> Verify `clock.NewMock`/`Advance` exist (`grep -n "func NewMock\|func.*Advance" core/clock/*.go`); if the mock API differs, adapt to it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run TestTokenRoundTrip`
Expected: FAIL — `fp.IssueToken undefined`.

- [ ] **Step 3: Implement**

```go
// jsprobe_token.go
package fingerprint

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/web/clientip"
)

type tokenClaims struct {
	Nonce  string `json:"n"`
	Exp    int64  `json:"e"`
	IPHash string `json:"i"`
}

// IssueToken returns a short-lived signed token binding a fresh nonce, an expiry
// (now + TokenTTL), and a hash of the client IP. Embed it in the page so the JS
// probe can echo it to IngestHandler.
func (fp *Fingerprinter) IssueToken(r *http.Request) (string, error) {
	claims := tokenClaims{
		Nonce:  random.String(16),
		Exp:    fp.clock.Now().Add(fp.cfg.TokenTTL).Unix(),
		IPHash: fp.ipHash(r),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + fp.signer.SignString(payload), nil
}

func (fp *Fingerprinter) verifyToken(r *http.Request, token string) (tokenClaims, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || !fp.signer.VerifyString(payload, sig) {
		return tokenClaims{}, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return tokenClaims{}, ErrBadToken
	}
	var claims tokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return tokenClaims{}, ErrBadToken
	}
	if fp.clock.Now().Unix() > claims.Exp {
		return tokenClaims{}, ErrBadToken
	}
	if !hmac.Equal([]byte(claims.IPHash), []byte(fp.ipHash(r))) {
		return tokenClaims{}, ErrBadToken
	}
	return claims, nil
}

// ipHash is a keyed, non-reversible hash of the client IP, used to bind a token
// to the requester without storing the raw address.
func (fp *Fingerprinter) ipHash(r *http.Request) string {
	m := hmac.New(sha256.New, fp.secret)
	m.Write([]byte(clientip.Get(r)))
	return hex.EncodeToString(m.Sum(nil)[:8])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/ -run TestTokenRoundTrip`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/jsprobe_token.go web/fingerprint/jsprobe_token_test.go web/fingerprint/export_test.go
git commit -m "feat(fingerprint): signed JS-probe token issue/verify"
```

---

### Task 9: JS ingest handler, script handler, JS collector, probe.js

**Files:**
- Create: `web/fingerprint/jsprobe.go`, `web/fingerprint/assets/probe.js`
- Test: `web/fingerprint/jsprobe_test.go`

**Interfaces:**
- Consumes: `Fingerprinter` (`cookies`, `store`, `cfg`, `verifyToken`, `clock`); `Component`, `Collector`; `resilience/cache` (`WithTTL` SetOption); `web/cookie`.
- Produces: `(*Fingerprinter).ScriptHandler() http.Handler`; `(*Fingerprinter).IngestHandler() http.Handler`; `(*Fingerprinter).ProbeSRI() string`; `JS() Collector`; unexported `probePayload` struct + `normalizeProbe`, `cookieName = "fpjs"`. Emits JS components: `js-timezone`, `js-languages`, `js-platform`, `js-webdriver`, and (when present) `js-canvas`, `js-webgl`.

- [ ] **Step 1: Create the embedded probe script**

```javascript
// assets/probe.js
// forge web/fingerprint browser probe. Reads window.__fp = {token, url, canvas, webgl}
// set by the page, collects a minimal high-signal payload, and POSTs it once.
(function () {
  var cfg = window.__fp || {};
  if (!cfg.token || !cfg.url) return;
  var d = {
    timezone: (Intl.DateTimeFormat().resolvedOptions().timeZone) || "",
    languages: (navigator.languages || []).slice(0, 10),
    platform: navigator.platform || "",
    hardwareConcurrency: navigator.hardwareConcurrency || 0,
    webdriver: navigator.webdriver === true
  };
  if (cfg.canvas) {
    try {
      var c = document.createElement("canvas");
      var g = c.getContext("2d");
      g.textBaseline = "top";
      g.font = "14px 'Arial'";
      g.fillText("forge-fp", 2, 2);
      d.canvas = c.toDataURL().slice(-64);
    } catch (e) {}
  }
  if (cfg.webgl) {
    try {
      var gl = document.createElement("canvas").getContext("webgl");
      var ext = gl.getExtension("WEBGL_debug_renderer_info");
      d.webgl = ext ? String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL)).slice(0, 64) : "";
    } catch (e) {}
  }
  try {
    fetch(cfg.url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: cfg.token, data: d }),
      keepalive: true
    });
  } catch (e) {}
})();
```

- [ ] **Step 2: Write failing test**

```go
// jsprobe_test.go
package fingerprint_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestScriptHandlerServesJS(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg)
	rec := httptest.NewRecorder()
	fp.ScriptHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/_fp/probe.js", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "window.__fp") {
		t.Fatalf("script not served: %d", rec.Code)
	}
	if !strings.HasPrefix(fp.ProbeSRI(), "sha384-") {
		t.Fatalf("bad SRI: %s", fp.ProbeSRI())
	}
}

func TestIngestThenCollect(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg)
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	tok, _ := fp.IssueToken(r)

	body, _ := json.Marshal(map[string]any{
		"token": tok,
		"data":  map[string]any{"timezone": "Asia/Tokyo", "languages": []string{"ja-JP", "ja"}, "webdriver": true},
	})
	ingReq := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
	ingReq.RemoteAddr = "203.0.113.7:5555"
	ingRec := httptest.NewRecorder()
	fp.IngestHandler().ServeHTTP(ingRec, ingReq)
	if ingRec.Code != http.StatusNoContent {
		t.Fatalf("ingest failed: %d", ingRec.Code)
	}

	// Replay the Set-Cookie onto a new request and collect.
	next := httptest.NewRequest("GET", "/", nil)
	for _, c := range ingRec.Result().Cookies() {
		next.AddCookie(c)
	}
	comps, err := fingerprint.JS().Collect(next)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range comps {
		got[c.Name] = c.Value
	}
	if got["js-timezone"] != "Asia/Tokyo" || got["js-webdriver"] != "true" {
		t.Fatalf("collected wrong payload: %v", got)
	}
}

func TestIngestRejectsBadToken(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg)
	body, _ := json.Marshal(map[string]any{"token": "bogus.sig", "data": map[string]any{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
	fp.IngestHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./web/fingerprint/ -run 'TestScriptHandler|TestIngest'`
Expected: FAIL — `fp.ScriptHandler undefined`.

- [ ] **Step 4: Implement**

```go
// jsprobe.go
package fingerprint

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmitrymomot/forge/resilience/cache"
)

//go:embed assets/probe.js
var probeJS []byte

const cookieName = "fpjs"

// probePayload is the whitelisted, clamped shape accepted from the browser.
type probePayload struct {
	Timezone            string   `json:"timezone"`
	Languages           []string `json:"languages"`
	Platform            string   `json:"platform"`
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	WebDriver           bool     `json:"webdriver"`
	Canvas              string   `json:"canvas"`
	WebGL               string   `json:"webgl"`
}

func clampStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func normalizeProbe(p probePayload) probePayload {
	p.Timezone = clampStr(p.Timezone, 64)
	p.Platform = clampStr(p.Platform, 40)
	p.Canvas = clampStr(p.Canvas, 64)
	p.WebGL = clampStr(p.WebGL, 64)
	if p.HardwareConcurrency < 0 || p.HardwareConcurrency > 1024 {
		p.HardwareConcurrency = 0
	}
	if len(p.Languages) > 10 {
		p.Languages = p.Languages[:10]
	}
	for i := range p.Languages {
		p.Languages[i] = clampStr(p.Languages[i], 20)
	}
	return p
}

// ProbeSRI returns the "sha384-..." Subresource Integrity value of the served
// probe.js, for the consumer's <script integrity> / CSP.
func (fp *Fingerprinter) ProbeSRI() string {
	sum := sha256.Sum256(probeJS) // replaced below with sha512.Sum384 — see note
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// ScriptHandler serves the embedded probe.js with a long immutable cache and an
// ETag equal to the SRI value.
func (fp *Fingerprinter) ScriptHandler() http.Handler {
	sri := fp.ProbeSRI()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", strconv.Quote(sri))
		_, _ = w.Write(probeJS)
	})
}

// IngestHandler verifies the probe token, whitelists+clamps the payload, and
// persists it (cookie by default, or the cache.Store when WithStore is set) so
// the JS() collector can merge it on subsequent requests.
func (fp *Fingerprinter) IngestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var in struct {
			Token string       `json:"token"`
			Data  probePayload `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		claims, err := fp.verifyToken(r, in.Token)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		payload, _ := json.Marshal(normalizeProbe(in.Data))
		if fp.store != nil {
			if err := fp.store.Set(r.Context(), storeKey(claims.Nonce), payload, cache.WithTTL(fp.cfg.TokenTTL)); err != nil {
				http.Error(w, "store error", http.StatusInternalServerError)
				return
			}
			_ = fp.cookies.SetSigned(w, cookieName, claims.Nonce)
		} else {
			_ = fp.cookies.SetSigned(w, cookieName, base64.RawURLEncoding.EncodeToString(payload))
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func storeKey(nonce string) string { return "fpjs:" + nonce }

type jsCollector struct{ fp *Fingerprinter }

// JS returns a Collector that merges the ingested probe payload. It requires the
// same Fingerprinter that served IngestHandler (for the cookie codec + store);
// obtain it via fp.JSCollector — see below.
func (fp *Fingerprinter) JSCollector() Collector { return jsCollector{fp: fp} }

func (c jsCollector) Collect(r *http.Request) ([]Component, error) {
	raw, ok := c.fp.readProbe(r)
	if !ok {
		return nil, nil
	}
	var p probePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil
	}
	comps := []Component{
		{Name: "js-webdriver", Value: strconv.FormatBool(p.WebDriver)},
	}
	if p.Timezone != "" {
		comps = append(comps, Component{Name: "js-timezone", Value: p.Timezone})
	}
	if len(p.Languages) > 0 {
		comps = append(comps, Component{Name: "js-languages", Value: strings.Join(p.Languages, ",")})
	}
	if p.Platform != "" {
		comps = append(comps, Component{Name: "js-platform", Value: p.Platform})
	}
	if p.Canvas != "" {
		comps = append(comps, Component{Name: "js-canvas", Value: p.Canvas})
	}
	if p.WebGL != "" {
		comps = append(comps, Component{Name: "js-webgl", Value: p.WebGL})
	}
	return comps, nil
}

func (fp *Fingerprinter) readProbe(r *http.Request) ([]byte, bool) {
	v, err := fp.cookies.GetSigned(r, cookieName)
	if err != nil || v == "" {
		return nil, false
	}
	if fp.store != nil {
		raw, err := fp.store.Get(r.Context(), storeKey(v))
		if err != nil || len(raw) == 0 {
			return nil, false
		}
		return raw, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return nil, false
	}
	return raw, true
}

var _ = context.Background // keep import if unused after edits
```

> **Two required fixes while implementing:**
> 1. `ProbeSRI` must use SHA-384, not SHA-256 — import `crypto/sha512` and use `sha512.Sum384(probeJS)`. The stub above uses `sha256` only to keep the snippet importable; switch it and drop the `crypto/sha256` import.
> 2. The plan's earlier interface names the collector `JS()`, but it needs the Fingerprinter's cookie codec/store, so the real constructor is the method `(*Fingerprinter).JSCollector()`. Update the test to call `fp.JSCollector().Collect(next)` instead of `fingerprint.JS().Collect(next)`, and update Task 8/10 references (`Antifraud` uses `fp` after construction — see Task 10 note). Remove the `context` keep-alive line once imports settle.

- [ ] **Step 5: Run tests, fmt, commit**

Run: `go test ./web/fingerprint/ -run 'TestScriptHandler|TestIngest'` → PASS. Then `just fmt ./web/fingerprint/...`.

```bash
git add web/fingerprint/jsprobe.go web/fingerprint/assets/probe.js web/fingerprint/jsprobe_test.go
git commit -m "feat(fingerprint): JS probe script, ingest handler, JS collector"
```

---

### Task 10: Presets, doc.go, adapter examples

**Files:**
- Create: `web/fingerprint/presets.go`, `web/fingerprint/doc.go`, `web/fingerprint/example_test.go`
- Test: covered by `example_test.go` (runnable Examples) + a preset unit test in `presets_test.go`

**Interfaces:**
- Consumes: `New`, `WithCollectors`, `WithGeoLookup`, `WithUAFamily`, `Headers`, `ClientIP`, `JSCollector`.
- Produces: `Session(cfg Config, opts ...Option) (*Fingerprinter, error)`; `Antifraud(cfg Config, geo GeoLookup, ua UAFamily, tls Collector, opts ...Option) (*Fingerprinter, error)`.

> Note: `Antifraud` must add the JS collector, but `JSCollector()` is a method on the built `*Fingerprinter`. Build in two phases: construct with the non-JS collectors + seams, then append `fp.JSCollector()` to `fp.cols` before returning. Expose a tiny internal helper `(*Fingerprinter).addCollectors(cs ...Collector)` for this.

- [ ] **Step 1: Write failing test**

```go
// presets_test.go
package fingerprint_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func TestSessionPreset(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.Session(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	f, _ := fp.FromRequest(r)
	if f.Hash == "" {
		t.Fatal("session preset produced no fingerprint")
	}
}

func TestAntifraudPresetHasJSAndSeams(t *testing.T) {
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.Antifraud(cfg,
		func(_ fingerprint.GeoLookupArg) {}, nil, nil) // compile check only — see real signature
	_ = fp
	_ = err
}
```

> Replace `TestAntifraudPresetHasJSAndSeams` with a real call once signatures are in: pass a `GeoLookup`, a `UAFamily`, and `nil` for the TLS collector, then assert `fp.FromRequest` works and that issuing a token + ingesting makes a `js-*` component appear (reuse the Task 9 ingest→collect flow through the preset's Fingerprinter). Delete the bogus `GeoLookupArg` placeholder.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/ -run TestSessionPreset`
Expected: FAIL — `undefined: fingerprint.Session`.

- [ ] **Step 3: Implement**

```go
// presets.go
package fingerprint

// Session returns a Fingerprinter for general apps: header components only, pure
// stdlib, no wired seams. Suitable for session-hijack detection (persist the
// Digest, compare with Drift).
func Session(cfg Config, opts ...Option) (*Fingerprinter, error) {
	base := []Option{WithCollectors(Headers())}
	return New(cfg, append(base, opts...)...)
}

// Antifraud returns a full-stack Fingerprinter: headers + client IP + JS probe,
// with the geoip and useragent seams wired and an optional TLS collector (pass
// tlsprint.Chain(...), or nil to omit the TLS layer).
func Antifraud(cfg Config, geo GeoLookup, ua UAFamily, tls Collector, opts ...Option) (*Fingerprinter, error) {
	cols := []Collector{Headers(), ClientIP()}
	if tls != nil {
		cols = append(cols, tls)
	}
	base := []Option{WithCollectors(cols...), WithGeoLookup(geo), WithUAFamily(ua)}
	fp, err := New(cfg, append(base, opts...)...)
	if err != nil {
		return nil, err
	}
	fp.cols = append(fp.cols, fp.JSCollector())
	return fp, nil
}
```

```go
// doc.go
// Package fingerprint turns an HTTP request into a versioned identity plus
// structured anti-fraud signals, from headers alone up to a full TLS + JS device
// probe. Layers are opt-in Collectors; heavy lookups (geoip, useragent) enter
// through wired function seams so the core stays stdlib-light. Output is facts,
// never a score — weighting signals into a decision is the consumer's policy.
package fingerprint
```

```go
// example_test.go — runnable, and shows wiring the geoip/useragent seams
// (imported here in the external test package only).
package fingerprint_test

import (
	"fmt"
	"net/http/httptest"
	"net/netip"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
	"github.com/dmitrymomot/forge/web/useragent"
)

func ExampleAntifraud() {
	uaSeam := func(ua string) (fingerprint.Family, bool) {
		parsed := useragent.Parse(ua)
		if parsed.IsBot() {
			return fingerprint.FamilyBot, true
		}
		return fingerprint.FamilyBrowser, true
	}
	geoSeam := func(_ netip.Addr) (fingerprint.GeoInfo, bool) {
		return fingerprint.GeoInfo{Continent: "NA", Hosting: false}, true
	}
	cfg := fingerprint.Config{Secret: "example-secret", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.Antifraud(cfg, geoSeam, uaSeam, nil)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Googlebot/2.1")
	f, _ := fp.FromRequest(r)
	for _, s := range fp.Signals(r, f) {
		if s.Name == "bot-ua" {
			fmt.Println("bot-ua:", s.Value)
		}
	}
	// Output: bot-ua: true
}
```

> The `example_test.go` importing `web/useragent` proves seam wiring without making the core package depend on it (test deps ≠ package deps). Add a geoip adapter example similarly if desired (import `web/geoip`, map `Location` → `GeoInfo`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./web/fingerprint/ -run 'TestSessionPreset|TestAntifraud|Example'`
Expected: PASS. Then `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/presets.go web/fingerprint/doc.go web/fingerprint/example_test.go web/fingerprint/presets_test.go
git commit -m "feat(fingerprint): Session/Antifraud presets, doc, seam examples"
```

---

### Task 11: tlsprint — trust gate + header sources + Chain

**Files:**
- Create: `web/fingerprint/tlsprint/tlsprint.go`
- Test: `web/fingerprint/tlsprint/tlsprint_test.go`

**Interfaces:**
- Consumes: `fingerprint.Collector`, `fingerprint.Component`; `web/clientip.PrivateRanges`.
- Produces: `TrustFunc func(*http.Request) bool`; `TrustPrivateProxies() TrustFunc`; `TrustRanges(cidrs ...string) TrustFunc`; `CloudflareJA3(trusted TrustFunc) fingerprint.Collector`; `CloudFrontJA4(trusted TrustFunc) fingerprint.Collector`; `Header(name string, trusted TrustFunc) fingerprint.Collector`; `Chain(cs ...fingerprint.Collector) fingerprint.Collector` (first non-empty `tls` component wins).

- [ ] **Step 1: Write failing test**

```go
// tlsprint_test.go
package tlsprint_test

import (
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/fingerprint/tlsprint"
)

func TestHeaderSourceTrustGate(t *testing.T) {
	trustAll := tlsprint.TrustFunc(func(_ *interface{ H() }) bool { return true })
	_ = trustAll // replaced with real signature below

	// Untrusted -> component dropped.
	src := tlsprint.CloudflareJA3(func(_ *http.Request) bool { return false })
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cf-Ja3-Hash", "abc123")
	comps, _ := src.Collect(r)
	if len(comps) != 0 {
		t.Fatalf("untrusted header must be dropped: %+v", comps)
	}

	// Trusted -> emits tls component.
	src = tlsprint.CloudflareJA3(func(_ *http.Request) bool { return true })
	comps, _ = src.Collect(r)
	if len(comps) != 1 || comps[0].Name != "tls" || comps[0].Value != "abc123" {
		t.Fatalf("trusted header not emitted: %+v", comps)
	}
}
```

> Delete the `trustAll` illustrative line and fix the `net/http` import before running.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/tlsprint/ -run TestHeaderSourceTrustGate`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

```go
// tlsprint.go
package tlsprint

import (
	"net/http"
	"net/netip"
	"strings"

	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

// TrustFunc reports whether a request provably transited a trusted proxy, so an
// upstream-set JA hash header may be believed.
type TrustFunc func(*http.Request) bool

// TrustPrivateProxies trusts requests whose immediate peer (RemoteAddr) is in a
// private/loopback/CGNAT range — the standard "app behind a reverse proxy on a
// private network" setup.
func TrustPrivateProxies() TrustFunc {
	ranges := clientip.PrivateRanges()
	return func(r *http.Request) bool { return peerInRanges(r, ranges) }
}

// TrustRanges trusts requests whose immediate peer is in one of the CIDRs. It
// panics on an invalid CIDR (call at startup).
func TrustRanges(cidrs ...string) TrustFunc {
	ranges := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			panic("tlsprint: TrustRanges: invalid CIDR " + c + ": " + err.Error())
		}
		ranges = append(ranges, p)
	}
	return func(r *http.Request) bool { return peerInRanges(r, ranges) }
}

func peerInRanges(r *http.Request, ranges []netip.Prefix) bool {
	host := r.RemoteAddr
	if h, _, ok := strings.Cut(host, ":"); ok {
		// Handle IPv6 "[::1]:port" and IPv4 "1.2.3.4:port".
		host = strings.Trim(h, "[]")
		if strings.Contains(r.RemoteAddr, "]") {
			host = r.RemoteAddr[1:strings.LastIndex(r.RemoteAddr, "]")]
		}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, p := range ranges {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

type headerSource struct {
	header  string
	trusted TrustFunc
}

// CloudflareJA3 reads the Cf-Ja3-Hash header when trusted.
func CloudflareJA3(trusted TrustFunc) fingerprint.Collector {
	return headerSource{header: "Cf-Ja3-Hash", trusted: trusted}
}

// CloudFrontJA4 reads the CloudFront-Viewer-JA4-Fingerprint header when trusted.
func CloudFrontJA4(trusted TrustFunc) fingerprint.Collector {
	return headerSource{header: "CloudFront-Viewer-JA4-Fingerprint", trusted: trusted}
}

// Header reads an arbitrary upstream TLS-fingerprint header (Envoy/Caddy/Traefik)
// when trusted.
func Header(name string, trusted TrustFunc) fingerprint.Collector {
	return headerSource{header: name, trusted: trusted}
}

func (s headerSource) Collect(r *http.Request) ([]fingerprint.Component, error) {
	if s.trusted != nil && !s.trusted(r) {
		return nil, nil
	}
	v := strings.TrimSpace(r.Header.Get(s.header))
	if v == "" {
		return nil, nil
	}
	return []fingerprint.Component{{Name: "tls", Value: v}}, nil
}

type chain struct{ cs []fingerprint.Collector }

// Chain returns a Collector that queries each TLS source in order and returns
// the first non-empty "tls" component.
func Chain(cs ...fingerprint.Collector) fingerprint.Collector { return chain{cs: cs} }

func (c chain) Collect(r *http.Request) ([]fingerprint.Component, error) {
	var firstErr error
	for _, s := range c.cs {
		comps, err := s.Collect(r)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(comps) > 0 {
			return comps, nil
		}
	}
	return nil, firstErr
}
```

> Simplify `peerInRanges` host parsing with `net.SplitHostPort` (stdlib) instead of the manual cutting shown — the manual version is deliberately explicit but `net.SplitHostPort` is cleaner and correct for IPv6. Use it: `host, _, err := net.SplitHostPort(r.RemoteAddr); if err != nil { host = r.RemoteAddr }`.

- [ ] **Step 4: Run test, fmt, commit**

Run: `go test ./web/fingerprint/tlsprint/ -run TestHeaderSourceTrustGate` → PASS. `just fmt ./web/fingerprint/...`.

```bash
git add web/fingerprint/tlsprint/tlsprint.go web/fingerprint/tlsprint/tlsprint_test.go
git commit -m "feat(tlsprint): trust gate, header sources, chain"
```

---

### Task 12: tlsprint — ClientHello parser + JA4 assembly

**Files:**
- Create: `web/fingerprint/tlsprint/clienthello.go`, `web/fingerprint/tlsprint/ja4.go`
- Test: `web/fingerprint/tlsprint/clienthello_test.go`, `web/fingerprint/tlsprint/ja4_test.go`

**Interfaces:**
- Produces: unexported `clientHello{version uint16; sni bool; ciphers []uint16; extensions []uint16; alpn []string; sigAlgs []uint16}`; `parseClientHello(record []byte) (clientHello, error)` (GREASE-stripped cipher/extension lists); `ja4(h clientHello) string`; sentinel `errShortHello`.

**JA4 construction reference** (FoxIO JA4 spec): `ja4 = a_"_"_b_"_"_c` where
- `a` = `t` (TLS) + 2-digit version (`13`/`12`/`11`/`10`) + `d` if SNI else `i` + 2-digit cipher count (capped 99) + 2-digit extension count (capped 99) + first-ALPN first+last char (or `00`).
- `b` = first 12 hex of `sha256(hex-of-sorted-ciphers joined by ",")`.
- `c` = first 12 hex of `sha256(hex-of-sorted-extensions joined by "," + "_" + hex-of-signature-algorithms-in-order joined by ",")`.
- GREASE values (where `v & 0x0f0f == 0x0a0a`) are removed from cipher and extension lists before counting/sorting; SNI (0x0000) and ALPN (0x0010) extensions are excluded from the sorted extension list per JA4 spec.

- [ ] **Step 1: Write failing tests**

```go
// ja4_test.go
package tlsprint

import (
	"strings"
	"testing"
)

func TestJA4Assembly(t *testing.T) {
	h := clientHello{
		version:    0x0304, // TLS 1.3
		sni:        true,
		ciphers:    []uint16{0x1301, 0x1302, 0x1303},
		extensions: []uint16{0x0005, 0x000a, 0x000b, 0x0023},
		alpn:       []string{"h2"},
		sigAlgs:    []uint16{0x0403, 0x0804},
	}
	got := ja4(h)
	// a = t13 d 03 04 h2  (3 ciphers, 4 extensions, alpn h2)
	if !strings.HasPrefix(got, "t13d0304h2_") {
		t.Fatalf("JA4_a wrong: %s", got)
	}
	parts := strings.Split(got, "_")
	if len(parts) != 3 || len(parts[1]) != 12 || len(parts[2]) != 12 {
		t.Fatalf("JA4 shape wrong: %q", got)
	}
	if ja4(h) != got {
		t.Fatal("JA4 not deterministic")
	}
}

func TestGreaseStripping(t *testing.T) {
	h := clientHello{version: 0x0304, sni: false,
		ciphers:    []uint16{0x0a0a, 0x1301}, // 0x0a0a is GREASE
		extensions: []uint16{0x1a1a, 0x0005},
	}
	// After stripping GREASE: 1 cipher, 1 extension -> a = t13 i 01 01 00
	if !strings.HasPrefix(ja4(h), "t13i010100_") {
		t.Fatalf("grease not stripped / counts wrong: %s", ja4(h))
	}
}
```

```go
// clienthello_test.go — parser tested against a ClientHello assembled from known
// fields (correct-by-construction, no magic hex blob).
package tlsprint

import "testing"

// buildHello assembles a minimal but valid TLS record framing a ClientHello with
// the given ciphers and an SNI + supported_versions(TLS1.3) extension.
func buildHello(ciphers []uint16, withSNI bool) []byte {
	// ... implement per the TLS 1.2/1.3 ClientHello wire format:
	//  record: 0x16 0x03 0x01 <len16>
	//  handshake: 0x01 <len24>
	//  client_version 0x0303, random[32], session_id(0), cipher_suites, comp(1,0),
	//  extensions( optional SNI 0x0000, supported_versions 0x002b -> 0x0304 )
	// Build with append + big-endian length patching. Return the full record.
	return nil // implement in this step
}

func TestParseClientHelloExtractsFields(t *testing.T) {
	rec := buildHello([]uint16{0x1301, 0x1302}, true)
	h, err := parseClientHello(rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ciphers) != 2 || h.ciphers[0] != 0x1301 {
		t.Fatalf("ciphers wrong: %v", h.ciphers)
	}
	if !h.sni {
		t.Fatal("SNI not detected")
	}
	if h.version != 0x0304 {
		t.Fatalf("version wrong: %#x", h.version)
	}
}
```

> Implement `buildHello` fully in this step (it is the parser's executable spec). If hand-assembling the record is error-prone, generate it once instead: run Go's `crypto/tls` client against a `net.Pipe`, capture the first record bytes the client writes, and paste them as a hex constant with a comment naming the Go version — but prefer `buildHello` so the test documents the wire layout.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/fingerprint/tlsprint/ -run 'TestJA4|TestGrease|TestParseClientHello'`
Expected: FAIL — `undefined: clientHello`.

- [ ] **Step 3: Implement** `clienthello.go` (record framing + field extraction, GREASE strip, bounds-checked) and `ja4.go` (the assembly per the reference above, using `crypto/sha256` + `encoding/hex`, `slices.Sort` for cipher/extension ordering). Guard every slice read against truncation and return `errShortHello` on any out-of-bounds — this parser reads attacker-influenced bytes and MUST NOT panic (it will be fuzzed in Task 14).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./web/fingerprint/tlsprint/ -run 'TestJA4|TestGrease|TestParseClientHello'`
Expected: PASS. `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/tlsprint/clienthello.go web/fingerprint/tlsprint/ja4.go web/fingerprint/tlsprint/*_test.go
git commit -m "feat(tlsprint): ClientHello parser + JA4 assembly"
```

---

### Task 13: tlsprint — Listener, Conn, ConnContext, Local

**Files:**
- Create: `web/fingerprint/tlsprint/listener.go`, `web/fingerprint/tlsprint/doc.go`
- Test: `web/fingerprint/tlsprint/listener_test.go`

**Interfaces:**
- Consumes: `parseClientHello`, `ja4`, `fingerprint.Collector`, `core/ctxkey`.
- Produces: `Listener(inner net.Listener) net.Listener`; `*Conn` (wraps `net.Conn`, peeks the ClientHello on first Read, computes JA4); `ConnContext(ctx context.Context, c net.Conn) context.Context`; `Local() fingerprint.Collector` (reads the JA4 stashed by ConnContext from `r.Context()`).

- [ ] **Step 1: Write failing test** (real in-process TLS handshake through the wrapper)

```go
// listener_test.go
package tlsprint_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint/tlsprint"
)

func TestLocalCapturesJA4(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := tlsprint.Listener(ln)

	cert := testCert(t) // helper: generate a self-signed cert (crypto/tls + crypto/x509)
	srv := &http.Server{
		TLSConfig:   &tls.Config{Certificates: []tls.Certificate{cert}},
		ConnContext: tlsprint.ConnContext,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			comps, _ := tlsprint.Local().Collect(r)
			if len(comps) == 1 && comps[0].Name == "tls" && strings.HasPrefix(comps[0].Value, "t1") {
				w.WriteHeader(200)
				return
			}
			w.WriteHeader(500)
		}),
	}
	go srv.ServeTLS(wrapped, "", "")
	defer srv.Close()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: 3 * time.Second}
	resp, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("JA4 not captured, status %d", resp.StatusCode)
	}
	_ = context.Background
}
```

> Implement `testCert(t)` with `crypto/x509` + `crypto/ecdsa` self-signed generation (there are existing examples in the repo — `grep -rl "x509.CreateCertificate" --include=*_test.go` to copy the pattern, e.g. from `web/autocert` or `crypto` tests).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./web/fingerprint/tlsprint/ -run TestLocalCapturesJA4`
Expected: FAIL — `undefined: tlsprint.Listener`.

- [ ] **Step 3: Implement**

```go
// listener.go
package tlsprint

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

var connKey = ctxkey.New[*Conn]("tlsprint-conn")

// Conn wraps an accepted connection, peeks the TLS ClientHello on the first Read
// (before the tls.Server handshake consumes it), computes its JA4, and then
// replays the buffered bytes transparently.
type Conn struct {
	net.Conn
	once   sync.Once
	prefix *bytes.Reader
	mu     sync.RWMutex
	ja4    string
}

type listener struct{ net.Listener }

// Listener wraps ln so each accepted connection is a *Conn that captures a JA4.
// Pair it with ConnContext on your http.Server.
func Listener(ln net.Listener) net.Listener { return listener{Listener: ln} }

func (l listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &Conn{Conn: c}, nil
}

func (c *Conn) Read(p []byte) (int, error) {
	c.once.Do(c.peek)
	if c.prefix != nil && c.prefix.Len() > 0 {
		n, _ := c.prefix.Read(p)
		return n, nil
	}
	return c.Conn.Read(p)
}

// peek reads the first TLS record (the ClientHello), computes JA4, and buffers
// the bytes for replay. Any read/parse failure leaves ja4 empty and replays what
// was read, so a malformed hello never breaks the connection.
func (c *Conn) peek() {
	header := make([]byte, 5)
	if _, err := io.ReadFull(c.Conn, header); err != nil {
		c.prefix = bytes.NewReader(header[:0])
		return
	}
	recLen := int(header[3])<<8 | int(header[4])
	body := make([]byte, recLen)
	n, _ := io.ReadFull(c.Conn, body)
	full := append(append([]byte{}, header...), body[:n]...)
	c.prefix = bytes.NewReader(full)
	if h, err := parseClientHello(full); err == nil {
		s := ja4(h)
		c.mu.Lock()
		c.ja4 = s
		c.mu.Unlock()
	}
}

// JA4 returns the captured fingerprint (empty until the handshake has been read).
func (c *Conn) JA4() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ja4
}

// ConnContext stores the *Conn in the request context. Assign it to
// http.Server.ConnContext so Local can retrieve the JA4 at request time.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	if fc, ok := c.(*Conn); ok {
		return connKey.With(ctx, fc)
	}
	return ctx
}

type localCollector struct{}

// Local returns a Collector that emits the JA4 captured by the Listener for this
// request's connection (via ConnContext). Absent capture contributes nothing.
func Local() fingerprint.Collector { return localCollector{} }

func (localCollector) Collect(r *http.Request) ([]fingerprint.Component, error) {
	c, ok := connKey.From(r.Context())
	if !ok {
		return nil, nil
	}
	if s := c.JA4(); s != "" {
		return []fingerprint.Component{{Name: "tls", Value: s}}, nil
	}
	return nil, nil
}
```

> Add `import "net/http"` for the `*http.Request` in `Collect`. Note the ClientHello-in-one-record assumption (true for typical hellos ≤ 16KB); a fragmented hello yields an empty JA4 (graceful degradation), which is acceptable for v1 — document it in `doc.go`.

```go
// doc.go
// Package tlsprint contributes a TLS (JA4) fingerprint as the "tls" component of
// a web/fingerprint Fingerprinter. Two paths: trusted upstream headers
// (Cloudflare/CloudFront/Envoy/Caddy/Traefik) for CDN-terminated TLS, and a
// net.Listener wrapper computing JA4 from the raw ClientHello when the Go server
// terminates TLS. Header sources are trust-gated; an untrusted header is dropped.
// The local path assumes the ClientHello fits one TLS record.
package tlsprint
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./web/fingerprint/tlsprint/ -run TestLocalCapturesJA4`
Expected: PASS. `just fmt ./web/fingerprint/...`.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint/tlsprint/listener.go web/fingerprint/tlsprint/doc.go web/fingerprint/tlsprint/listener_test.go
git commit -m "feat(tlsprint): listener JA4 capture, ConnContext, Local collector"
```

---

### Task 14: Fuzz targets, catalog update, final lint

**Files:**
- Create: `web/fingerprint/jsprobe_fuzz_test.go`, `web/fingerprint/tlsprint/clienthello_fuzz_test.go`
- Modify: `docs/packages.md` (delete the `auth/fingerprint` entry)

**Interfaces:** none new — hardening + docs.

- [ ] **Step 1: Add fuzz targets** (the geoip/useragent precedent found real DoS here)

```go
// jsprobe_fuzz_test.go
package fingerprint_test

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

func FuzzIngest(f *testing.F) {
	f.Add([]byte(`{"token":"x.y","data":{"timezone":"UTC"}}`))
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, _ := fingerprint.New(cfg)
	h := fp.IngestHandler()
	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
		h.ServeHTTP(httptest.NewRecorder(), req) // must never panic
	})
}
```

```go
// tlsprint/clienthello_fuzz_test.go
package tlsprint

import "testing"

func FuzzParseClientHello(f *testing.F) {
	f.Add([]byte{0x16, 0x03, 0x01, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseClientHello(b) // must never panic on any input
	})
}
```

- [ ] **Step 2: Run fuzz smoke + full test + race**

Run:
```bash
go test ./web/fingerprint/... -run '^$' -fuzz FuzzIngest -fuzztime 20s
go test ./web/fingerprint/tlsprint/ -run '^$' -fuzz FuzzParseClientHello -fuzztime 20s
go test ./web/fingerprint/... -race
```
Expected: no crashes; all tests PASS. Fix any panic the fuzzer finds (add bounds checks) before continuing.

- [ ] **Step 3: Update the package catalog**

Remove the `auth/fingerprint` roadmap entry from `docs/packages.md` (the block titled `**auth/fingerprint**` and its `Deps:` line + surrounding `---` separators). The shipped `web/fingerprint` package is documented by its `doc.go`, not the roadmap (per docs/design.md: the catalog lists only unbuilt packages).

- [ ] **Step 4: Full lint + doc pass**

Run:
```bash
just fmt ./web/fingerprint/...
just lint
just test ./web/fingerprint/...
```
Expected: `just lint` clean (nilaway, betteralign, modernize all pass); tests PASS. Fix every finding — nilaway in particular flags nil-deref paths in the new parser/handlers.

- [ ] **Step 5: Commit**

```bash
git add web/fingerprint docs/packages.md
git commit -m "test(fingerprint): fuzz ingest + ClientHello; drop auth/fingerprint from catalog"
```

---

## Self-Review

**1. Spec coverage:**
- Layered opt-in collectors → T2 (seam), T3 (headers), T4 (IP), T9 (JS), T11–13 (TLS). ✓
- Identity: keyed HMAC `Fingerprint`/`Digest`, `Drift` → T1. ✓
- Signals (7): datacenter-asn, bot-ua → T5; headless, tls-ua-mismatch, lang-mismatch, geo-tz-mismatch, header-anomaly → T6. ✓
- stdlib core + `GeoLookup`/`UAFamily` seams; geoip/useragent only in `example_test.go` → T2, T10. ✓
- TLS trust-gated source chain (CDN headers) + local JA4 fallback → T11, T12, T13. ✓
- JS: `ScriptHandler` (SRI, long-cache), signed-token `IngestHandler`, cookie/store carry, public payload schema, canvas/WebGL opt-in → T8, T9. ✓
- Middleware + `FromContext` + `LogExtractor` → T7. ✓
- Config env-loadable → T1. ✓ Presets `Session`/`Antifraud` → T10. ✓
- Anti-scope (no scoring/storage/device-graph): honored — no such code planned. ✓
- Testing: black-box, golden/structural JA4, token tamper, fuzz → throughout + T14. ✓
- Catalog: drop `auth/fingerprint` → T14. ✓

**2. Placeholder scan:** The inline `automationJA4` map ships empty by design (documented: unmatched → no signal); every other step has real code. Illustrative test stubs (`staticCollector`, `GeoLookupArg`, `trustAll`) are explicitly flagged for deletion in their steps. `buildHello` is called out as must-implement with a fallback capture strategy.

**3. Type consistency:** `Fingerprint`/`Digest`/`Component`/`Signal`/`GeoInfo`/`Family` names are stable across tasks. Two deliberate refinements from the spec, flagged in-plan: (a) `New` and presets return `error` (spec wrote `*Fingerprinter`) because `Config.Validate` can fail; (b) the JS collector is `(*Fingerprinter).JSCollector()` not the free `JS()` (it needs the codec/store) — Task 9's note and Task 10's `Antifraud` use the method form. `combineHash`, `componentIndex`, `verifyToken`, `readProbe`, `storeKey`, `parseClientHello`, `ja4`, `ConnContext`, `Local` are consistent between definition and use.
