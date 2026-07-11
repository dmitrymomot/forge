# web/geoip — design

Date: 2026-07-11
Status: approved for planning

## Purpose

Resolve a client IP into geographic and network facts — country, region,
city, timezone, ASN — behind a pluggable `Source` seam. A pure **lookup
primitive**: it caches a `Location` in request context (the way `web/clientip`
caches the IP) and ships no policy. Consumers build country blocking,
routing, analytics, and fraud signals on top of the cached `Location`.

Primary consumers: `auth/fingerprint` (risk scoring), `ops/auditlog` and the
logger (enrichment), `i18n/dates` (timezone). Two CDN-header sources plus a
hand-rolled mmdb reader cover both "behind a geo-CDN" and "raw origin"
deployments.

## Placement

`web/geoip` — a web-boundary concern beside `web/clientip` (the middleware is
request-oriented and the header sources read HTTP headers). One driver
subpackage:

- `web/geoip` — `Location`, the `Locator` + `Source` seams, header sources,
  `Chain`, `FromLocator`, middleware, context accessors, `LogExtractor`.
- `web/geoip/mmdb` — the stdlib mmdb file reader; implements `geoip.Locator`.

**Zero external dependencies anywhere.** The roadmap named a `geoip/maxmind`
driver isolating `oschwald/maxminddb-golang`; this design instead hand-rolls
the mmdb reader (see Decisions), so the subpackage is renamed `geoip/mmdb`
(it names the *file format* it reads, not a data vendor — the data is
CC-licensed DB-IP, not MaxMind) and takes no external dependency (it imports
only the parent `geoip` for the shared `Location`/`Locator` types, plus
stdlib — a normal child→parent driver import, like `cache/redis` → `cache`;
no import cycle, since `geoip` core references only the `Locator` interface,
never the concrete reader). When this ships, delete the `web/geoip` entry
from docs/packages.md.

## Decisions (resolved during brainstorming)

1. **Scope = pure lookup (option D).** No country allow/deny middleware; the
   package caches a `Location` and exposes accessors, mirroring `clientip`.
2. **`Location` fields = analytics-grade (option B).** Country, region
   (code + name), city, IANA timezone, ASN + org. No lat/long (almost nothing
   reads coordinates server-side; addable non-breakingly later).
3. **Two seams, both first-class.** An IP-based `Locator` primitive (for
   jobqueue/CLI/anywhere an IP is already held) and a request-based `Source`
   (for header sources and the middleware).
4. **Hand-rolled mmdb reader, zero deps.** The `.mmdb` format is publicly
   specified and frozen (format 2.0); reading it is ~500 LOC of deterministic
   binary parsing over a trusted, operator-supplied file. Decoding **directly
   into `Location`** (no reflection, no intermediate `map[string]any`) is
   faster and lower-alloc than the reflection-based library and honors the
   `no-reflection` DNA rule instead of exempting it — the `useragent`
   precedent. Data vendor (DB-IP / MaxMind / IPinfo) is the consumer's choice.
5. **Both load modes.** In-memory (`[]byte` / `go:embed` / `fs.FS`) and
   file-backed mmap converge to a `db []byte` the decoder walks identically.

## API surface — `web/geoip`

```go
package geoip

// Result. Every field is zero-valued when the source can't provide it.
type Location struct {
    CountryCode string // ISO 3166-1 alpha-2, upper ("US"); "" = unknown
    RegionCode  string // ISO 3166-2 subdivision suffix ("CA")
    RegionName  string // "California"
    City        string // "San Francisco"
    TimeZone    string // IANA ("America/Los_Angeles")
    ASN         uint32 // 0 = unknown
    ASNOrg      string // "Cloudflare, Inc."
}

// Empty reports whether no field is populated (a clean "not found").
func (l Location) Empty() bool

// IP-based primitive — mmdb implements it; call directly from non-HTTP code.
type Locator interface {
    Lookup(ctx context.Context, ip netip.Addr) (Location, error)
}

// Request seam — header sources implement it; the middleware consumes it.
type Source interface {
    Lookup(r *http.Request) (Location, error)
}
```

Seam contract for both: a clean miss (header absent, IP not in DB) is
`(Location{}, nil)` — use `Empty()` to tell found from not-found. A non-nil
`error` is a *real* failure (corrupt/closed DB), never "not found." This keeps
I/O errors visible instead of silently blanking geo.

### Header sources, chain, bridge

```go
// Generic header→Location mapper (escape hatch for Fastly/Akamai/custom).
func Headers(m HeaderMap) Source
type HeaderMap struct { Country, RegionCode, RegionName, City, TimeZone, ASN, ASNOrg string }

// Presets — thin Headers(...) literals.
func Cloudflare() Source // CF-IPCountry → CountryCode
func CloudFront() Source // CloudFront-Viewer-{Country,Country-Region,Country-Region-Name,City,Time-Zone,ASN}
func Vercel()     Source // x-vercel-ip-{country,country-region,city,timezone}

// Adapt any IP Locator into a request Source by resolving the client IP first.
// No opts: uses clientip.Get(r) (respects an installed clientip.Middleware).
// With opts: uses clientip.Resolve(r, opts...). Keeps geoip/mmdb decoupled
// from clientip — the bridge lives here in core.
func FromLocator(loc Locator, opts ...clientip.Option) Source

// Try each source in order; return the first non-empty result. A source that
// returns a real error is logged and skipped (one broken source never blanks
// the whole chain).
func Chain(sources ...Source) Source
```

Header normalization is centralized: country upper-cased and validated as
2-alpha (CF placeholders `XX`/`T1` → empty ⇒ treated as a miss so `Chain`
falls through to mmdb), city URL-decoded, ASN parsed to `uint32`.

Beyond the three presets (Cloudflare + CloudFront + Vercel), other CDNs use a
`Headers(...)` literal or a small follow-up — deferred (YAGNI).

### Middleware, accessors, logging

```go
// src is a required collaborator (no ambient geo to default to).
func Middleware(src Source, opts ...Option) middleware.Middleware

func From(ctx context.Context) (Location, bool) // bool = Middleware ran (true even on empty)
func Get(r *http.Request) Location              // cached, or Location{} if Middleware didn't run

// Wire into logger.New(logger.WithContextExtractors(geoip.LogExtractor)).
// Emits slog.Group("geo", ...) with only the populated fields; nothing when empty.
var LogExtractor logger.ContextExtractor

func WithLogger(*slog.Logger) Option // for the Debug error log; default slog.Default()
```

The middleware resolves the `Location` **once per request** (eager, so every
log line during the request carries it) and caches it under
`ctxkey.New[Location]("geoip")`. On a miss or Source error it caches
`Location{}` (still "ran") and logs the error at `Debug` — enrichment is
best-effort and never fails the request. The per-request cost is one lookup
(µs for in-memory mmdb); scope with `subroute` to skip health/static routes.

### Wiring

```go
reader, err := mmdb.New(mmdb.WithCity(cityPath), mmdb.WithASN(asnPath))
// ...
src := geoip.Chain(geoip.Cloudflare(), geoip.FromLocator(reader))
mux2 := geoip.Middleware(src)(mux)
// in a handler:
loc := geoip.Get(r) // loc.CountryCode, loc.ASN, ...
```

## API surface — `web/geoip/mmdb`

```go
package mmdb

func New(opts ...Option) (*Reader, error) // *Reader implements geoip.Locator

// Lookup below returns geoip.Location (mmdb imports the parent for the type).

// Sources (per DB; either DB optional):
func WithCity(path string) Option      // file-backed → mmap (unix); read-into-[]byte elsewhere
func WithASN(path string) Option
func WithCityBytes(b []byte) Option    // in-memory (go:embed / downloaded)
func WithASNBytes(b []byte) Option
func WithInMemory() Option             // force read files into heap instead of mmap

func (*Reader) Lookup(ctx context.Context, ip netip.Addr) (Location, error)
func (*Reader) Reload(opts ...Option) error // atomic swap under RWMutex, munmaps the old
func (*Reader) Close() error                // munmaps
```

- **Load modes converge:** in-memory (`os.ReadFile` / `io.ReadAll`) and mmap
  (`syscall.Mmap`, build-tagged `mmap_unix.go`; `mmap_other.go` falls back to
  read-into-memory) both yield a `db []byte`; the decoder is mode-agnostic.
  Default is mmap for file paths (DB paged by the OS, not Go heap), in-memory
  for bytes.
- **Two-file merge:** city DB fills Country/Region/City/TimeZone; ASN DB fills
  ASN/ASNOrg; `Lookup` walks whichever are present and merges. `WithCity` may
  point at a country-only DB.
- **Concurrency:** a `{city, asn}` db-set is guarded by an `RWMutex`; `Lookup`
  holds `RLock` for its duration so `Reload`'s `Munmap` cannot free bytes
  under an in-flight read. Reads vastly outnumber (monthly) reloads → `RWMutex`
  justified.
- **No network.** The driver never downloads — the consumer fetches/gunzips
  the `.mmdb`. A `doc.go` recipe wires `Reload` into `async/scheduler` for
  periodic refresh.

### Reader internals (~500 LOC, three parts)

Validated against MaxMind's published `MaxMind-DB/test-data/*.mmdb` fixtures
plus a DB-IP sample.

1. **Metadata** — scan backward from EOF for the `\xAB\xCD\xEF MaxMind.com`
   marker, decode the metadata map. Reject `binary_format_major_version != 2`
   (`ErrUnsupportedFormat`), `record_size ∉ {24,28,32}` or a node_count
   inconsistent with file size (`ErrInvalidDatabase`). Precompute the IPv4
   start node once (walk 96 zero bits) so IPv4 lookups in a v6 DB are correct.
2. **Tree walk** — `netip.Addr` → bits; per bit read the left/right record
   (24/28/32-bit unpacking) until a value `== node_count` (miss → empty),
   `< node_count` (descend), or `> node_count` (data pointer). A mismatched
   `ip_version` (IPv6 into a v4 DB) → empty, not error.
3. **Data decoder** — control-byte type/size + pointer resolution, decoding
   **selectively into `Location`**: navigate only `country.iso_code`,
   `subdivisions[0].{iso_code,names.en}`, `city.names.en`,
   `location.time_zone`, and (ASN DB) `autonomous_system_{number,organization}`;
   skip everything else. No reflection, no intermediate map — only the ~6
   requested string fields are copied out.

### Errors

`errors.Is`-matchable single-line sentinels: `ErrNoDatabase` (no city/asn
given), `ErrInvalidDatabase` (bad magic/metadata/truncated), `ErrUnsupportedFormat`
(major version ≠ 2), `ErrClosed`.

## File anatomy

- `web/geoip`: `doc.go`, `location.go`, `source.go` (seams + `Chain` +
  `FromLocator`), `headers.go` (`Headers` + presets), `middleware.go`
  (+ `From`/`Get`/`LogExtractor`), `options.go`, `errors.go`.
- `web/geoip/mmdb`: `doc.go`, `mmdb.go` (`Reader`, `New`, `Lookup`, `Reload`,
  `Close`), `metadata.go`, `decode.go`, `mmap_unix.go` + `mmap_other.go`
  (build-tagged), `options.go`, `errors.go`.

## Testing

Black-box `_test` packages (per design.md):

- **Header sources:** table tests over crafted `*http.Request`s — present /
  absent / placeholder (`XX`, `T1`), URL-encoded city, malformed ASN.
- **`Chain` / `FromLocator`:** fakes for both seams; assert first-non-empty,
  error-skip, and IP resolution via `clientip`.
- **Middleware:** `From` / `Get` / `LogExtractor` semantics and the
  empty-on-miss "ran" signal.
- **`mmdb`:** decode against **committed tiny fixtures** (MaxMind official
  `*-Test.mmdb` + a DB-IP sample in `testdata/`, a few KB each) with known
  IP→value expectations; both load modes; `Reload` swap; `FuzzLookup` /
  `FuzzDecode` feeding truncated/corrupt bytes — every offset bounds-checked
  against `len(db)`, pointer/recursion depth capped (kills pointer-cycle
  infinite loops); must return an error, never panic or OOM.

## Performance

- Zero-alloc target on the header/middleware path (`useragent` precedent).
- Minimal-alloc mmdb lookup — only the requested string fields copied; no
  reflection, no intermediate map.
- `BenchmarkLookup` ships in the PR (design.md: perf-motivated complexity
  carries a benchmark; it also backs the "hand-roll is more efficient" claim).

## Dependencies

- `web/geoip` → `web/clientip`, `web/middleware`, `core/ctxkey`, `ops/logger`
  (all forge leaves).
- `web/geoip/mmdb` → `web/geoip` (parent, for `Location`/`Locator`) + **stdlib
  only** (`net/netip`, `os`, `io/fs`, `syscall`). No cycle: `geoip` core
  references only the `Locator` interface, never the concrete reader.

No external modules.

## Non-goals (anti-scope)

- **No built-in downloader/updater** — consumer fetches/gunzips the `.mmdb`; a
  `doc.go` recipe wires `Reload` into `async/scheduler`.
- **No lat/long** (dropped in option B) — addable non-breakingly later.
- **No country-name localization** — that's `i18n`'s job; geoip returns codes.
- **No proxy/VPN/anonymizer/reputation detection** (a separate paid DB
  product) and no reverse geocoding.
- **No country allow/deny middleware** — per decision D, consumers build
  policy on the cached `Location`.
