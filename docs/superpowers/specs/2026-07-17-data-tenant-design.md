# data/tenant — Design

Date: 2026-07-17. Catalog entry: `docs/packages.md` (data/tenant). Deps: none (stdlib only).

## Purpose

The multi-tenancy package: resolve which tenant an inbound request (or job) belongs to, carry the tenant ID through context transport-agnostically, and scope SQL queries to that tenant with explicit, parameterized fragments — visible at every query, never auto-injected.

`tenant.Scope` is the canonical scope hook the rest of forge composes with: every package exposing `WithScope(fn func(context.Context) (string, error))` accepts it directly.

## Non-goals

- No tenant registry/CRUD (consumer domain; `DomainLookup` is the only storage seam).
- No automatic query rewriting or ORM hooks — ScopeClause is built and concatenated by the consumer, visibly.
- No slug→canonical-ID mapping: resolvers return the raw identifier they saw; consumers that key on something else wrap a `Resolver` (it is a plain func) or map inside `DomainLookup`.
- No path rewriting in `PathPrefix` — routing stays the consumer's concern.

## API

### Carrier (tenant.go)

```go
func NewContext(ctx context.Context, id string) context.Context
func FromContext(ctx context.Context) (string, bool)   // false when absent or empty
func Scope(ctx context.Context) (string, error)        // fail-closed: ErrNoTenant when absent/empty
```

`Scope`'s signature intentionally matches every forge `WithScope` option: `apikey.WithScope(tenant.Scope)`.

The context key is an unexported package-level key (stdlib-only; same collision-free shape as `core/ctxkey` without the dep).

### Resolver seam (resolver.go)

```go
type Resolver func(r *http.Request) (string, error)
```

Contract: `("", nil)` = not resolved, chain continues; non-empty = resolved; error = stop the chain (middleware responds 500). Shipped constructors:

- `Subdomain(base string) Resolver` — resolves `acme` from `acme.<base>`. Host is normalized (port stripped, IPv6 brackets, trailing FQDN dot, lowercased — hostrouter's zero-alloc approach). Resolves only when exactly one label precedes the base (nested `a.b.<base>` and the bare base do not resolve). Reserved names (`www`, `api`, …) are NOT special-cased — exclude them in your handler or by wrapping the resolver.
- `Domain(lookup DomainLookup) Resolver` — custom domains. Normalized full host is looked up; `ErrDomainNotFound` continues the chain, any other error stops it.
- `Header(name string) Resolver` — trusts a request header (`X-Tenant-ID`). For internal traffic behind a trusted gateway only; documented loudly.
- `Cookie(name string) Resolver` — cookie value.
- `PathPrefix(prefix string) Resolver` — tenant is the path segment after `prefix` (`PathPrefix("/t")`: `/t/acme/dash` → `acme`); `PathPrefix("")`: first segment. Never rewrites the path.
- `Context() Resolver` — reads a tenant already stamped on the request context by an upstream middleware (e.g. API-key auth that called `NewContext`). Gives ctx-derived tenancy an explicit slot in the precedence order.

### DomainLookup seam (resolver.go)

```go
type DomainLookup interface {
    TenantByDomain(ctx context.Context, domain string) (string, error)
}
```

Return `ErrDomainNotFound` (sentinel owned here) when the domain is unknown. `StaticDomains(map[string]string) DomainLookup` ships as the in-memory implementation for tests/dev; keys are normalized at construction.

### Middleware (middleware.go)

```go
func Middleware(resolvers ...Resolver) func(http.Handler) http.Handler
func Require(next http.Handler) http.Handler
```

- `Middleware`: precedence-ordered — first resolver returning a non-empty ID wins and is stamped via `NewContext` (overriding any pre-existing ctx tenant; use `Context()` in the chain to give the pre-existing value a precedence slot). A resolver error responds `500 Internal Server Error` (generic body, no leak) and does not call next. No resolution → the request passes through unchanged: a tenant stamped upstream is preserved, a request with none stays untenanted (single-tenant routes coexist).
- `Require`: guard middleware responding `404 Not Found` when the context carries no tenant. 404, not 401/403 — an unresolved tenant host is "nothing here", and 404 leaks nothing about tenancy. Both satisfy `web/middleware.Middleware` structurally (no import).

### ScopeClause (clause.go)

```go
type Clause struct {
    SQL string // "tenant_id = $3" / "tenant_id = ?"
    Arg string // tenant ID from ctx
}

func ScopeClause(ctx context.Context, column, placeholder string) (Clause, error)
```

- `placeholder` is literally what appears in the SQL: `"$3"` (pgx/postgres) or `"?"` (sqlite/mysql/clickhouse). What you pass is what you read in the query — no dialect enum, no hidden numbering.
- Fail-closed: no tenant in ctx → `ErrNoTenant`.
- `column` must match `[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?` (optionally qualified identifier) → else `ErrInvalidColumn`. `placeholder` must be `?` or `$<digits>` → else `ErrInvalidPlaceholder`. Both guard against accidental injection through misuse; they are programmer-error checks, validated without regexp on the hot path.

Usage:

```go
c, err := tenant.ScopeClause(ctx, "tenant_id", "$2")
rows, err := db.Query(ctx, "SELECT id FROM orders WHERE status = $1 AND "+c.SQL, status, c.Arg)
```

### Errors (errors.go)

`ErrNoTenant`, `ErrDomainNotFound`, `ErrInvalidColumn`, `ErrInvalidPlaceholder` — single-line, `errors.Is`-matchable.

## Layout

`doc.go` (runnable example) · `tenant.go` · `resolver.go` · `middleware.go` · `clause.go` · `errors.go` · black-box tests (`package tenant_test`) · `fuzz_test.go` (Subdomain/PathPrefix; host normalization is exercised through the Subdomain target) · `bench_test.go`.

## Performance

- Host normalization allocates only on uppercase input (sub-slicing otherwise) — hostrouter precedent.
- Resolver hot paths (Subdomain, Header, PathPrefix) target zero allocations; middleware allocates only the one unavoidable `context.WithValue` per resolved request.
- Identifier/placeholder validation is byte-loop, no regexp.
- `bench_test.go` covers each resolver, the middleware end-to-end, and ScopeClause; post-benchmark optimization pass with before/after in the PR.

## Testing

Unit-only (no DB): carrier round-trip and fail-closed Scope; every resolver's match/no-match/error cases incl. host edge cases (port, IPv6, FQDN dot, case, nested labels, bare base); middleware precedence, override-of-preexisting-ctx, 500-on-error, pass-through; Require; StaticDomains normalization; ScopeClause valid/missing-tenant/bad-column/bad-placeholder; fuzz the parsers.

## Catalog

The PR deletes the `data/tenant` entry from `docs/packages.md` (unbuilt-only rule). Cross-references to `tenant` in other entries stay.
