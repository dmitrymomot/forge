# auth/rbac — Design

Role-based access control: predefined roles with permission grants, inheritance (nesting + multi-parent), out-of-hierarchy standalone roles, wildcard grants. Resolves a subject's role names into an effective permission set and answers "does this subject hold this permission?". It plugs into the shipped `auth/access` decision seam as one `Decider`, and — with two small, symmetric additions to `guard`/`access` — the end-to-end wiring for both JSON APIs and full-stack (templ) apps collapses to a handful of lines.

## Design driver: DX first

An earlier draft grew a parallel middleware layer inside `rbac` (its own `Require`/`Extractor`/`Checker`) to cover full-stack apps. That duplicated `access` and read as over-engineered. The friction traced to two gaps in the existing packages, not to anything missing in `rbac`:

1. **`guard.Identity` carries `Scopes` but not `Roles`**, so roles couldn't flow `Identity → Subject → Decider` automatically — every consumer had to hand-write a `WithSubject` resolver just to attach roles.
2. **`access` could only deny via `problem+json`**, so an HTML 403 / redirect forced a second gating mechanism.

Closing both gaps in `guard`/`access` lets `rbac` stay a lean brick (engine + `Decider` + `Store`) while one gating mechanism (`access.RequirePermission`) serves APIs and full-stack alike, and view checks run the *same* decider chain as route gates. That is the whole point of this design.

## Goals & scope

- A **standalone permission engine** (Layer 1) with **zero forge deps in its API** — usable from a CLI, a job handler, anywhere.
- A thin **`access.Decider` adapter** (Layer 2) — roles → permissions, `Allow`/`Abstain` — that drops into a `FirstDecisive`/`DenyOverrides` chain beside `acl`/`abac`.
- A storage-agnostic **assignment `Store`** for runtime subject→role data, tenancy via the mandated `WithScope` hook, fail-closed, `pgstore` driver.
- Two **enabling changes to `guard`/`access`** so the wiring is trivial and there is one gate for API + full-stack.

**Deps (rbac):** `auth/access` (Layer 2 Decider only), `data/postgres`/`data/migration` (the `rbac/pgstore` leaf). Layer 1 imports nothing forge-internal. `resilience/cache` is a documented read-through recipe for `FromStore`, not a hard dep.

**Anti-scope:** per-subject/per-resource grants + deny-overrides are `acl`; relationship/attribute predicates are `abac`; `rbac` only *grants*, never denies, never inspects `Resource`. Mutable role *definitions* at serve-time are out (definitions are predefined; the runtime-changing part is *assignments*, in the Store). External policy engines (casbin/OPA) plug in behind the `access` seam, not here. **No `rbac`-owned HTTP middleware** — gating and view checks live in `access` (see below).

## Layer 1 — the permission engine (access-free)

### Declaring the RoleSet

Functional options, validated once, immutable. Permissions and inheritance are two separate sections:

```go
rs, err := rbac.NewRoleSet(
    rbac.WithRoles(
        rbac.Role("viewer",  "documents:read", "comments:read"),
        rbac.Role("editor",  "documents:*"),
        rbac.Role("admin",   "*"),
        rbac.Role("auditor", "auditlog:read"),
    ),
    rbac.WithRoleInheritance(
        rbac.RoleInherits("editor",  "viewer"),
        rbac.RoleInherits("manager", "editor", "auditor"), // multi-parent aggregator
    ),
)
```

- `Role(name string, grants ...string) RoleDef` — a role and its *own* permission patterns.
- `RoleInherits(child string, parents ...string) InheritEdge` — one edge; multi-parent.
- `WithRoles(...RoleDef)` and `WithRoleInheritance(...InheritEdge)` are `RoleSetOption`s; both accumulate across repeated calls.
- A role may be pure-grants, pure-aggregator, both, or neither. "Standalone / out-of-hierarchy" is simply a role with no edges — no separate concept.

`NewRoleSet` resolves the graph once and errors on **`ErrDuplicateRole`** (name declared twice), **`ErrUnknownRole`** (an edge names a role not defined via `WithRoles`), **`ErrCycle`** (inheritance cycle). Diamonds dedupe (union — `rbac` only grants). After construction the `RoleSet` is **immutable and concurrent-safe**, with each role's effective permission set and ancestor closure **pre-expanded** so lookups are map hits, not graph walks.

### Permission matching (wildcards)

Grants are flat verb-on-noun strings with a single `:` (`documents:read`). Three forms:

- **Exact** — `documents:read` grants `documents:read`.
- **Segment wildcard** — `documents:*` grants any single segment after the noun.
- **Super-wildcard** — `*` grants everything (the admin role).

Multi-level globs / prefix wildcards / `**` are out — `access.Action` is documented flat verb:noun.

Internal per-role effective set:

```go
type permSet struct {
    exact    map[string]struct{} // "documents:read"
    wildcard map[string]struct{} // noun before ":*", stored bare: "documents"
    super    bool                // "*" present
}

func (p *permSet) allows(action string) bool {
    if p.super {
        return true
    }
    if _, ok := p.exact[action]; ok {
        return true
    }
    i := strings.IndexByte(action, ':')
    if i < 0 {
        return false
    }
    _, ok := p.wildcard[action[:i]] // substring — no allocation
    return ok
}
```

O(1), **zero-alloc** (`IndexByte` + substring, no `[]byte`/concat round-trips). Assumes single-colon actions; documented.

### Resolution API

- `func (rs *RoleSet) Can(roleNames []string, action string) bool` — the hot-path answer. Iterates the role names, short-circuits on the first whose pre-expanded set allows `action`. **Zero-alloc** — reads stored per-role sets, builds no union.
- `func (rs *RoleSet) HasRole(roleNames []string, required string) bool` — **inheritance-aware role membership**: is `required` in the ancestor closure of `roleNames`? Holding `editor` (inherits `viewer`) satisfies `HasRole(…, "viewer")`; exact-name when no edges. **Zero-alloc** (reads pre-expanded closure). Backs cosmetic role checks; not a route gate.
- `func (rs *RoleSet) Resolve(roleNames ...string) PermissionSet` — the whole effective set for listing/debug/admin UIs. *May* allocate (not hot-path). `PermissionSet` has `Allows(action) bool` and `List() []string`.

**Unknown role names** contribute nothing and are skipped silently — roles get renamed/retired and authz must not wedge. Optionally surfaced in the Decision trace under `access.WithExplain`.

## Layer 2 — the access.Decider adapter (imports access)

```go
d := rbac.Decider(rs, rbac.FromSubject())    // zero-I/O; roles flow from guard.Identity
d := rbac.Decider(rs, rbac.FromStore(store)) // runtime-assigned roles
```

- `func Decider(rs *RoleSet, src RoleSource) access.Decider`.
- `type RoleSource interface { Roles(ctx context.Context, s access.Subject) ([]string, error) }`.
- `func FromSubject() RoleSource` — returns `s.Roles`. Zero-alloc, zero-I/O.
- `func FromStore(store Store) RoleSource` — returns `store.RolesFor(ctx, s.Tenant, s.ID)` using the authoritative resolved `Subject.Tenant`; wrap with `cache.Store` when hot (documented recipe, not built in).

**Effect:** permission present → `Allow`; absent → `Abstain`. Never `Deny` — a missing grant must not veto what `acl`/`abac` might grant; `access.Authorize` closes an all-`Abstain` chain to a fail-closed `Deny`. Mirrors `ScopeDecider`. **Resource-agnostic** — inspects only the `Action`. A `RoleSource` error is returned from `Decide` so the chain fails closed via `Authorize`. `Decision.Decider = "rbac"`; reasons are constants (zero-alloc path).

## The assignment Store (runtime subject→role data)

Read by `FromStore`; written by admin surfaces. Tenant explicit at the boundary:

```go
type Store interface {
    RolesFor(ctx context.Context, tenant, subject string) ([]string, error)
    Assign(ctx context.Context, tenant, subject string, roles []string) error
    Unassign(ctx context.Context, tenant, subject string, roles []string) error
}
```

- **Tenancy** rides the mandated `WithScope` hook via a thin wrapper: `rbac.NewManager(store, rbac.WithScope(fn))` derives tenant from context, **fails closed with `ErrScope`** when a configured scope is missing, passes the resolved tenant to the Store. Single-tenant skips `WithScope`, tenant is `""` — zero ceremony. Matches `apikey`/`otp`/`lockout`.
- `Manager` exposes `Assign(ctx, subject, roles...)`, `Unassign(ctx, subject, roles...)`, `RolesFor(ctx, subject)` — the context-scoped admin surface.
- **`Assign`/`Unassign` do not validate against the `RoleSet`** — Store is decoupled from definitions (assign-before-define, staged renames). Validation is `NewRoleSet`'s job.
- **Drivers:** `NewMemoryStore()` built in; `rbac/pgstore` real backend (embedded migration). Schema `rbac_assignments(tenant text not null default '', subject text not null, role text not null, primary key (tenant, subject, role))`; `Assign` = `INSERT … ON CONFLICT DO NOTHING`, `Unassign` = `DELETE`, `RolesFor` = `SELECT role WHERE tenant=$1 AND subject=$2`.

No reverse "list subjects with role" query in v1 (YAGNI for an authz decision).

## Change 1 — auth/guard: `Identity.Roles`

Add one optional field to `guard.Identity`, symmetric with `Scopes`:

```go
type Identity struct {
    // …existing: Subject, Tenant, Scopes, Meta…
    Roles []string // role names; carried from the token/session by the Verifier
}
```

The `Verifier` (JWT claim, session, apikey) fills `Roles` exactly as it fills `Scopes`. `guard` gains no dependency on `rbac` — `Roles` is a generic identity attribute (just like `Scopes`, which `guard` carries without knowing about `ScopeDecider`). This is the change that lets roles flow to the decider with no per-consumer plumbing.

## Change 2 — auth/access: Subject.Roles, WithForbidden, view checks

### 2a. `Subject.Roles` + `SubjectFromIdentity`

```go
type Subject struct {
    Attrs  map[string]any // abac reads these; nil-safe
    ID     string
    Tenant string
    Scopes []string       // ScopeDecider reads these
    Roles  []string       // rbac.FromSubject reads these
}
```

`SubjectFromIdentity` is updated to copy `id.Roles` into `s.Roles` (alongside `ID`/`Tenant`/`Scopes`), staying zero-alloc (shares the slice header). Because `defaultSubject` already runs `guard.From → SubjectFromIdentity`, **roles now reach `rbac.Decider(rs, FromSubject())` with no `WithSubject` resolver** — the biggest DX win. `Subject` remains the shared carrier of every input the built-in deciders read (`Scopes`→scope, `Attrs`→abac, `Roles`→rbac).

### 2b. `WithForbidden` — HTML / redirect denial

Today `reject` only calls the `problem.Responder`. Add a denial hook that overrides it:

```go
func WithForbidden(fn func(w http.ResponseWriter, r *http.Request)) Option
```

`config` gains `forbidden func(http.ResponseWriter, *http.Request)`; `reject` calls it (after the existing decider-error log) instead of the problem responder when set. The denied `Decision` is already on the request context (`gate` calls `withDecision`), so the handler reads it via `access.DecisionFrom(r.Context())` if it wants the reason. The gate imports neither `templ` nor `render` — it just calls the function:

```go
htmlForbidden := func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusForbidden)
    _ = views.Forbidden().Render(r.Context(), w) // templ 403 page
}
// or: http.Redirect(w, r, "/", http.StatusSeeOther)
```

This makes one gate (`RequirePermission`/`Require`) serve JSON (default `problem+json`) and full-stack (HTML/redirect) alike. Unauthenticated users stay `guard`'s upstream 401/login-redirect concern; by the time a gate runs, this is a genuine 403.

### 2c. `WithChecker` + `Can`/`CanResource` — view checks through the same chain

A view check ("render the Delete button?") must run the **same decider** as the route gate, or a button can show for an action the route denies. So it belongs in `access` (which owns `Subject`/`Decider`), not in `rbac`:

```go
func WithChecker(d Decider, opts ...Option) middleware.Middleware // binds {d, subject} per request
func Can(ctx context.Context, action Action) bool
func CanResource(ctx context.Context, action Action, r Resource) bool
```

`WithChecker(d)` resolves the `Subject` once per request (the same `cfg.subject` resolver, default `guard`) and stashes a small `{decider, subject, ok}` value under a `ctxkey`. `Can(ctx, action)` reads it, runs `Authorize(ctx, d, subject, action, Resource{})`, returns `Effect == Allow` (`false` when unbound or no subject). `CanResource` passes a resource for `acl`/`abac` view checks. Since `WithChecker(d)` and `RequirePermission(d, …)` share the one app-global decider, routes and views have a single source of truth. templ:

```templ
templ DocActions(id string) {
	if access.Can(ctx, "documents:delete") {
		<button hx-delete={ "/docs/" + id }>Delete</button>
	}
}
```

**Role-name route-gating is intentionally not a middleware.** In RBAC, "admin-only page" is cleanest as "gate on a permission only the admin role grants" (e.g. `admin:panel`). For the rare cosmetic role check, read `guard.From(ctx).Roles` and call `rs.HasRole(...)`.

## End-to-end usage (the resulting DX)

```go
// once, at boot
rs := buildRoles()                          // rbac.NewRoleSet(...); panic on err
d  := rbac.Decider(rs, rbac.FromSubject())  // roles arrive via guard.Identity.Roles

// full-stack HTML app
htmlForbidden := func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusForbidden); _ = views.Forbidden().Render(r.Context(), w)
}
mux.Handle("GET  /docs/{id}", authn(access.WithChecker(d)(
    access.RequirePermission(d, "documents:read",  access.WithForbidden(htmlForbidden))(getDoc))))
mux.Handle("POST /docs/{id}", authn(access.WithChecker(d)(
    access.RequirePermission(d, "documents:write", access.WithForbidden(htmlForbidden))(putDoc))))

// JSON API: same gate, default problem+json denial
mux.Handle("GET /api/docs/{id}", authn(access.RequirePermission(d, "documents:read")(getDocJSON)))
```

No `WithSubject`, no `WithResource` (rbac is resource-agnostic), no `FirstDecisive` wrapper for the single-layer case, no `rbac`-specific middleware. Adding `acl`/`abac` later is `d := access.FirstDecisive(access.TenantMatch(), rbac.Decider(...), acl.Decider(...), abac.Decider(...))` — the routes and views are untouched. The Verifier populating `Identity.Roles` is the only integration point, and it mirrors how it already sets `Scopes`.

## Package anatomy

**rbac** — per design.md: `doc.go` (runnable example) · `options.go` (`RoleSetOption` for `WithRoles`/`WithRoleInheritance`; `Option` for `WithScope` — `type … func(*config)`, never builders) · `errors.go` (`ErrDuplicateRole`, `ErrUnknownRole`, `ErrCycle`, `ErrScope`) · impl. Files:

- `doc.go` — package doc + runnable example (engine + Decider + the end-to-end wiring).
- `roleset.go` — `Role`, `RoleInherits`, `WithRoles`, `WithRoleInheritance`, `NewRoleSet`, `RoleSet`, `PermissionSet`, `permSet`, `Can`, `HasRole`, `Resolve`, matching.
- `options.go` — `RoleSetOption` + `Option`/`WithScope`.
- `decider.go` — `Decider`, `RoleSource`, `FromSubject`, `FromStore` (imports `access`).
- `store.go` — `Store`, `Manager`, `NewManager`, `Assign`/`Unassign`/`RolesFor`.
- `memory.go` — `NewMemoryStore`.
- `errors.go` — sentinels.
- `bench_test.go` — required.
- `pgstore/` — `pgstore.go` + embedded migration (sole third-level driver leaf).

**guard** (change) — add `Roles []string` to `Identity` (+ doc note).

**access** (changes) — `access.go`: `Subject.Roles`, `SubjectFromIdentity` copies `id.Roles`. `options.go`: `WithForbidden` + `config.forbidden` + `reject` branch. `checker.go` (new): `checker` ctxkey, `WithChecker`, `Can`, `CanResource`.

Single Go module; two levels max. Black-box tests (`_test` package); white-box only for unexported graph state.

## Testing & benchmarks

- **rbac engine:** graph validation (dup/unknown/cycle), inheritance expansion (single, multi-parent, diamond dedupe, deep chains), wildcard matching (exact/segment/super, non-match, colon-less action), unknown-role skip, standalone roles, `HasRole` closure (held ancestor satisfies; sibling/descendant does not).
- **rbac Decider:** `FromSubject` Allow/Abstain, `FromStore` Allow/Abstain, `RoleSource` error → fail-closed `Deny` via `Authorize`, chain composition with `TenantMatch`.
- **rbac Store/Manager:** idempotent `Assign`, `Unassign`, `RolesFor`; `WithScope` fail-closed (`ErrScope`); single-tenant zero-scope path; per-tenant isolation; `pgstore` against live Postgres (dbtest-style).
- **guard:** `Identity.Roles` round-trips through a Verifier; `SubjectFromIdentity` copies `Roles`.
- **access:** `WithForbidden` renders custom body / redirect and suppresses the problem responder; decider-error still logged; `Decision` readable via `DecisionFrom` inside the hook. `WithChecker`/`Can`/`CanResource` — Allow→true / Abstain→false, unbound context → false, no-subject → false, resource variant runs an `acl`-style layer; view result matches the route gate for the same `(subject, action)`.
- **Benchmarks (required, rbac):** `BenchmarkCan` (wildcard), `BenchmarkHasRole` (**0 allocs/op**), `BenchmarkDecider_FromSubject` (**0 allocs/op**), `BenchmarkResolve`, `BenchmarkNewRoleSet`. Post-benchmark optimization pass with before/after in the PR.

## Open questions

None outstanding — DX simplification resolved: two enabling changes in `guard`/`access`, a lean `rbac`, one gate for API + full-stack, view checks through the same decider.
