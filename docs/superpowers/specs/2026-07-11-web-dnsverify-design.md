# web/dnsverify — design

Date: 2026-07-11
Status: approved for planning

## Purpose

DNS-based domain-ownership and routing verification: prove a tenant controls a
domain, and confirm a custom domain is pointed at us so TLS/routing works.
The single mechanic is *look up a record type at a host, compare the observed
value(s) against expected*. Primary consumers: custom-domain onboarding
(pairs with `web/hostrouter` + `web/autocert`). Single-shot and stateless —
the consumer owns token persistence and polling cadence.

## Scope

Two verification **intents**, both DNS-only:

| Intent | Record types | Who supplies the expected value |
|---|---|---|
| **Ownership token** | `TXT` | package mints a random token |
| **Routing target** | `CNAME`, `A`, `AAAA` | consumer supplies the target host / IPs |

Explicitly **out** of v1 (see Non-goals): email deliverability (SPF/DKIM/DMARC),
HTTP `/.well-known` / meta-tag verification, polling/scheduling, token storage.

## Placement

`web/dnsverify`. Web-boundary concern (custom-domain onboarding, same shelf as
`hostrouter`/`autocert`). Stdlib-only DNS via the `net` package; one forge dep
(`random`, for token minting). No driver subpackage — the resolver seam is
stdlib. When this ships, delete the `web/dnsverify` entry from docs/packages.md.

## API surface

```go
package dnsverify

// Construction — New(...Option) with env-loadable Config.
func New(opts ...Option) (*Verifier, error)

type Config struct {
    Timeout    time.Duration // per-lookup deadline           env:"DNSVERIFY_TIMEOUT"      (default 5s)
    Label      string        // TXT ownership host prefix      env:"DNSVERIFY_LABEL"        (default "_forge-verify")
    TokenBytes int           // entropy for minted tokens      env:"DNSVERIFY_TOKEN_BYTES"  (default 16)
}
func DefaultConfig() Config
func (Config) Validate() error

// Options (options.go): type Option func(*config)
//   WithResolver(Resolver)   // default net.DefaultResolver
//   WithConfig(Config)
//   WithTimeout(time.Duration)
//   WithLabel(string)
//   WithTokenBytes(int)

// The verify core — single-shot, one lookup-and-compare.
func (v *Verifier) Verify(ctx context.Context, c Challenge) (Result, error)

// Batteries-included challenge constructors (pure — Verify is the sole gate).
func (v *Verifier) TXTChallenge(domain string) Challenge            // mints token
func (v *Verifier) CNAMEChallenge(host, target string) Challenge
func (v *Verifier) AChallenge(host string, ips ...netip.Addr) Challenge
func (v *Verifier) AAAAChallenge(host string, ips ...netip.Addr) Challenge

// Core value type — plain, serializable; the consumer persists it (e.g. a
// Postgres row/JSONB) between "show instructions" and "verify later".
type Challenge struct {
    Record RecordType // TXT | CNAME | A | AAAA
    Host   string     // FQDN to query, e.g. "_forge-verify.example.com"
    Expect []string   // one-or-more acceptable values (token / target / IPs)
}

type RecordType uint8
const ( TXT RecordType = iota; CNAME; A; AAAA )
func (RecordType) String() string // stable uppercase token: "TXT"/"CNAME"/"A"/"AAAA"

type Result struct {
    Verified bool     // did observed DNS satisfy the challenge?
    Found    []string // observed record values at Host (for UI/debug)
}
```

### Construction & config

- `WithResolver` defaults to `net.DefaultResolver`. A custom `*net.Resolver`
  (e.g. with a specific dialer/DNS server) drops in unchanged — it satisfies
  the `Resolver` interface structurally.
- `Timeout` is applied per lookup via a context derived from the caller's
  `ctx` (honors upstream cancellation) — satisfies "every external call has an
  explicit timeout with a safe default."
- `Config.Validate`: `Timeout > 0`, `Label` non-empty and a syntactically
  valid DNS label, `TokenBytes >= 8`. `New` applies `DefaultConfig()` then
  options and returns `(nil, err)` when the resulting `Config.Validate` fails —
  the erroring-`New` convention shared by web packages that carry a validatable
  `Config` (`secheaders`, `timeout`, `compress`, `cors`, `cookie`).

### Challenge construction (structured, i18n-friendly)

- `TXTChallenge(domain)` mints a token via `random.URLSafe(TokenBytes)`
  (unpadded base64url — TXT-safe), sets `Host = Label + "." + domain`,
  `Expect = ["forge-verification=<token>"]`. The `forge-verification=` prefix
  namespaces the value so it never collides with SPF / other TXT records at
  the same host.
- All constructors are **pure** (no error): `random` panics only on a broken
  OS RNG, and `Verify` is the single validation gate (`ErrInvalidChallenge`
  for empty `Host`/`Expect` or unknown `Record`). This keeps `TXTChallenge`
  consistent with `CNAMEChallenge`/`AChallenge`/`AAAAChallenge`.
- Raw `Challenge{...}` literals + `Verify` remain fully supported for advanced
  flows (e.g. a CNAME-in-host ownership token) — the batteries are opt-in.
- **No user-facing string rendering.** The package renders no prose. Consumers
  feed the structured fields into their own i18n layer, e.g.:

  ```go
  catalog.T(ctx, "dns_setup."+strings.ToLower(c.Record.String()), i18n.Params{
      "host":  c.Host,
      "value": strings.Join(c.Expect, ", "),
  })
  ```

  `RecordType.String()` is guaranteed stable (doubles as an i18n key fragment
  and as the literal record type the user types into their DNS panel).

## Resolver seam

A minimal interface that `*net.Resolver` satisfies structurally:

```go
type Resolver interface {
    LookupTXT(ctx context.Context, host string) ([]string, error)
    LookupCNAME(ctx context.Context, host string) (string, error)
    LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}
```

### Test double — `StaticResolver` (ships with the package)

Map-backed, in-memory, configured entirely at construction via functional
options (no mutating setters — forge bans builders), immutable afterward so
it is safe to share across concurrent-lookup tests:

```go
r := dnsverify.NewStaticResolver(
    dnsverify.WithTXT("_forge-verify.example.com", "forge-verification=abc123"),
    dnsverify.WithCNAME("app.example.com", "ingress.ourservice.com."),
    dnsverify.WithIP("example.com", netip.MustParseAddr("203.0.113.10")),
    dnsverify.WithLookupError("timeout.example.com", someTempErr),
)

type StaticOption func(*StaticResolver) // distinct from the Verifier's Option
func NewStaticResolver(opts ...StaticOption) *StaticResolver
func WithTXT(host string, values ...string) StaticOption   // repeatable → multiple TXT RRs
func WithCNAME(host, target string) StaticOption
func WithIP(host string, ips ...netip.Addr) StaticOption   // A/AAAA inferred from family
func WithLookupError(host string, err error) StaticOption
```

Consumers reuse `StaticResolver` to test their own onboarding flows without
real DNS.

## Match semantics

- **TXT** — exact string match: `Verified` if any observed TXT RR equals any
  `Expect` entry. Case-sensitive (tokens are). `LookupTXT` already joins the
  255-char chunks of a single RR, so comparison is against the whole value.
- **CNAME** — target equality, normalized on both sides (lowercase + strip
  trailing dot). `LookupCNAME` returns the queried host itself when there is
  no CNAME, so "no CNAME" reads as not-verified, never a false match.
  Caveat (stdlib limit): `LookupCNAME` follows the whole chain and returns the
  **final canonical name**, not the immediate record — so `Expect` must be the
  terminal target. When our ingress is itself a CNAME (e.g. to an ELB), the
  robust pointing check is **A/AAAA** (resolve the domain, compare to our
  anycast IPs), which is chain-agnostic; `doc.go` recommends A/AAAA for apex
  and for CNAME-fronted ingress. Raw immediate-CNAME inspection would need a
  non-stdlib DNS library — out of scope.
- **A / AAAA** — set intersection: `Verified` if the host resolves to **at
  least one** expected IP. `A` queries `LookupNetIP(ctx, "ip4", host)`, `AAAA`
  queries `"ip6"`. `Expect` holds canonical IP strings (parsed to
  `netip.Addr` for comparison so `203.0.113.10` == `203.0.113.010`-style
  variants compare correctly). `Found` lists observed IPs.

## Error taxonomy

The distinction a polling consumer needs — "user hasn't added it yet" must not
look like "DNS is broken":

- **Pending** — `NXDOMAIN` / no-such-host / empty record set → **not an
  error**: `Result{Verified:false, Found:nil}, nil`. Detected via
  `*net.DNSError.IsNotFound` (and an empty result slice).
- **Broken DNS** — timeout / SERVFAIL / temporary → wrapped `ErrLookup`.
  Detected via `*net.DNSError.IsTemporary` / non-NotFound `DNSError`.
- **Bad input** — unknown `Record`, empty `Host`, empty `Expect` →
  `ErrInvalidChallenge` (returned before any lookup).

Sentinels in `errors.go`, single-line, `errors.Is`-matchable: `ErrLookup`,
`ErrInvalidChallenge`, and `ErrInvalidConfig` (construction-time
`Config.Validate` failure — the `secheaders`/`timeout` convention).

The consumer distinguishes four states:

| State | Test |
|---|---|
| verified | `res.Verified` |
| pending | `!res.Verified && len(res.Found)==0 && err==nil` |
| misconfigured | `!res.Verified && len(res.Found)>0` |
| broken DNS | `errors.Is(err, ErrLookup)` |

## Internals / file layout

```
web/dnsverify/
├── doc.go          # package doc + runnable example (mint TXT → persist → reload → verify; CNAME routing check)
├── config.go       # Config, DefaultConfig, Validate
├── options.go      # type Option func(*config); With* options
├── errors.go       # ErrLookup, ErrInvalidChallenge
├── challenge.go    # RecordType (+String), Challenge, constructors
├── dnsverify.go    # Verifier, New, Verify + per-record match logic
└── resolver.go     # Resolver interface + StaticResolver (+ StaticOption, With* fakes)
```

~400–500 LOC. `Verify` dispatches on `c.Record` to a per-type lookup+compare,
routes DNS errors through the taxonomy above, and fills `Result`.

## Performance

Not a hot path (onboarding-time, human-driven). Readable-first; per-lookup
context timeout; clients/resolver created once in `New` and reused. No
benchmark owed.

## Testing (black-box only, `dnsverify_test`)

- **Per record type**: match / normalization (CNAME trailing-dot + case, IP
  canonicalization), multi-value `Expect`, multiple TXT RRs at a host.
- **Result states**: verified / pending / misconfigured for each type.
- **Error taxonomy**: `NXDOMAIN` → pending (`err==nil`); temporary/SERVFAIL →
  `ErrLookup`; unknown record / empty host / empty Expect → `ErrInvalidChallenge`.
- **Constructors**: `TXTChallenge` host = `Label`+domain, value prefix, token
  uniqueness across calls, `TokenBytes` honored; CNAME/A/AAAA field shapes.
- **Config**: `DefaultConfig` values; `Validate` rejects non-positive timeout,
  empty/invalid label, `TokenBytes < 8`.
- **Context**: caller cancellation is honored; per-lookup timeout fires.
- **StaticResolver**: options compose (repeatable `WithTXT`), `WithLookupError`
  surfaces as `ErrLookup`, immutability (shared across goroutines, `-race`).
- **doc.go example** compiles and runs.

## Non-goals

- **No email deliverability** (SPF/DKIM/DMARC/return-path) — dropped in scoping;
  a separate package later if needed.
- **No HTTP `/.well-known` or meta-tag verification** — DNS-only.
- **No polling / scheduling / `WaitFor`** — single-shot; the consumer's
  `scheduler`/`jobqueue` owns cadence and expiry.
- **No token persistence / `Store` seam** — the consumer owns durability
  (Postgres). `Challenge` is plain and serializable for exactly this.
- **No user-facing prose** — structured fields for the consumer's i18n.
- **No live DNS record *creation*** — the package reads DNS; publishing records
  is the domain owner's action.

## Wrap-up checklist for implementation

- doc.go with runnable usage example.
- docs/packages.md: delete the `web/dnsverify` roadmap entry.
- `just fmt ./web/dnsverify/...`, `just lint` green.
