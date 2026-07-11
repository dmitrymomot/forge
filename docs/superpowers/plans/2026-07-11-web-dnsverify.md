# web/dnsverify Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `web/dnsverify` — a single-shot, stateless DNS ownership/routing verification package behind a `net.Resolver` seam.

**Architecture:** A `Verifier` (built via `New(...Option)`) does one lookup-and-compare per `Verify(ctx, Challenge)` call over a small `Resolver` interface that `*net.Resolver` satisfies. Batteries-included constructors mint TXT ownership tokens or build CNAME/A/AAAA routing challenges. The package holds no token state — the consumer persists the plain `Challenge`. An in-memory `StaticResolver` (functional-options-configured) is the shipped test double.

**Tech Stack:** Go (stdlib `net`, `net/netip`, `context`, `time`, `strings`, `errors`, `fmt`) + one forge dep, `github.com/dmitrymomot/forge/core/random`, for token minting. No external deps, no driver subpackage.

**Reference spec:** `docs/superpowers/specs/2026-07-11-web-dnsverify-design.md`

## Global Constraints

- **Module path:** `github.com/dmitrymomot/forge`; package import path `github.com/dmitrymomot/forge/web/dnsverify`.
- **Work only in the current branch; never switch branches.**
- **Black-box tests only** — test package is `dnsverify_test` (external), reusing the exported `StaticResolver`.
- **Errors:** single-line, `errors.Is`-matchable sentinels in `errors.go`; wrap with `fmt.Errorf("%w: ...", Sentinel)`.
- **Env prefix baked into tags:** `DNSVERIFY_` (`DNSVERIFY_TIMEOUT`, `DNSVERIFY_LABEL`, `DNSVERIFY_TOKEN_BYTES`).
- **`New` convention:** `func New(opts ...Option) (*Verifier, error)` — returns error when `Config.Validate` fails (matches `secheaders`/`timeout`/`compress`).
- **No builders / mutating setters** — configure via `Option` / `StaticOption` functional options only.
- **Go 1.26:** use `new(expr)` (no `ptr.To` wrapper) if a pointer to a literal is ever needed; `just lint` runs modernize + betteralign + nilaway.
- **Formatting/lint:** after file changes run `just fmt ./web/dnsverify/...` (package-path form — single-file form trips a spurious betteralign "undefined"); before finishing run `just lint`.
- **No Claude attribution** in any commit message.
- **Constructors are pure** (no error return); `Verify` is the single validation gate.

---

### Task 1: Errors and core value types

**Files:**
- Create: `web/dnsverify/errors.go`
- Create: `web/dnsverify/challenge.go` (types only; constructors added in Task 5)
- Test: `web/dnsverify/challenge_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `ErrLookup`, `ErrInvalidChallenge`, `ErrInvalidConfig error` (package sentinels).
  - `type RecordType uint8` with `const ( TXT RecordType = iota; CNAME; A; AAAA )` and `func (RecordType) String() string`.
  - `type Challenge struct { Record RecordType; Host string; Expect []string }`.

- [ ] **Step 1: Write the failing test**

Create `web/dnsverify/challenge_test.go`:

```go
package dnsverify_test

import (
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestRecordTypeString(t *testing.T) {
	cases := map[dnsverify.RecordType]string{
		dnsverify.TXT:   "TXT",
		dnsverify.CNAME: "CNAME",
		dnsverify.A:     "A",
		dnsverify.AAAA:  "AAAA",
	}
	for rt, want := range cases {
		if got := rt.String(); got != want {
			t.Errorf("RecordType(%d).String() = %q, want %q", rt, got, want)
		}
	}
	if got := dnsverify.RecordType(99).String(); got != "UNKNOWN" {
		t.Errorf("unknown RecordType.String() = %q, want UNKNOWN", got)
	}
}

func TestChallengeIsPlainValue(t *testing.T) {
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	if c.Host != "_forge-verify.example.com" || len(c.Expect) != 1 {
		t.Fatalf("Challenge fields not readable: %+v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/dnsverify/...`
Expected: FAIL — build error, `dnsverify` package/undefined `RecordType`, `Challenge`.

- [ ] **Step 3: Write minimal implementation**

Create `web/dnsverify/errors.go`:

```go
package dnsverify

import "errors"

var (
	// ErrLookup wraps a genuine resolver failure (timeout, SERVFAIL, other
	// temporary DNS error). It is distinct from "the record is not published
	// yet", which Verify reports as an unverified Result with a nil error.
	ErrLookup = errors.New("dnsverify: lookup failed")

	// ErrInvalidChallenge marks a malformed Challenge — unknown Record, empty
	// Host, or empty Expect — rejected before any lookup.
	ErrInvalidChallenge = errors.New("dnsverify: invalid challenge")

	// ErrInvalidConfig marks a Config that fails Validate at construction.
	ErrInvalidConfig = errors.New("dnsverify: invalid config")
)
```

Create `web/dnsverify/challenge.go`:

```go
package dnsverify

// RecordType is the DNS record kind a Challenge verifies.
type RecordType uint8

const (
	TXT RecordType = iota
	CNAME
	A
	AAAA
)

// String returns the stable uppercase DNS type token ("TXT", "CNAME", "A",
// "AAAA"). It is safe as an i18n key fragment and matches the record type a
// user types into a DNS panel. Unknown values render "UNKNOWN".
func (t RecordType) String() string {
	switch t {
	case TXT:
		return "TXT"
	case CNAME:
		return "CNAME"
	case A:
		return "A"
	case AAAA:
		return "AAAA"
	default:
		return "UNKNOWN"
	}
}

// Challenge describes one DNS record to verify: look up Record at Host and
// check the observed value(s) against Expect (any match verifies). It is plain
// and serializable — persist it (e.g. a Postgres row/JSONB) between issuing
// setup instructions and verifying later.
type Challenge struct {
	Record RecordType
	Host   string
	Expect []string
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./web/dnsverify/...` then `just test ./web/dnsverify/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/dnsverify/errors.go web/dnsverify/challenge.go web/dnsverify/challenge_test.go
git commit -m "feat(dnsverify): add error sentinels and core Challenge/RecordType types"
```

---

### Task 2: Resolver seam and StaticResolver test double

**Files:**
- Create: `web/dnsverify/resolver.go`
- Test: `web/dnsverify/resolver_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Resolver interface { LookupTXT(ctx, host) ([]string, error); LookupCNAME(ctx, host) (string, error); LookupNetIP(ctx, network, host) ([]netip.Addr, error) }`.
  - `type StaticResolver struct{...}` implementing `Resolver`.
  - `type StaticOption func(*StaticResolver)`.
  - `func NewStaticResolver(opts ...StaticOption) *StaticResolver`.
  - `func WithTXT(host string, values ...string) StaticOption` (repeatable/appends).
  - `func WithCNAME(host, target string) StaticOption`.
  - `func WithIP(host string, ips ...netip.Addr) StaticOption`.
  - `func WithLookupError(host string, err error) StaticOption`.
  - Unknown hosts return a `*net.DNSError` with `IsNotFound == true`.

- [ ] **Step 1: Write the failing test**

Create `web/dnsverify/resolver_test.go`:

```go
package dnsverify_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestStaticResolverTXT(t *testing.T) {
	r := dnsverify.NewStaticResolver(
		dnsverify.WithTXT("h.example.com", "a"),
		dnsverify.WithTXT("h.example.com", "b"), // repeatable → appends
	)
	got, err := r.LookupTXT(context.Background(), "h.example.com")
	if err != nil {
		t.Fatalf("LookupTXT: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("LookupTXT = %v, want [a b]", got)
	}
}

func TestStaticResolverNotFound(t *testing.T) {
	r := dnsverify.NewStaticResolver()
	_, err := r.LookupTXT(context.Background(), "missing.example.com")
	var d *net.DNSError
	if !errors.As(err, &d) || !d.IsNotFound {
		t.Fatalf("want IsNotFound DNSError, got %v", err)
	}
}

func TestStaticResolverCNAME(t *testing.T) {
	r := dnsverify.NewStaticResolver(dnsverify.WithCNAME("app.example.com", "ingress.svc.com."))
	got, err := r.LookupCNAME(context.Background(), "app.example.com")
	if err != nil || got != "ingress.svc.com." {
		t.Fatalf("LookupCNAME = %q, %v", got, err)
	}
}

func TestStaticResolverNetIPFamilyFilter(t *testing.T) {
	r := dnsverify.NewStaticResolver(dnsverify.WithIP("example.com",
		netip.MustParseAddr("203.0.113.10"),
		netip.MustParseAddr("2001:db8::1"),
	))
	v4, err := r.LookupNetIP(context.Background(), "ip4", "example.com")
	if err != nil || len(v4) != 1 || !v4[0].Is4() {
		t.Fatalf("ip4 lookup = %v, %v", v4, err)
	}
	v6, err := r.LookupNetIP(context.Background(), "ip6", "example.com")
	if err != nil || len(v6) != 1 || v6[0].Is4() {
		t.Fatalf("ip6 lookup = %v, %v", v6, err)
	}
}

func TestStaticResolverLookupError(t *testing.T) {
	sentinel := errors.New("boom")
	r := dnsverify.NewStaticResolver(
		dnsverify.WithTXT("h.example.com", "a"),
		dnsverify.WithLookupError("h.example.com", sentinel),
	)
	if _, err := r.LookupTXT(context.Background(), "h.example.com"); !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/dnsverify/...`
Expected: FAIL — undefined `NewStaticResolver`, `WithTXT`, etc.

- [ ] **Step 3: Write minimal implementation**

Create `web/dnsverify/resolver.go`:

```go
package dnsverify

import (
	"context"
	"net"
	"net/netip"
)

// Resolver is the DNS seam. *net.Resolver satisfies it structurally, so
// net.DefaultResolver is the zero-config default and a custom *net.Resolver
// (own dialer/DNS server) drops in unchanged.
type Resolver interface {
	LookupTXT(ctx context.Context, host string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// StaticResolver is an in-memory Resolver for tests. Configure it entirely at
// construction with the With* options; it is immutable afterward and safe to
// share across goroutines. Unknown hosts resolve as not-found (IsNotFound),
// which Verify reports as an unverified Result with a nil error.
type StaticResolver struct {
	txt   map[string][]string
	cname map[string]string
	ips   map[string][]netip.Addr
	errs  map[string]error
}

// StaticOption configures a StaticResolver. It is distinct from the Verifier's
// Option type.
type StaticOption func(*StaticResolver)

// NewStaticResolver builds an in-memory resolver from the given records.
func NewStaticResolver(opts ...StaticOption) *StaticResolver {
	r := &StaticResolver{
		txt:   map[string][]string{},
		cname: map[string]string{},
		ips:   map[string][]netip.Addr{},
		errs:  map[string]error{},
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithTXT adds TXT values at host. Repeatable — each call appends, modeling
// multiple TXT records at one host.
func WithTXT(host string, values ...string) StaticOption {
	return func(r *StaticResolver) { r.txt[host] = append(r.txt[host], values...) }
}

// WithCNAME sets the CNAME target at host.
func WithCNAME(host, target string) StaticOption {
	return func(r *StaticResolver) { r.cname[host] = target }
}

// WithIP adds A/AAAA addresses at host; the family is inferred per address at
// lookup time.
func WithIP(host string, ips ...netip.Addr) StaticOption {
	return func(r *StaticResolver) { r.ips[host] = append(r.ips[host], ips...) }
}

// WithLookupError makes every lookup for host return err — used to exercise
// the ErrLookup path.
func WithLookupError(host string, err error) StaticOption {
	return func(r *StaticResolver) { r.errs[host] = err }
}

func notFound(host string) error {
	return &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// LookupTXT implements Resolver.
func (r *StaticResolver) LookupTXT(_ context.Context, host string) ([]string, error) {
	if err := r.errs[host]; err != nil {
		return nil, err
	}
	v, ok := r.txt[host]
	if !ok {
		return nil, notFound(host)
	}
	return v, nil
}

// LookupCNAME implements Resolver.
func (r *StaticResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	if err := r.errs[host]; err != nil {
		return "", err
	}
	v, ok := r.cname[host]
	if !ok {
		return "", notFound(host)
	}
	return v, nil
}

// LookupNetIP implements Resolver. network is "ip4", "ip6", or "ip".
func (r *StaticResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	if err := r.errs[host]; err != nil {
		return nil, err
	}
	all, ok := r.ips[host]
	if !ok {
		return nil, notFound(host)
	}
	out := make([]netip.Addr, 0, len(all))
	for _, ip := range all {
		switch network {
		case "ip4":
			if ip.Is4() {
				out = append(out, ip)
			}
		case "ip6":
			if !ip.Is4() {
				out = append(out, ip)
			}
		default:
			out = append(out, ip)
		}
	}
	if len(out) == 0 {
		return nil, notFound(host)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./web/dnsverify/...` then `just test ./web/dnsverify/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/dnsverify/resolver.go web/dnsverify/resolver_test.go
git commit -m "feat(dnsverify): add Resolver seam and in-memory StaticResolver test double"
```

---

### Task 3: Config, options, and Verifier construction

**Files:**
- Create: `web/dnsverify/config.go`
- Create: `web/dnsverify/options.go`
- Modify: `web/dnsverify/dnsverify.go` (create — `Verifier` + `New` only in this task)
- Test: `web/dnsverify/config_test.go`

**Interfaces:**
- Consumes: `Resolver` (Task 2), `ErrInvalidConfig` (Task 1).
- Produces:
  - `type Config struct { Timeout time.Duration; Label string; TokenBytes int }` with env tags, `func DefaultConfig() Config`, `func (Config) Validate() error`.
  - `type Option func(*config)`; `WithConfig`, `WithResolver`, `WithTimeout`, `WithLabel`, `WithTokenBytes`.
  - `type Verifier struct{...}`; `func New(opts ...Option) (*Verifier, error)`.

- [ ] **Step 1: Write the failing test**

Create `web/dnsverify/config_test.go`:

```go
package dnsverify_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestDefaultConfig(t *testing.T) {
	c := dnsverify.DefaultConfig()
	if c.Timeout != 5*time.Second || c.Label != "_forge-verify" || c.TokenBytes != 16 {
		t.Fatalf("DefaultConfig = %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("DefaultConfig must be valid: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	base := dnsverify.DefaultConfig()

	bad := base
	bad.Timeout = 0
	if !errors.Is(bad.Validate(), dnsverify.ErrInvalidConfig) {
		t.Error("zero Timeout must be invalid")
	}

	bad = base
	bad.Label = ""
	if !errors.Is(bad.Validate(), dnsverify.ErrInvalidConfig) {
		t.Error("empty Label must be invalid")
	}

	bad = base
	bad.TokenBytes = 4
	if !errors.Is(bad.Validate(), dnsverify.ErrInvalidConfig) {
		t.Error("TokenBytes < 8 must be invalid")
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	_, err := dnsverify.New(dnsverify.WithTimeout(0))
	if !errors.Is(err, dnsverify.ErrInvalidConfig) {
		t.Fatalf("New with zero Timeout: want ErrInvalidConfig, got %v", err)
	}
}

func TestNewAppliesOptions(t *testing.T) {
	v, err := dnsverify.New(
		dnsverify.WithResolver(dnsverify.NewStaticResolver()),
		dnsverify.WithLabel("_custom"),
		dnsverify.WithTokenBytes(24),
	)
	if err != nil || v == nil {
		t.Fatalf("New: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/dnsverify/...`
Expected: FAIL — undefined `DefaultConfig`, `New`, `WithTimeout`, etc.

- [ ] **Step 3: Write minimal implementation**

Create `web/dnsverify/config.go`:

```go
package dnsverify

import (
	"fmt"
	"time"
)

// Config is the env-loadable deployment config. The resolver is a code-shaped
// seam and lives in options (WithResolver), not env.
type Config struct {
	Timeout    time.Duration `env:"DNSVERIFY_TIMEOUT"`     // per-lookup deadline
	Label      string        `env:"DNSVERIFY_LABEL"`       // TXT ownership host prefix
	TokenBytes int           `env:"DNSVERIFY_TOKEN_BYTES"` // entropy (bytes) of minted tokens
}

// DefaultConfig returns a 5s per-lookup timeout, the "_forge-verify" TXT label,
// and 16-byte tokens.
func DefaultConfig() Config {
	return Config{
		Timeout:    5 * time.Second,
		Label:      "_forge-verify",
		TokenBytes: 16,
	}
}

// Validate rejects a non-positive Timeout, an empty Label, and TokenBytes < 8.
func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: non-positive Timeout", ErrInvalidConfig)
	}
	if c.Label == "" {
		return fmt.Errorf("%w: empty Label", ErrInvalidConfig)
	}
	if c.TokenBytes < 8 {
		return fmt.Errorf("%w: TokenBytes %d (want >= 8)", ErrInvalidConfig, c.TokenBytes)
	}
	return nil
}
```

Create `web/dnsverify/options.go`:

```go
package dnsverify

import "time"

type config struct {
	cfg      Config
	resolver Resolver
}

// Option configures a Verifier.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it before other options so
// they can override individual fields.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithResolver sets the DNS resolver seam (default net.DefaultResolver).
func WithResolver(r Resolver) Option { return func(cf *config) { cf.resolver = r } }

// WithTimeout sets the per-lookup deadline.
func WithTimeout(d time.Duration) Option { return func(cf *config) { cf.cfg.Timeout = d } }

// WithLabel sets the TXT ownership host prefix.
func WithLabel(s string) Option { return func(cf *config) { cf.cfg.Label = s } }

// WithTokenBytes sets the entropy (in bytes) of minted tokens.
func WithTokenBytes(n int) Option { return func(cf *config) { cf.cfg.TokenBytes = n } }
```

Create `web/dnsverify/dnsverify.go`:

```go
package dnsverify

import "net"

// Verifier performs single-shot DNS verification against a Resolver. Build it
// with New; it is safe for concurrent use.
type Verifier struct {
	resolver Resolver
	cfg      Config
}

// New builds a Verifier. It applies DefaultConfig, then the options, then
// returns an error if the resulting Config is invalid. The resolver defaults
// to net.DefaultResolver.
func New(opts ...Option) (*Verifier, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	r := cf.resolver
	if r == nil {
		r = net.DefaultResolver
	}
	return &Verifier{resolver: r, cfg: cf.cfg}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./web/dnsverify/...` then `just test ./web/dnsverify/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/dnsverify/config.go web/dnsverify/options.go web/dnsverify/dnsverify.go web/dnsverify/config_test.go
git commit -m "feat(dnsverify): add Config/Validate, options, and Verifier construction"
```

---

### Task 4: Verify — per-record match and error taxonomy

**Files:**
- Modify: `web/dnsverify/dnsverify.go` (add `Result`, `Verify`, helpers)
- Test: `web/dnsverify/verify_test.go`

**Interfaces:**
- Consumes: `Verifier`, `Resolver`, `StaticResolver`, `Challenge`, `RecordType`, `ErrLookup`, `ErrInvalidChallenge`.
- Produces:
  - `type Result struct { Verified bool; Found []string }`.
  - `func (v *Verifier) Verify(ctx context.Context, c Challenge) (Result, error)`.
  - Behavior: TXT/CNAME/A/AAAA match; NXDOMAIN → `Result{}, nil` (pending); temporary DNS error → `ErrLookup`; empty Host/Expect or unknown Record → `ErrInvalidChallenge`.

- [ ] **Step 1: Write the failing test**

Create `web/dnsverify/verify_test.go`:

```go
package dnsverify_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func newVerifier(t *testing.T, r dnsverify.Resolver) *dnsverify.Verifier {
	t.Helper()
	v, err := dnsverify.New(dnsverify.WithResolver(r))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestVerifyTXT(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithTXT("_forge-verify.example.com", "other=1", "forge-verification=abc"),
	))
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || !res.Verified {
		t.Fatalf("want verified, got %+v err=%v", res, err)
	}
}

func TestVerifyTXTMisconfigured(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithTXT("_forge-verify.example.com", "forge-verification=WRONG"),
	))
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || res.Verified || len(res.Found) != 1 {
		t.Fatalf("want misconfigured (found, not verified), got %+v err=%v", res, err)
	}
}

func TestVerifyPendingIsNotError(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver()) // nothing published
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || res.Verified || len(res.Found) != 0 {
		t.Fatalf("want pending (nil err, empty found), got %+v err=%v", res, err)
	}
}

func TestVerifyTemporaryErrorIsErrLookup(t *testing.T) {
	temp := &net.DNSError{Err: "server misbehaving", Name: "h", IsTemporary: true}
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithTXT("h.example.com", "x"),
		dnsverify.WithLookupError("h.example.com", temp),
	))
	c := dnsverify.Challenge{Record: dnsverify.TXT, Host: "h.example.com", Expect: []string{"y"}}
	_, err := v.Verify(context.Background(), c)
	if !errors.Is(err, dnsverify.ErrLookup) {
		t.Fatalf("want ErrLookup, got %v", err)
	}
}

func TestVerifyCNAMENormalizes(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithCNAME("app.example.com", "Ingress.SVC.com."), // mixed case + trailing dot
	))
	c := dnsverify.Challenge{
		Record: dnsverify.CNAME,
		Host:   "app.example.com",
		Expect: []string{"ingress.svc.com"},
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || !res.Verified {
		t.Fatalf("want verified (normalized), got %+v err=%v", res, err)
	}
}

func TestVerifyAIntersects(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver(
		dnsverify.WithIP("example.com", netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("203.0.113.10")),
	))
	c := dnsverify.Challenge{
		Record: dnsverify.A,
		Host:   "example.com",
		Expect: []string{"203.0.113.10"}, // one of the resolved set
	}
	res, err := v.Verify(context.Background(), c)
	if err != nil || !res.Verified || len(res.Found) != 2 {
		t.Fatalf("want verified with 2 found, got %+v err=%v", res, err)
	}
}

func TestVerifyInvalidChallenge(t *testing.T) {
	v := newVerifier(t, dnsverify.NewStaticResolver())
	cases := []dnsverify.Challenge{
		{Record: dnsverify.TXT, Host: "", Expect: []string{"x"}},          // empty host
		{Record: dnsverify.TXT, Host: "h", Expect: nil},                   // empty expect
		{Record: dnsverify.RecordType(99), Host: "h", Expect: []string{"x"}}, // unknown record
	}
	for i, c := range cases {
		if _, err := v.Verify(context.Background(), c); !errors.Is(err, dnsverify.ErrInvalidChallenge) {
			t.Errorf("case %d: want ErrInvalidChallenge, got %v", i, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/dnsverify/...`
Expected: FAIL — undefined `Result`, `(*Verifier).Verify`.

- [ ] **Step 3: Write minimal implementation**

Replace `web/dnsverify/dnsverify.go` with (keeps `Verifier`/`New` from Task 3, adds the rest):

```go
package dnsverify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Verifier performs single-shot DNS verification against a Resolver. Build it
// with New; it is safe for concurrent use.
type Verifier struct {
	resolver Resolver
	cfg      Config
}

// New builds a Verifier. It applies DefaultConfig, then the options, then
// returns an error if the resulting Config is invalid. The resolver defaults
// to net.DefaultResolver.
func New(opts ...Option) (*Verifier, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	r := cf.resolver
	if r == nil {
		r = net.DefaultResolver
	}
	return &Verifier{resolver: r, cfg: cf.cfg}, nil
}

// Result reports the outcome of a Verify call. Verified is whether observed
// DNS satisfied the Challenge; Found lists the observed values at Host (for
// display/debug). With a nil error there are three states: verified
// (Verified), pending (!Verified && len(Found) == 0 — nothing published yet),
// and misconfigured (!Verified && len(Found) > 0 — published but wrong).
type Result struct {
	Verified bool
	Found    []string
}

// Verify performs one lookup-and-compare for c. A record that is not published
// yet (NXDOMAIN / empty) yields an unverified Result with a nil error; a
// genuine resolver failure returns ErrLookup; a malformed Challenge returns
// ErrInvalidChallenge. The Verifier's Timeout bounds each lookup and the
// caller's ctx cancellation is honored.
func (v *Verifier) Verify(ctx context.Context, c Challenge) (Result, error) {
	if c.Host == "" || len(c.Expect) == 0 {
		return Result{}, ErrInvalidChallenge
	}
	ctx, cancel := context.WithTimeout(ctx, v.cfg.Timeout)
	defer cancel()

	switch c.Record {
	case TXT:
		return v.verifyTXT(ctx, c)
	case CNAME:
		return v.verifyCNAME(ctx, c)
	case A:
		return v.verifyIP(ctx, c, "ip4")
	case AAAA:
		return v.verifyIP(ctx, c, "ip6")
	default:
		return Result{}, ErrInvalidChallenge
	}
}

func (v *Verifier) verifyTXT(ctx context.Context, c Challenge) (Result, error) {
	records, err := v.resolver.LookupTXT(ctx, c.Host)
	if err != nil {
		return errResult(err)
	}
	for _, rec := range records {
		for _, want := range c.Expect {
			if rec == want {
				return Result{Verified: true, Found: records}, nil
			}
		}
	}
	return Result{Found: records}, nil
}

func (v *Verifier) verifyCNAME(ctx context.Context, c Challenge) (Result, error) {
	cname, err := v.resolver.LookupCNAME(ctx, c.Host)
	if err != nil {
		return errResult(err)
	}
	got := canonicalHost(cname)
	// LookupCNAME returns the queried host itself when there is no CNAME.
	if got == canonicalHost(c.Host) {
		return Result{}, nil
	}
	for _, want := range c.Expect {
		if got == canonicalHost(want) {
			return Result{Verified: true, Found: []string{cname}}, nil
		}
	}
	return Result{Found: []string{cname}}, nil
}

func (v *Verifier) verifyIP(ctx context.Context, c Challenge, network string) (Result, error) {
	got, err := v.resolver.LookupNetIP(ctx, network, c.Host)
	if err != nil {
		return errResult(err)
	}
	want := make(map[netip.Addr]struct{}, len(c.Expect))
	for _, s := range c.Expect {
		if addr, perr := netip.ParseAddr(s); perr == nil {
			want[addr.Unmap()] = struct{}{}
		}
	}
	found := make([]string, 0, len(got))
	verified := false
	for _, ip := range got {
		found = append(found, ip.String())
		if _, ok := want[ip.Unmap()]; ok {
			verified = true
		}
	}
	if len(found) == 0 {
		return Result{}, nil // nothing resolved yet → pending
	}
	return Result{Verified: verified, Found: found}, nil
}

// errResult routes a resolver error: "not published yet" (NXDOMAIN) becomes an
// unverified Result with a nil error; anything else becomes ErrLookup.
func errResult(err error) (Result, error) {
	var d *net.DNSError
	if errors.As(err, &d) && d.IsNotFound {
		return Result{}, nil
	}
	return Result{}, fmt.Errorf("%w: %v", ErrLookup, err)
}

// canonicalHost lowercases and strips a trailing dot for CNAME comparison.
func canonicalHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(h, "."))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./web/dnsverify/...` then `just test ./web/dnsverify/...`
Expected: PASS (all `verify_test.go` cases plus earlier tasks).

- [ ] **Step 5: Commit**

```bash
git add web/dnsverify/dnsverify.go web/dnsverify/verify_test.go
git commit -m "feat(dnsverify): implement Verify with per-record match and error taxonomy"
```

---

### Task 5: Batteries-included Challenge constructors

**Files:**
- Modify: `web/dnsverify/challenge.go` (add constructors + helper + token-prefix const)
- Test: `web/dnsverify/constructors_test.go`

**Interfaces:**
- Consumes: `Verifier` (uses `v.cfg.Label`, `v.cfg.TokenBytes`), `Challenge`, `RecordType`, `github.com/dmitrymomot/forge/core/random`.
- Produces:
  - `func (v *Verifier) TXTChallenge(domain string) Challenge` — `Host = Label + "." + domain`, `Expect = ["forge-verification=<token>"]`, token via `random.URLSafe(TokenBytes)`.
  - `func (v *Verifier) CNAMEChallenge(host, target string) Challenge`.
  - `func (v *Verifier) AChallenge(host string, ips ...netip.Addr) Challenge`.
  - `func (v *Verifier) AAAAChallenge(host string, ips ...netip.Addr) Challenge`.

- [ ] **Step 1: Write the failing test**

Create `web/dnsverify/constructors_test.go`:

```go
package dnsverify_test

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func TestTXTChallengeShapeAndUniqueness(t *testing.T) {
	v, err := dnsverify.New(dnsverify.WithResolver(dnsverify.NewStaticResolver()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c1 := v.TXTChallenge("example.com")
	c2 := v.TXTChallenge("example.com")

	if c1.Record != dnsverify.TXT {
		t.Errorf("Record = %v, want TXT", c1.Record)
	}
	if c1.Host != "_forge-verify.example.com" {
		t.Errorf("Host = %q", c1.Host)
	}
	if len(c1.Expect) != 1 || !strings.HasPrefix(c1.Expect[0], "forge-verification=") {
		t.Errorf("Expect = %v, want one forge-verification= value", c1.Expect)
	}
	if c1.Expect[0] == c2.Expect[0] {
		t.Errorf("tokens must be unique across calls: %q", c1.Expect[0])
	}
}

func TestTXTChallengeHonorsConfig(t *testing.T) {
	v, err := dnsverify.New(
		dnsverify.WithResolver(dnsverify.NewStaticResolver()),
		dnsverify.WithLabel("_custom"),
		dnsverify.WithTokenBytes(32),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := v.TXTChallenge("acme.test")
	if c.Host != "_custom.acme.test" {
		t.Errorf("Host = %q, want _custom.acme.test", c.Host)
	}
	// 32 raw bytes → 43 unpadded base64url chars after the prefix.
	token := strings.TrimPrefix(c.Expect[0], "forge-verification=")
	if len(token) != 43 {
		t.Errorf("token length = %d, want 43 for 32 bytes", len(token))
	}
}

func TestRoutingConstructors(t *testing.T) {
	v, err := dnsverify.New(dnsverify.WithResolver(dnsverify.NewStaticResolver()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cn := v.CNAMEChallenge("app.example.com", "ingress.svc.com")
	if cn.Record != dnsverify.CNAME || cn.Host != "app.example.com" || cn.Expect[0] != "ingress.svc.com" {
		t.Errorf("CNAMEChallenge = %+v", cn)
	}

	a := v.AChallenge("example.com", netip.MustParseAddr("203.0.113.10"))
	if a.Record != dnsverify.A || a.Expect[0] != "203.0.113.10" {
		t.Errorf("AChallenge = %+v", a)
	}

	aaaa := v.AAAAChallenge("example.com", netip.MustParseAddr("2001:db8::1"))
	if aaaa.Record != dnsverify.AAAA || aaaa.Expect[0] != "2001:db8::1" {
		t.Errorf("AAAAChallenge = %+v", aaaa)
	}
}

func TestTXTChallengeRoundTripsThroughVerify(t *testing.T) {
	v, err := dnsverify.New(dnsverify.WithResolver(dnsverify.NewStaticResolver())) // placeholder, replaced below
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := v.TXTChallenge("example.com")

	// Simulate the user publishing exactly the minted record, then verify.
	v2, err := dnsverify.New(dnsverify.WithResolver(
		dnsverify.NewStaticResolver(dnsverify.WithTXT(c.Host, c.Expect[0])),
	))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := v2.Verify(context.Background(), c)
	if err != nil || !res.Verified {
		t.Fatalf("round-trip: want verified, got %+v err=%v", res, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/dnsverify/...`
Expected: FAIL — undefined `(*Verifier).TXTChallenge`, `CNAMEChallenge`, etc.

- [ ] **Step 3: Write minimal implementation**

Append to `web/dnsverify/challenge.go` (add imports `net/netip` and `github.com/dmitrymomot/forge/core/random`):

```go
// txtValuePrefix namespaces the ownership token inside the TXT value so it
// never collides with SPF or other TXT records at the same host.
const txtValuePrefix = "forge-verification="

// TXTChallenge mints a fresh ownership token and returns a TXT Challenge to
// publish at Label.domain (e.g. "_forge-verify.example.com"). Persist the
// returned Challenge; verify it later with Verify. The token comes from
// random.URLSafe (unpadded base64url — safe in a TXT value).
func (v *Verifier) TXTChallenge(domain string) Challenge {
	token := random.URLSafe(v.cfg.TokenBytes)
	return Challenge{
		Record: TXT,
		Host:   v.cfg.Label + "." + domain,
		Expect: []string{txtValuePrefix + token},
	}
}

// CNAMEChallenge builds a routing Challenge: host must CNAME to target.
func (v *Verifier) CNAMEChallenge(host, target string) Challenge {
	return Challenge{Record: CNAME, Host: host, Expect: []string{target}}
}

// AChallenge builds a routing Challenge: host must resolve (A) to at least one
// of ips.
func (v *Verifier) AChallenge(host string, ips ...netip.Addr) Challenge {
	return Challenge{Record: A, Host: host, Expect: addrsToStrings(ips)}
}

// AAAAChallenge builds a routing Challenge: host must resolve (AAAA) to at
// least one of ips.
func (v *Verifier) AAAAChallenge(host string, ips ...netip.Addr) Challenge {
	return Challenge{Record: AAAA, Host: host, Expect: addrsToStrings(ips)}
}

func addrsToStrings(ips []netip.Addr) []string {
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out
}
```

The final import block of `web/dnsverify/challenge.go` must be:

```go
import (
	"net/netip"

	"github.com/dmitrymomot/forge/core/random"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./web/dnsverify/...` then `just test ./web/dnsverify/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/dnsverify/challenge.go web/dnsverify/constructors_test.go
git commit -m "feat(dnsverify): add batteries-included Challenge constructors"
```

---

### Task 6: Package doc, runnable example, and roadmap cleanup

**Files:**
- Create: `web/dnsverify/doc.go`
- Create: `web/dnsverify/example_test.go`
- Modify: `docs/packages.md` (delete the `web/dnsverify` roadmap entry)

**Interfaces:**
- Consumes: the full public API.
- Produces: package documentation + one runnable `Example` (deterministic via `StaticResolver`).

- [ ] **Step 1: Write the failing test**

Create `web/dnsverify/example_test.go`:

```go
package dnsverify_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/web/dnsverify"
)

func Example() {
	// A resolver double stands in for real DNS so the example is deterministic.
	v, err := dnsverify.New(dnsverify.WithResolver(
		dnsverify.NewStaticResolver(
			dnsverify.WithTXT("_forge-verify.example.com", "forge-verification=abc123"),
		),
	))
	if err != nil {
		panic(err)
	}

	// In production, mint a token and persist the Challenge (e.g. in Postgres):
	//   c := v.TXTChallenge("example.com"); save(c)
	// then reload it later and verify. Here we verify a known record.
	c := dnsverify.Challenge{
		Record: dnsverify.TXT,
		Host:   "_forge-verify.example.com",
		Expect: []string{"forge-verification=abc123"},
	}
	res, err := v.Verify(context.Background(), c)
	fmt.Println(res.Verified, err)
	// Output: true <nil>
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/dnsverify/...`
Expected: PASS for existing tests; the `Example` runs and its `// Output:` matches. (If the package doc is missing, the build still succeeds — the doc.go in Step 3 adds the package comment the linter expects.)

Note: this Example uses only already-implemented API, so it passes immediately — its purpose is to lock the doc example as a compiled, output-checked test.

- [ ] **Step 3: Write the package doc**

Create `web/dnsverify/doc.go`:

```go
// Package dnsverify performs single-shot DNS ownership and routing
// verification behind a small Resolver seam that *net.Resolver satisfies.
//
// Two intents share one mechanic — look up a record at a host, compare the
// observed value(s) against expected:
//
//   - Ownership token: TXTChallenge mints a random token; the domain owner
//     publishes a TXT record; Verify confirms the token is present.
//   - Routing target: CNAMEChallenge / AChallenge / AAAAChallenge check that a
//     domain points at your ingress (for custom-domain onboarding with
//     hostrouter + autocert).
//
// The package is stateless: TXTChallenge returns a plain, serializable
// Challenge that you persist (e.g. a Postgres row) between showing setup
// instructions and verifying later — nothing needs to survive a restart on
// this side. Verify is single-shot; the caller owns polling cadence (a
// scheduler/jobqueue re-checking a pending domain).
//
// A nil error from Verify has three states: verified (Result.Verified),
// pending (!Verified && len(Found) == 0 — not published yet), and
// misconfigured (!Verified && len(Found) > 0 — published but wrong). A genuine
// resolver failure returns ErrLookup, distinct from "not published yet".
//
//	v, err := dnsverify.New()
//	if err != nil { /* invalid config */ }
//
//	// Issue an ownership challenge and show the record to the user.
//	c := v.TXTChallenge("example.com")
//	// persist c (c.Host, c.Record, c.Expect) tied to the tenant/domain
//
//	// Later, after the user says they added it:
//	res, err := v.Verify(ctx, c)
//	switch {
//	case err != nil:                 // errors.Is(err, dnsverify.ErrLookup)
//	case res.Verified:               // done
//	case len(res.Found) == 0:        // pending — ask them to wait for DNS
//	default:                         // misconfigured — show res.Found
//	}
//
// Consumers render setup instructions from the structured Challenge fields via
// their own i18n layer; dnsverify emits no user-facing prose. The exported
// StaticResolver is an in-memory test double for exercising these flows
// without real DNS.
package dnsverify
```

- [ ] **Step 4: Verify build, tests, and lint**

Run: `just fmt ./web/dnsverify/...` then `just test ./web/dnsverify/...`
Expected: PASS, including the `Example` output check.

Then update `docs/packages.md`: delete the `web/dnsverify` entry (the heading `**web/dnsverify**` and its description paragraph plus the trailing `---` separator), since the roadmap lists only unbuilt packages.

Run: `just lint`
Expected: clean (go vet, build, golangci-lint incl. modernize/betteralign/nilaway all green).

- [ ] **Step 5: Commit**

```bash
git add web/dnsverify/doc.go web/dnsverify/example_test.go docs/packages.md
git commit -m "docs(dnsverify): add package doc, runnable example, and drop roadmap entry"
```

---

## Self-Review

**1. Spec coverage:**

| Spec item | Task |
|---|---|
| `New(...Option) (*Verifier, error)`, erroring on invalid config | 3 |
| `Config` (Timeout/Label/TokenBytes) + env tags + DefaultConfig + Validate | 3 |
| Options: WithResolver/WithConfig/WithTimeout/WithLabel/WithTokenBytes | 3 |
| `Verify(ctx, Challenge) (Result, error)` single-shot + per-lookup timeout | 4 |
| `Challenge{Record,Host,Expect}` plain/serializable | 1 |
| `RecordType` + stable `String()` | 1 |
| `Result{Verified,Found}` + three no-error states | 4 |
| Resolver seam (`*net.Resolver` satisfies) | 2 |
| `StaticResolver` via functional options, immutable | 2 |
| TXT exact match / CNAME normalize / A-AAAA intersection | 4 |
| Error taxonomy: pending vs ErrLookup vs ErrInvalidChallenge | 4 |
| `ErrInvalidConfig` sentinel | 1, 3 |
| Batteries constructors (pure), token via random.URLSafe | 5 |
| i18n-friendly structured fields, no prose | 1 (fields), 6 (doc) |
| doc.go runnable example | 6 |
| Delete packages.md roadmap entry | 6 |
| Black-box tests only | all test files use `dnsverify_test` |
| CNAME chain caveat (steer to A/AAAA) | documented in spec + doc.go recommends A/AAAA; no code beyond terminal-name match |

No gaps.

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to". Every code step shows complete code. The word "placeholder" in Task 5's round-trip test is a real comment on a throwaway verifier that the test immediately replaces — not a plan placeholder; the code is complete.

**3. Type consistency:** `Verifier`, `Config`, `config`, `Option`, `StaticOption`, `Resolver`, `StaticResolver`, `Challenge`, `RecordType`, `Result` names are identical across tasks. `Verify`/`TXTChallenge`/`CNAMEChallenge`/`AChallenge`/`AAAAChallenge`/`NewStaticResolver`/`WithTXT`/`WithCNAME`/`WithIP`/`WithLookupError` used consistently. `errResult`/`canonicalHost`/`addrsToStrings`/`notFound`/`txtValuePrefix` helpers each defined once. `random.URLSafe` signature matches `core/random`. `*net.Resolver` satisfies the three-method `Resolver` interface (all exist in stdlib since Go 1.18).

Note for the implementer: Task 4 **replaces** the whole `dnsverify.go` from Task 3 (the file is shown in full, carrying `Verifier`/`New` forward) — do not append and duplicate `New`.
