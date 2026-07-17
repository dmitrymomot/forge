# web/smartlink unified link engine — Design

Date: 2026-07-17. Status: approved for planning.

## Summary

`web/smartlink` grows from a pure destination-decision engine into the single link package: engine (unchanged) + a storage-backed link manager + a redirect handler with a uniform per-click pipeline. A stored link is a short code pointing at either an inline URL template (`Target`) or a consumer-side offer reference (`Ref`); both resolve through the same flow — Visit build, metadata merge, decorated `Decide`, redirect, async hit observer — so a "simple short link" is just the degenerate case (single default target, no rules) with every ability (macros, param forwarding, metadata stamping, hooks) intact.

This supersedes `web/shortlink` (PR #65, unmerged): its mechanics (Store contract with atomic tenant-predicate mutators, vanity codes + reserved blocklist, expiry/deactivation, scheme allowlist, cache read-through, fallback/no-store/never-301 handler behavior, pgstore + migration) are ported into the manager layer; the package itself is dropped from the catalog. PR #65 closes without merging. The engine PR #64 merges first as-is; this work lands as a new PR on top.

## Why one package

- A fixed-destination short link is a `Spec` with only `Default` — shortlink was a strict subset of smartlink, and keeping both meant catalog redundancy plus composition friction (a resolver seam in shortlink would have to import smartlink anyway).
- Hooks (fraud guard, visit tracking, postbacks) are wanted identically for one-target links and rule links; one pipeline serves both.
- Precedent: `async/queue` — one substantial package = engine + seams in the root, drivers isolated in subpackages (`smartlink/pgstore`). Stays within design.md layout rules.

## Package layout

```
web/smartlink/           engine (existing) + Decider/Chain + Cache helper
                         + Manager, Store seam, memory store, Handler, Hit
web/smartlink/pgstore/   postgres Store driver + migration
```

## Engine additions (root package, no new deps)

The existing engine (`Spec`/`Compile`/`Link.Decide`, matchers, templates, `ParamPolicy`) is untouched. Added on top:

```go
type Decider interface{ Decide(Visit) Decision }
type DecideFunc func(Visit) Decision            // adapter; implements Decider
type Decorator func(Decider) Decider            // middleware

// Chain composes left-to-right in application order: Chain(A,B,C)(d) == A(B(C(d))).
func Chain(ds ...Decorator) Decorator
```

The compiled artifact (`*Compiled` after the rename in §Stored record) already has `Decide(Visit) Decision` and satisfies `Decider` for free. Decorators cover sync concerns that must affect the current click: fraud/dup diversion (short-circuit with an alternative `Decision`), A/B overrides, metrics. Async concerns go through the handler's `OnHit` observer instead.

### Compile cache helper

Every consumer with `Ref` links writes the same lazy compile-with-invalidation cache, so the package ships it (pure engine level, stdlib only):

```go
func NewCache(load func(ctx context.Context, ref string) (Spec, error)) *Cache

func (c *Cache) Get(ctx context.Context, ref string) (*Compiled, error) // lazy load+Compile, cached
func (c *Cache) Invalidate(ref string)                              // called from the consumer's offer-save path
func (c *Cache) Resolver(ds ...Decorator) Resolver                  // ready-made Manager resolver, decorators applied
```

`sync.RWMutex` copy-on-read; a concurrent miss may compile twice (benign, documented). Load errors are not cached — the next click retries. Rule storage/admin stays consumer-side, as the engine doc already mandates.

### Visit facts

No new `Visit` fields. `StickyKey` doubles as the fingerprint carrier: the consumer sets it to their fingerprint digest, so weighted-split stickiness and fraud identity agree by construction. Documented on `Visit.StickyKey`.

## Stored record

```go
type Link struct {
	CreatedAt     time.Time         `json:"created_at"`
	ExpiresAt     time.Time         `json:"expires_at,omitzero"`     // zero = never
	DeactivatedAt time.Time         `json:"deactivated_at,omitzero"` // zero = active
	Code          string            `json:"code"`                    // globally unique path segment
	Target        string            `json:"target,omitempty"`        // inline URL template; "" when Ref set
	Ref           string            `json:"ref,omitempty"`           // consumer offer reference; "" when Target set
	Metadata      map[string]string `json:"metadata,omitempty"`      // fixed per-link params (affiliate/team identity)
	Tenant        string            `json:"tenant,omitempty"`
	ShortURL      string            `json:"short_url,omitempty"`     // derived (WithBaseURL), never stored
}
```

Name collision note: the engine's compiled `*Link` is renamed to avoid two `Link` types in one package — the compiled artifact becomes `*Compiled` (`Compile(Spec) (*Compiled, error)`), and the stored record owns the `Link` name. This is a rename inside unmerged #64 surface; no released API breaks. (Chosen over renaming the record, because "link" is what consumers store and list; the compiled artifact is an internal-feeling handle.)

Exactly one of `Target`/`Ref` is set — enforced at `Create`. Records are immutable except lifecycle (deactivate/activate/delete): destination changes happen consumer-side for `Ref` links (edit the offer, invalidate the cache) or by re-creating for `Target` links.

```go
type CreateParams struct {
	ExpiresAt time.Time
	Target    string            // inline URL template: absolute, host required, scheme on allowlist; macros compile-checked
	Ref       string            // consumer reference; validated via Resolver at create (see below)
	Code      string            // optional vanity code: 1–64 chars [A-Za-z0-9_-], not on the reserved blocklist
	Metadata  map[string]string
	Tenant    string            // constrained by WithScope when configured
	// SkipRefCheck skips create-time Resolver validation of Ref, for
	// offers that will exist later.
	SkipRefCheck bool
}
```

## Manager

```go
func NewManager(store Store, opts ...Option) (*Manager, error)
```

Ported from #65 semantics: collision-retried code generation, vanity codes + reserved-word blocklist, expiry/deactivation, `WithScope` fail-closed tenancy on management ops (public resolve needs no scope — a code is a public URL), optional cache read-through with the bounded-staleness contract, delete-then-retry cache handling.

Options:

- `WithCodeFunc(func() string)` — code generator seam. **Default: `id.Short` lowercase** (16-char Crockford base32, URL-safe, collision-free by construction, sortable; creation-time leak documented). `RandomCode(n int) func() string` ships as a base58 alternative for 7–8 char pretty codes (collision-retried against `ErrDuplicate`, as in #65).
- `WithBaseURL(string)` — populates `Link.ShortURL` on returned records.
- `WithSchemes(...string)` — `Target` scheme allowlist, default http/https.
- `WithReservedCodes(...string)` — extends the vanity blocklist.
- `WithScope(func(ctx) (string, error))` — tenant hook, fail-closed (error or empty aborts), management ops only.
- `WithCache(cache.Store)` — read-through for resolve; #65 bounded-staleness contract carries over.
- `WithLinkParamPolicy(ParamPolicy)` — policy for the degenerate specs compiled from `Target` links. **Default `ParamsFill`** (incoming params forwarded, target's own params win) — the engine zero value `ParamsDrop` would silently break param forwarding for the simple case this design exists to serve. `Ref` links carry their policy in the consumer's `Spec`.
- `WithVisitFunc(func(*http.Request, Visit) Visit)` — enricher, not builder: the handler pre-fills `Visit.Params` from the query string (first value per key), the consumer adds country/device/sticky-key from their own middleware (geoip, useragent, fingerprint — smartlink imports none of them). Optional: without it, param-only Visits still work and geo/device rules fall through to defaults.
- `WithResolver(Resolver)` where `type Resolver func(ctx context.Context, l Link) (Decider, error)` — required before any `Ref` link resolves; returning `ErrNoTarget` marks the link dead (fallback/404), any other error is an outage (500).
- `WithOnHit(func(ctx context.Context, h Hit))` — post-redirect observer, called synchronously; hand the Hit to a bounded sink (queue push, buffered channel), never do work inline or spawn per-hit goroutines. Same contract #65 documented.
- `WithFallbackURL(string)`, `WithRedirectStatus(int)` — dead-link fallback and 302/307 choice; 301 is rejected (a cached 301 kills hit observation forever).
- `WithLogger(*slog.Logger)` — default `logger.NewNope()`.

Create-time `Ref` validation: when a Resolver is configured, `Create` calls it once — a typo'd ref fails at mint time (and warms the compile cache) instead of 404ing on the affiliate's first click. `CreateParams`-level opt-out flag (`SkipRefCheck bool`) for offers that will exist later.

```go
type Hit struct {
	Link     Link     // includes Metadata → attribution for postback/tracking
	Visit    Visit    // as decided (metadata already merged into Params)
	Decision Decision // final URL + matched rule
}
```

## Store seam

The #65 contract verbatim, with the record fields above (`Target`/`Ref`/`Metadata` replace `URL`):

```go
type Store interface {
	Create(ctx context.Context, l Link) error                              // ErrDuplicate on existing code
	Get(ctx context.Context, code string) (Link, error)                    // ErrNotFound
	List(ctx context.Context, f Filter) ([]Link, error)                    // newest first
	Deactivate(ctx context.Context, code, tenant string, at time.Time) error
	Activate(ctx context.Context, code, tenant string) error
	Delete(ctx context.Context, code, tenant string) error
}
```

Mutators keep the atomic tenant-predicate rule (one statement, not check-then-act — codes are reusable after delete and vanity codes user-claimable, so a pre-check could race a delete-and-recreate and mutate another tenant's link). Codes match case-sensitively. `NewMemoryStore()` ships in the root for tests/dev; `pgstore` is the production driver (`forge_smart_links` table: code PK, target, ref, metadata jsonb, tenant, timestamps; goose migration via `migration.Migration`).

## Per-click pipeline (uniform for both modes)

```
lookup code (cache read-through)
→ expired/deactivated/unknown? → WithFallbackURL redirect, else 404
→ Visit{Params: query} → VisitFunc enrich
→ merge link.Metadata into Visit.Params (metadata wins — it identifies the link, not the click)
→ Decider: Target → compile degenerate Spec{Default, WithLinkParamPolicy}   (per-hit; see perf note)
           Ref    → Resolver(ctx, link)                                     (consumer cache + Chain)
→ Decide → redirect (302/307, Cache-Control: no-store)
→ OnHit(ctx, Hit)
```

Error semantics (from #65): store/cache failure or resolver failure (other than `ErrNoTarget`) → 500 — an outage must read as an outage, not as every link being gone. Missing Resolver with a `Ref` link is a configuration error → 500 and an error log.

Perf note: degenerate `Target` specs are compiled per hit in v1 — a single-template compile is a few µs and an unbounded per-code compiled-link cache is a memory liability at affiliate-code cardinality. The required `bench_test.go` covers the full resolve→redirect path; a bounded LRU is added only if the benchmark proves the compile matters (design.md §Performance: readable first, optimize proven-hot paths with numbers in the PR).

Metadata merge happens before `Decide`, so `ParamEquals` rules can branch per-affiliate, and merged params flow into macros (`{param.affiliate_id}`), the final URL (per policy), and `Hit`.

## Anti-scope

- No fraud/bot/dup detection — that's a consumer decorator (sync divert) or OnHit consumer (async scoring feeding a denylist the decorator reads).
- No postback delivery, no click counting, no analytics storage — `Hit` hands the consumer everything; `comms/postback` (catalog) owns tracker postbacks.
- No offer/spec persistence — rule storage/admin stays consumer-side; `NewCache` only caches compiles.
- No QR codes, no link-in-bio, no preview pages.

## Catalog & PR mechanics

- packages.md: rewrite the `web/smartlink` entry (engine + link manager + pipeline), delete the `web/shortlink` entry, fix cross-references (`magiclink`, `attribution`, engine doc "not shortlink" notes).
- PR #64 (engine) merges first, unchanged except the `Link`→`Compiled` rename noted above (amended on that branch before merge, or as the first commit of the new PR — decided at planning).
- PR #65 (shortlink) closes without merging, with a comment pointing at this design.
- This design lands as one new PR: engine additions + manager + memory store + pgstore + catalog updates. Benchmarks (engine ones exist in #64; manager/handler added) + before/after numbers per repo policy.

## Testing

- Black-box (`smartlink_test`) throughout; white-box only where unexported state demands (compile cache internals).
- Unit tier (default `go test`, Docker-free): engine additions, Chain semantics, Cache invalidation/error-not-cached, manager over memory store, handler pipeline incl. metadata-wins merge, fallback/status/no-store, Ref-without-resolver 500, create-time ref validation, vanity/blocklist, code collision retry, scope fail-closed.
- Integration tier (`//go:build integration`, testcontainers pg16): pgstore conformance incl. atomic tenant-predicate mutators and delete-recreate race shape.
- Bench: resolve→redirect for `Target` (compile-per-hit) and `Ref` (cache hit) paths, `Chain` overhead, memory-store ops.
