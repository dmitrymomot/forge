# web/smartlink Unified Link Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `web/smartlink` into the single link package — engine (seeded verbatim from the #64 branch) + storage-backed link Manager + redirect Handler with one uniform per-click pipeline for both inline-URL and offer-ref links.

**Architecture:** Engine stays a pure decision core (`Spec`→`Compile`→`Compiled.Decide`); a `Decider`/`Chain` decorator seam and a compile `Cache` sit on top of it; the Manager owns short codes over a `Store` seam (memory + `pgstore`) and serves clicks through a Handler: lookup → Visit build/enrich → metadata merge → decorated Decide → redirect → async `OnHit`. Spec: `docs/superpowers/specs/2026-07-17-smartlink-unified-design.md` — read it before starting; it is the source of truth. Plans are direction, not transcription: implement the behavior, don't copy sketches verbatim.

**Tech Stack:** Go stdlib + forge-internal deps only: `core/clock` (engine), `core/id` + `core/random` (codes), `resilience/cache` (read-through seam), `ops/logger` (nope default), `data/migration` + pgx (pgstore only).

## Global Constraints

- Branch: work on `dm/smartlink-shortlink-consolidation-25c16a` (current worktree branch, already holds the spec). Never switch branches.
- Do NOT re-read the #65 (shortlink) diff — the spec encodes everything worth keeping from it.
- `just fmt ./web/smartlink/...` after file changes (never single-file fmt — betteralign quirk); `just lint` when a task finishes.
- Tests: black-box (`package smartlink_test`) unless unexported state demands white-box. Default `go test` tier must stay Docker-free; pgstore tests behind `//go:build integration`.
- Optional `*slog.Logger` defaults to `logger.NewNope()`, never `slog.Default()`.
- Go 1.26: use `new(expr)`, run modernize via `just lint`.
- `bench_test.go` required; before/after numbers go in the PR body.
- Redirects: 302 default, 307 allowed, 301 rejected at construction. `Cache-Control: no-store` on every handler response.
- No manual line-wrapping in any prose/PR/commit body.

---

### Task 1: Seed engine from #64 and rename Link→Compiled

**Files:**
- Create (verbatim from branch): `web/smartlink/{compile,decide,errors,matcher,options,rule,template,visit,doc}.go` + `{compile,decide}_test.go`, `bench_test.go`, `matcher` tests — whatever `git ls-tree origin/dm/web-smartlink-package-e1168c -- web/smartlink` lists.
- Modify: all seeded files (rename only).

**Interfaces (produced):** engine API used by every later task — `Compile(Spec, ...Option) (*Compiled, error)`, `(*Compiled).Decide(Visit) Decision`, `Spec{Rules, Default, Params ParamPolicy}`, `Rule{Name, When []Matcher, Targets []Target}`, `Target{URL string, Weight int}`, `Visit{At, Params map[string]string, Country, Device, Locale, StickyKey}`, `Decision{Rule, URL string, Target Target}`, `ParamsDrop/ParamsFill/ParamsOverride`, matchers `Geo/Device/Locale/ParamEquals/TimeWindow/Percent`, `WithClock`, errors `ErrNoDefault/ErrInvalidRule/ErrInvalidTarget/ErrInvalidMatcher/ErrInvalidTemplate/ErrUnknownMacro`.

- [ ] **Step 1: Seed the files** — `git fetch origin dm/web-smartlink-package-e1168c && git checkout origin/dm/web-smartlink-package-e1168c -- web/smartlink` (one mechanical commit; do not hand-edit while seeding).
- [ ] **Step 2: Verify seed builds and tests pass** — `go test ./web/smartlink/...` → PASS. Commit: `feat(smartlink): seed decision engine from PR #64 branch`.
- [ ] **Step 3: Rename** the compiled artifact: `type Link` → `type Compiled` in `compile.go`, all `*Link` receivers/returns in `compile.go`/`decide.go`, all test references, and the doc.go mention. Mechanical: `gofmt -r 'Link -> Compiled'` is WRONG here (would rename unrelated idents) — use grep-guided manual edits; `git grep -n '\bLink\b' web/smartlink` must return zero hits afterwards.
- [ ] **Step 4: Test + fmt** — `go test ./web/smartlink/...` PASS, `just fmt ./web/smartlink/...`.
- [ ] **Step 5: Commit** — `refactor(smartlink): rename compiled Link to Compiled, freeing Link for the stored record`.

### Task 2: Decider, DecideFunc, Decorator, Chain

**Files:**
- Create: `web/smartlink/decider.go`, Test: `web/smartlink/decider_test.go`

**Interfaces:**
- Consumes: `Visit`, `Decision`, `*Compiled` (Task 1).
- Produces: `type Decider interface{ Decide(Visit) Decision }`; `type DecideFunc func(Visit) Decision` (implements Decider); `type Decorator func(Decider) Decider`; `func Chain(ds ...Decorator) Decorator` with `Chain(A,B,C)(d) == A(B(C(d)))`.

- [ ] **Step 1: Failing tests** (black-box): `TestCompiledIsDecider` (`var _ smartlink.Decider = (*smartlink.Compiled)(nil)` + a compiled link decides through the interface); `TestChainOrder` — decorators A, B, C each append their tag to `Decision.Rule` before delegating; assert application order A(B(C(d))) i.e. innermost C runs closest to d and A sees the final result; `TestChainEmpty` — `Chain()(d)` returns d's decisions unchanged.
- [ ] **Step 2: Run, verify FAIL** (`go test ./web/smartlink/ -run 'Decider|Chain'`).
- [ ] **Step 3: Implement** — ~15 lines: DecideFunc method, Chain loops `for i := len(ds)-1; i >= 0; i--`.
- [ ] **Step 4: Run tests → PASS; `just fmt ./web/smartlink/...`.**
- [ ] **Step 5: Commit** — `feat(smartlink): Decider seam with DecideFunc adapter and Chain decorator composition`.

### Task 3: Stored record, errors, Store seam, memory store

**Files:**
- Create: `web/smartlink/link.go` (record + CreateParams + Filter), `web/smartlink/store.go` (Store interface), `web/smartlink/memory.go`, `web/smartlink/manage_errors.go` (or extend `errors.go`)
- Test: `web/smartlink/memory_test.go`

**Interfaces:**
- Produces:

```go
type Link struct { // json tags per spec; ShortURL derived, never persisted
	CreatedAt, ExpiresAt, DeactivatedAt time.Time
	Code, Target, Ref string
	Metadata map[string]string
	Tenant, ShortURL string
}
type CreateParams struct {
	ExpiresAt time.Time
	Target, Ref, Code string
	Metadata map[string]string
	Tenant string
	SkipRefCheck bool
}
type Filter struct{ Tenant string; Limit int }
type Store interface {
	Create(ctx context.Context, l Link) error                                // ErrDuplicate on existing code
	Get(ctx context.Context, code string) (Link, error)                      // ErrNotFound
	List(ctx context.Context, f Filter) ([]Link, error)                      // CreatedAt desc, code asc ties
	Deactivate(ctx context.Context, code, tenant string, at time.Time) error // atomic tenant predicate
	Activate(ctx context.Context, code, tenant string) error
	Delete(ctx context.Context, code, tenant string) error
}
func NewMemoryStore() *MemoryStore
```

- Errors: `ErrNotFound`, `ErrDuplicate`, `ErrLinkExpired`, `ErrLinkDeactivated`, `ErrNoTarget`, `ErrInvalidLink`, `ErrCodeReserved`, `ErrScope` — all `errors.New("smartlink: ...")`.

**Store contract notes (from spec):** mutators with non-empty tenant apply only when the record belongs to that tenant, else `ErrNotFound`; empty tenant unconstrained; predicate atomic with the mutation (codes are reusable after Delete, vanity codes user-claimable — a pre-check races delete-and-recreate). Codes case-sensitive. Memory store: `map[string]Link` + `sync.RWMutex`, clone Metadata maps on both write and read (no aliasing), zero `at` in Deactivate leaves the link active.

- [ ] **Step 1: Failing tests** — `TestMemoryStoreCreateGet` (roundtrip, ErrDuplicate on same code), `TestMemoryStoreGetUnknown` (ErrNotFound), `TestMemoryStoreListOrder` (newest first, code-asc ties, tenant filter, limit), `TestMemoryStoreTenantPredicate` (deactivate/activate/delete with wrong tenant → ErrNotFound + record untouched; empty tenant → applies), `TestMemoryStoreMetadataAliasing` (mutating caller's map after Create and returned map after Get doesn't change stored state), `TestMemoryStoreDeleteRecreate` (delete then create same code succeeds).
- [ ] **Step 2: Run → FAIL.** **Step 3: Implement.** **Step 4: Run → PASS; fmt.**
- [ ] **Step 5: Commit** — `feat(smartlink): stored Link record, Store seam, in-memory store`.

### Task 4: Compile cache + Resolver type

**Files:**
- Create: `web/smartlink/cache.go`, Test: `web/smartlink/cache_test.go`

**Interfaces:**
- Consumes: `Spec`, `Compile`, `*Compiled`, `Decider`, `Decorator`, `Chain` (Tasks 1–2), `Link`, `ErrNoTarget` (Task 3).
- Produces:

```go
type Resolver func(ctx context.Context, l Link) (Decider, error)
func NewCache(load func(ctx context.Context, ref string) (Spec, error), compileOpts ...Option) *Cache
func (c *Cache) Get(ctx context.Context, ref string) (*Compiled, error)
func (c *Cache) Invalidate(ref string)
func (c *Cache) Resolver(ds ...Decorator) Resolver // Get(l.Ref), wrap with Chain(ds...); load error wrapped, not cached
```

Behavior: RLock fast path; on miss, load + Compile outside the write lock, then store (concurrent double-compile is benign — document on NewCache). Errors (load or compile) are never cached; next Get retries. `Resolver` returns the decorated decider; it does not special-case ErrNoTarget — the consumer's load func returns `ErrNoTarget` (wrapped ok) for a paused/deleted offer and the Manager maps that to dead-link handling (Task 6).

- [ ] **Step 1: Failing tests** — `TestCacheGetCompilesOnce` (counting load func; two Gets, one load), `TestCacheInvalidate` (Get, Invalidate, Get → two loads), `TestCacheLoadErrorNotCached` (first load errors, second succeeds), `TestCacheCompileErrorPropagates` (invalid Spec → error wrapping the Compile error), `TestCacheResolver` (Resolver with a tagging Decorator resolves a Link{Ref:...} and decides through the decoration).
- [ ] **Step 2: FAIL.** **Step 3: Implement.** **Step 4: PASS; fmt.**
- [ ] **Step 5: Commit** — `feat(smartlink): lazy compile cache with invalidation and ready-made Resolver`.

### Task 5: Manager — construction, codegen, Create, lifecycle, scope

**Files:**
- Create: `web/smartlink/manager.go`, `web/smartlink/manager_options.go`, `web/smartlink/codes.go`
- Test: `web/smartlink/manager_test.go`, `web/smartlink/scope_test.go`

**Interfaces:**
- Consumes: Store/Link/CreateParams/Filter/errors (Task 3), Resolver (Task 4), engine Compile for Target validation (Task 1).
- Produces:

```go
func NewManager(store Store, opts ...ManagerOption) (*Manager, error)
type ManagerOption func(*managerConfig)
func WithCodeFunc(f func() string) ManagerOption            // default: func() string { return id.NewShort().StringLower() }
func RandomCode(n int) func() string                        // base58 alphabet "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz" via random.String
func WithBaseURL(u string) ManagerOption                    // trailing-slash normalized; validated absolute http(s)
func WithSchemes(s ...string) ManagerOption                 // default http, https
func WithReservedCodes(codes ...string) ManagerOption       // extends default blocklist
func WithScope(f func(ctx context.Context) (string, error)) ManagerOption
func WithLinkParamPolicy(p ParamPolicy) ManagerOption       // default ParamsFill (see spec — zero value ParamsDrop would break forwarding)
func WithResolver(r Resolver) ManagerOption
func WithVisitFunc(f VisitFunc) ManagerOption               // declare here: type VisitFunc func(*http.Request, Visit) Visit
func WithOnHit(f func(context.Context, Hit)) ManagerOption  // declare here: type Hit struct{ Link Link; Visit Visit; Decision Decision }
func WithCache(cs cache.Store, ttl time.Duration) ManagerOption
func WithFallbackURL(u string) ManagerOption
func WithRedirectStatus(code int) ManagerOption             // 302/307 only; 301 or anything else → NewManager error
func WithLogger(l *slog.Logger) ManagerOption               // default logger.NewNope()

func (m *Manager) Create(ctx context.Context, p CreateParams) (Link, error)
func (m *Manager) Get(ctx context.Context, code string) (Link, error)      // management read: scope-filtered
func (m *Manager) List(ctx context.Context, f Filter) ([]Link, error)
func (m *Manager) Deactivate(ctx context.Context, code string) error
func (m *Manager) Activate(ctx context.Context, code string) error
func (m *Manager) Delete(ctx context.Context, code string) error
func (m *Manager) ShortURL(code string) string                              // "" without WithBaseURL
```

**Create semantics:** exactly one of Target/Ref set, else `ErrInvalidLink`. Vanity Code: 1–64 chars of `[A-Za-z0-9_-]` (`ErrInvalidLink`), not in blocklist (`ErrCodeReserved`), `ErrDuplicate` surfaces. Generated code: call codeFunc, retry on `ErrDuplicate` up to 5 times, then give up with a wrapped error. Target: compile `Spec{Default: []Target{{URL: p.Target}}, Params: cfg.linkParamPolicy}` — compile error → wrapped `ErrInvalidLink`; then scheme check: macro-elide the raw target (strip `{...}` spans), `url.Parse`, scheme must be on the allowlist and non-empty; require non-empty host only when the authority contains no macro (dynamic-host templates stay legal). Ref + Resolver configured + !SkipRefCheck: call `cfg.resolver(ctx, candidate)` once, error → wrapped `ErrInvalidLink` (warms consumer cache for free). Metadata cloned. `CreatedAt` set via `time.Now().UTC()`. Returned Link gets ShortURL populated.

**Scope semantics** (fail-closed, management ops only): when WithScope is set, every management method calls it; error or empty string → `ErrScope`. Create: empty `p.Tenant` → scope tenant; non-empty and different → `ErrScope`. Get: fetch then compare record.Tenant, mismatch → `ErrNotFound`. List: force `f.Tenant` to scope tenant. Deactivate/Activate/Delete: pass scope tenant as the store predicate. Without WithScope: tenant strings pass through verbatim (single-tenant zero ceremony).

Default reserved codes: `api, admin, app, assets, static, health, healthz, metrics, favicon.ico, robots.txt, login, logout, signup, docs, status, www, .well-known`.

- [ ] **Step 1: Failing tests** — construction: `TestNewManagerRejects301`, `TestNewManagerBadBaseURL`; create: `TestCreateTargetXorRef` (both/neither → ErrInvalidLink), `TestCreateGeneratedCodeDefault` (16-char lowercase Crockford, parses via `id.ParseShort`), `TestCreateCollisionRetry` (stubbed codeFunc returning a taken code twice then fresh — succeeds; always-colliding → error), `TestCreateVanity` (valid, invalid chars, >64, reserved → ErrCodeReserved, duplicate → ErrDuplicate), `TestCreateTargetValidation` (bad template, disallowed scheme, scheme-relative, macro-host allowed, plain host required), `TestCreateRefValidation` (resolver error → ErrInvalidLink; SkipRefCheck bypasses; no resolver configured → no check), `TestCreateMetadataCloned`, `TestShortURL` (with/without base, slash normalization, populated on Create result); lifecycle: `TestLifecycle` (deactivate/activate/delete via manager reach the store); scope (scope_test.go): `TestScopeFailClosed` (hook error / empty → ErrScope on every management op), `TestScopeCreateTenantMismatch`, `TestScopeGetForeignTenant` (→ ErrNotFound), `TestScopeListForced`, `TestScopeMutatorsUsePredicate` (foreign record untouched), `TestUnscopedPassthrough`; codes: `TestRandomCode` (length, alphabet).
- [ ] **Step 2: FAIL.** **Step 3: Implement.** **Step 4: PASS; fmt.**
- [ ] **Step 5: Commit** — `feat(smartlink): link Manager — codegen, create-time validation, lifecycle, fail-closed scope`.

### Task 6: Resolve, cache read-through, Handler pipeline, Hit

**Files:**
- Create: `web/smartlink/resolve.go`, `web/smartlink/handler.go`
- Test: `web/smartlink/resolve_test.go`, `web/smartlink/handler_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:

```go
// VisitFunc and Hit are declared in Task 5's options file; this task implements their behavior.
func (m *Manager) Resolve(ctx context.Context, code string) (Link, error) // public read: cache read-through + liveness checks
func (m *Manager) Handler() http.Handler
```

**Resolve:** cache read-through when WithCache set — key `"smartlink:code:" + code`, JSON round-trip of Link, `cache.WithTTL(ttl)`; hit → decode; miss/any cache error → store Get, best-effort Set (log at debug on cache errors, never fail the resolve). Then liveness: `DeactivatedAt` non-zero → `ErrLinkDeactivated`; `ExpiresAt` non-zero and `<= now` → `ErrLinkExpired`. Bounded staleness documented: lifecycle mutations best-effort `Delete` the cache key (add that to Task 5's Deactivate/Activate/Delete — do it in this task as a small modify of manager.go), so a stale active record lives at most ttl. No negative caching.

**Handler pipeline** (per spec, uniform for Target/Ref):

```go
code := r.PathValue("code"); if code == "" { code = strings.Trim(r.URL.Path, "/") }
w.Header().Set("Cache-Control", "no-store")
l, err := m.Resolve(r.Context(), code)
// dead: ErrNotFound/ErrLinkExpired/ErrLinkDeactivated → fallback redirect or 404
// any other err → 500 (outage reads as outage), log error
v := Visit{Params: firstValues(r.URL.Query())}      // first value per key
if cfg.visitFunc != nil { v = cfg.visitFunc(r, v) }
for k, val := range l.Metadata { if v.Params == nil {...}; v.Params[k] = val } // metadata wins
var d Decider
switch {
case l.Target != "": d, err = m.compileTarget(l)     // per-hit degenerate compile; err "unreachable" (validated at Create) → 500 + log
case cfg.resolver == nil: 500 + log "Ref link with no resolver configured"
default: d, err = cfg.resolver(r.Context(), l)
    // errors.Is(err, ErrNoTarget) → dead-link path (fallback/404); other err → 500 + log
}
dec := d.Decide(v)
http.Redirect(w, r, dec.URL, cfg.redirectStatus)
if cfg.onHit != nil { cfg.onHit(r.Context(), Hit{Link: l, Visit: v, Decision: dec}) } // sync, after redirect written
```

`compileTarget` = `Compile(Spec{Default: []Target{{URL: l.Target}}, Params: cfg.linkParamPolicy})` — no caching in v1 (spec §perf note; bench in Task 9 decides if an LRU is ever warranted).

- [ ] **Step 1: Failing tests** — resolve: `TestResolveLiveness` (active ok; expired/deactivated sentinels; unknown ErrNotFound), `TestResolveCacheReadThrough` (fake cache.Store: second Resolve served from cache, store not hit), `TestResolveCacheErrorFallsThrough` (failing cache → store still serves), `TestLifecycleInvalidatesCache` (deactivate → cache key deleted); handler (httptest): `TestHandlerTargetRedirect` (302, Location, no-store header), `TestHandlerParamForwarding` (incoming `?click_id=x` lands in final URL under default ParamsFill; target's own param wins on collision), `TestHandlerMetadataWins` (link Metadata overrides same-key query param, reaches macros `{param.affiliate_id}`), `TestHandlerVisitFuncEnriches` (geo rule matches only when VisitFunc sets Country; without VisitFunc falls to default target), `TestHandlerRefResolves` (resolver + Cache from Task 4 end-to-end), `TestHandlerRefNoTarget` (resolver returns wrapped ErrNoTarget → fallback URL when set, else 404), `TestHandlerRefNoResolver` (→ 500), `TestHandlerDeadLink` (unknown/expired/deactivated → fallback or 404), `TestHandlerStoreOutage` (erroring store → 500), `TestHandlerOnHit` (Hit carries Link.Metadata, merged Visit, final Decision; fires only on successful redirect), `TestHandler307` (WithRedirectStatus(307)), `TestHandlerPathFallback` (mounted without pattern, path-trim extraction).
- [ ] **Step 2: FAIL.** **Step 3: Implement** (resolve.go + handler.go + small manager.go modify for cache invalidation). **Step 4: PASS; fmt.**
- [ ] **Step 5: Commit** — `feat(smartlink): public Resolve with cache read-through and the uniform redirect Handler pipeline`.

### Task 7: pgstore driver + migration + integration tests

**Files:**
- Create: `web/smartlink/pgstore/doc.go`, `web/smartlink/pgstore/pgstore.go`, `web/smartlink/pgstore/migrations/00001_create_forge_smart_links.sql`
- Test: `web/smartlink/pgstore/pgstore_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes: `smartlink.Store` contract, `smartlink.Link/Filter`, sentinel errors (Task 3).
- Produces: `pgstore.New(pool *pgxpool.Pool) *Store` (implements `smartlink.Store`), `pgstore.Migrations fs.FS`.

Follow the repo pgstore idiom exactly (see `auth/apikey/pgstore`): `//go:embed migrations/*.sql` + `fs.Sub` root (data/migration globs fsys root), version table `forge_smartlink_schema`, pool lifecycle is the caller's, `pgconn.PgError` 23505 → `ErrDuplicate`, `pgx.ErrNoRows` → `ErrNotFound`.

Migration:

```sql
-- +goose Up
CREATE TABLE forge_smart_links (
    code           text PRIMARY KEY,
    target         text NOT NULL DEFAULT '',
    ref            text NOT NULL DEFAULT '',
    metadata       jsonb NOT NULL DEFAULT '{}',
    tenant         text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL,
    expires_at     timestamptz,
    deactivated_at timestamptz
);
CREATE INDEX forge_smart_links_tenant_created_idx ON forge_smart_links (tenant, created_at DESC, code);
-- +goose Down
DROP TABLE forge_smart_links;
```

Mutators are single statements with the tenant predicate inline, e.g. `UPDATE forge_smart_links SET deactivated_at = $3 WHERE code = $1 AND ($2 = '' OR tenant = $2)` → `RowsAffected() == 0` ⇒ `ErrNotFound`. NULL timestamptz ↔ zero `time.Time` via the nullable-scan helpers idiom. List: `ORDER BY created_at DESC, code ASC`, optional `tenant =` filter, `LIMIT` when > 0.

- [ ] **Step 1: Write integration tests first** (mirror the memory_test.go suite: roundtrip/duplicate/not-found/list order+filter+limit/tenant-predicate mutators/delete-recreate/metadata fidelity incl. nil vs empty map, zero vs set timestamps). Setup via `testkit/pgtest.DSN(t)` + `data/postgres.Open` + `data/migration` applying `pgstore.Migrations` (copy the `newStore` helper shape from `auth/apikey/pgstore/pgstore_test.go`).
- [ ] **Step 2: Run → FAIL** (`just test-integration ./web/smartlink/...`).
- [ ] **Step 3: Implement pgstore.** **Step 4: `just test-integration ./web/smartlink/...` → PASS; fmt.**
- [ ] **Step 5: Commit** — `feat(smartlink/pgstore): postgres Store driver with goose migration`.

### Task 8: doc.go rewrite, catalog update, lint sweep

**Files:**
- Modify: `web/smartlink/doc.go`, `docs/packages.md`

- [ ] **Step 1: Rewrite doc.go** — keep the engine paragraphs (updated for `Compiled`), add the manager/handler story: unified record (Target template XOR Ref), uniform pipeline, hooks contract (decorators sync, OnHit bounded-sink), tenancy (WithScope management-only, codes globally unique), the `Visit.StickyKey` doubles-as-fingerprint-carrier note (spec §Visit facts — add it to the StickyKey field comment in visit.go too), `Example` (engine-only, existing) + `Example_manager` (memory store, Create fixed link, Handler over httptest — runnable). Godoc conventions per repo.
- [ ] **Step 2: packages.md** — delete the whole `web/shortlink` entry; rewrite `web/smartlink` entry: engine + link manager + pipeline, one short paragraph each; Deps line: `core/clock`, `core/id`, `core/random`, `resilience/cache`, `ops/logger`. `grep -rn shortlink docs/ --include='*.md' | grep -v superpowers` must show zero remaining refs (specs/plans stay).
- [ ] **Step 3: Full check** — `go test ./web/smartlink/...`, `just lint` (fix everything), `just fmt ./web/smartlink/...`.
- [ ] **Step 4: Commit** — `docs(smartlink): unified package docs; drop web/shortlink from catalog`.

### Task 9: Benchmarks + measured optimization pass

**Files:**
- Modify: `web/smartlink/bench_test.go` (engine benches exist from seed — add manager/handler benches)

- [ ] **Step 1: Add benches** — `BenchmarkHandlerTarget` (memory store, fixed link, httptest.NewRecorder loop: full lookup→compile-per-hit→decide→redirect), `BenchmarkHandlerRef` (Cache-backed resolver hit path), `BenchmarkChain3` (3 no-op decorators vs bare Decide), `BenchmarkMemoryStoreGet`. Report allocs (`b.ReportAllocs()`).
- [ ] **Step 2: Baseline** — `go test ./web/smartlink/ -bench . -benchmem -run xxx | tee /tmp/smartlink-bench-before.txt` (save numbers for PR body).
- [ ] **Step 3: Optimization pass, measured wins only** — expected hotspots: per-hit degenerate compile in `BenchmarkHandlerTarget` (if it dominates, the spec sanctions a bounded LRU — only add with before/after numbers), Visit params map allocs, JSON round-trip in cache path. Skip anything the numbers don't justify; readable first.
- [ ] **Step 4: After-numbers** — rerun, save; both files' numbers go in the PR body. Commit: `perf(smartlink): benchmark manager/handler paths` (+ any measured opt commits separately).

### Task 10: Ship — push, PR, close #64/#65, CI/review loop

- [ ] **Step 1: Final verification** — `go test ./web/smartlink/...`, `just test-integration ./web/smartlink/...` (Docker up), `just lint` — all green, then `git status` clean.
- [ ] **Step 2: Push + PR** — `git push -u origin dm/smartlink-shortlink-consolidation-25c16a`; `gh pr create` titled `feat(smartlink): unified link engine — decision core + short-code manager + redirect pipeline (absorbs shortlink)`; body: summary, design/spec link, what was seeded from #64 vs written new, bench before/after, test tiers. NO Claude attribution lines anywhere.
- [ ] **Step 3: Close the superseded PRs** — comment + close: #64 (`superseded: engine landed verbatim as the seed commits of <new PR url>; Link renamed Compiled per the unified design`) and #65 (`superseded by the unified web/smartlink design (docs/superpowers/specs/2026-07-17-smartlink-unified-design.md); behaviors ported per spec, code not merged`). `gh pr close 64 --comment "..."`, same for 65.
- [ ] **Step 4: CI/review loop per CLAUDE.md** — wait for all CI; fix failures; read Claude's review (note: the review workflow can time out silently on big PRs — if it posts nothing, run a local review pass instead); fix all real findings, resolve fixed threads, commit, repeat until clean.
