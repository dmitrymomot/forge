# auth/rbac — Design

Role-based access control: predefined roles with permission grants, role inheritance (nesting + multi-parent), out-of-hierarchy standalone roles, wildcard grants. Resolves a subject's role names into an effective permission set and answers "does this subject hold this permission?". Ships an `access.Decider` adapter (the 403 half of the guard split) and a storage-agnostic assignment `Store` for runtime subject→role management.

## Goals & scope

- A **standalone permission engine** (Layer 1) usable with **zero `access` types in its API** — from a CLI, a jobqueue handler, a gRPC interceptor, anywhere. Pure role/permission model over plain strings.
- An **`access.Decider` adapter** (Layer 2) that drops into a `FirstDecisive`/`DenyOverrides` chain, emitting `Allow`/`Abstain`.
- A **hybrid** role source: a zero-I/O fast path (roles carried on the `Subject`) and a Store-backed path (roles assigned as pure runtime data).
- Single-tenant and multi-tenant from one package: tenancy enters via the mandated construction-time `WithScope` hook, fails closed, and single-tenant use pays zero ceremony.

**Deps:** `auth/access` (Layer 2 Decider adapter only), `web/middleware` + `web/problem` (the `Require` gate — already transitive via `access`), `core/ctxkey` (the `Checker` context carrier), `resilience/cache` (documented read-through recipe, not a hard dep), `data/postgres`/`data/migration` (the `rbac/pgstore` driver leaf). Layer 1's API surface imports nothing forge-internal.

**Anti-scope:** per-subject/per-resource grants and deny-overrides are `acl`'s job; relationship/attribute predicates are `abac`'s job; `rbac` only *grants*, never denies, and never inspects `Resource`. Role definitions changing while serving (mutable registry) is out — definitions are predefined; the runtime-changing part is *assignments*, which live in the Store. External policy engines (casbin/OPA) plug in behind the `access` seam, not here.

## Layer 1 — the permission engine (access-free)

### Declaring the RoleSet

Definitions are declared with functional options and validated once at construction. Permissions and inheritance are two separate, self-documenting sections:

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
- `RoleInherits(child string, parents ...string) InheritEdge` — one inheritance edge; multi-parent.
- `WithRoles(...RoleDef)` and `WithRoleInheritance(...InheritEdge)` are `RoleSetOption`s; both accumulate across repeated calls.
- A role may be pure-grants, pure-aggregator (only inherits), both, or neither. "Standalone / out-of-hierarchy" is simply a role with no inbound/outbound edges — no separate concept.

`NewRoleSet` resolves the whole inheritance graph once and returns an error on:

- **`ErrDuplicateRole`** — the same role name declared twice.
- **`ErrUnknownRole`** — an inheritance edge naming a role not defined via `WithRoles` (child or parent).
- **`ErrCycle`** — an inheritance cycle.

Diamonds (two parents sharing a grandparent) dedupe — `rbac` only grants, so the effective set is always a union; there is no conflict to resolve. After construction the `RoleSet` is **immutable and concurrent-safe**, with each role's effective permission set **pre-expanded** so lookups are map hits, not graph walks.

### Permission matching (wildcards)

Grants are `access.Action`-shaped strings, flat verb-on-noun with a single `:` (`documents:read`). Three match forms (Q2-A):

- **Exact** — `documents:read` grants `documents:read`.
- **Segment wildcard** — `documents:*` grants any single segment after the noun (`documents:read`, `documents:write`).
- **Super-wildcard** — `*` grants everything (the admin role).

Multi-level globs, prefix wildcards, and `**` are explicitly out — `access.Action` is documented flat verb:noun and there is no producer for deeper paths.

Internal representation per role (the pre-expanded effective set):

```go
type permSet struct {
    exact    map[string]struct{} // "documents:read"
    wildcard map[string]struct{} // noun before ":*", stored bare: "documents"
    super    bool                // "*" present
}

func (p *permSet) Allows(action string) bool {
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
    _, ok := p.wildcard[action[:i]] // action[:i] is a substring — no allocation
    return ok
}
```

`Allows` is O(1) and **zero-alloc** (`IndexByte` + substring slice, no `[]byte`/concat round-trips). This matches the single-colon flat action model; documented as such.

### Resolution API

- `func (rs *RoleSet) Can(roleNames []string, action string) bool` — the hot-path answer. Iterates the role names, short-circuits on the first role whose pre-expanded `permSet.Allows(action)` is true. **Zero-alloc** — no union map is built; it reads the stored per-role sets directly.
- `func (rs *RoleSet) Resolve(roleNames ...string) PermissionSet` — the "give me the whole effective set" view for listing/debugging/admin UIs. *May* allocate (not the hot path).
- `type PermissionSet` with `Allows(action string) bool` and `List() []string`.
- `func (rs *RoleSet) HasRole(roleNames []string, required string) bool` — **inheritance-aware role membership**: reports whether `required` is in the inheritance closure of `roleNames` (holding `editor`, which inherits `viewer`, satisfies `HasRole(…, "viewer")`). Exact-name match when no edges are defined. Access-free; backs `WithAnyRole` in the `Require` gate and any non-HTTP role check. Zero-alloc (reads the pre-expanded ancestor closure, no set built).

**Unknown role names** (a name absent from the `RoleSet`) contribute nothing and are skipped silently in resolution — roles get renamed and retired, and authz must not wedge. Optionally surfaced in the Decision trace at Layer 2 under `WithExplain`.

## Layer 2 — the access.Decider adapter (imports access)

```go
d := rbac.Decider(rs, rbac.FromSubject())        // zero-I/O fast path
d := rbac.Decider(rs, rbac.FromStore(store))     // runtime-assigned roles
```

- `func Decider(rs *RoleSet, src RoleSource, opts ...DeciderOption) access.Decider`.
- `type RoleSource interface { Roles(ctx context.Context, s access.Subject) ([]string, error) }`.
- `func FromSubject() RoleSource` — returns `s.Roles` (see the `access` change below). Zero-alloc, zero-I/O.
- `func FromStore(store Store) RoleSource` — returns `store.RolesFor(ctx, s.Tenant, s.ID)`; tenant is the already-resolved authoritative `Subject.Tenant`. Wrap with a `cache.Store` read-through when hot (documented recipe, not built in — keeps the dep off unless needed).

**Effect (Q3):** permission present → `Allow`; absent → `Abstain` ("no opinion — let a lower layer decide"). Never `Deny`: a missing grant must not veto what `acl`/`abac` might otherwise grant. `access.Authorize` turns an all-`Abstain` chain into the final fail-closed `Deny`. This mirrors `ScopeDecider` exactly.

**Resource-agnostic:** the Decider inspects only the `Action`. Resource-scoped grants are `acl`'s job.

A `RoleSource` error is returned from `Decide` (non-nil error) so the chain fails closed via `access.Authorize` — the same contract every `Decider` follows. `Decision.Decider` is `"rbac"`; reasons are constant strings so the `Allow`/`Abstain` path stays zero-alloc.

Wiring (behind a guard authn middleware):

```go
decider := access.FirstDecisive(
    access.TenantMatch(),
    rbac.Decider(rs, rbac.FromSubject()),
    // acl.Decider(...), abac.Decider(...) layer in here
)
read := access.RequirePermission(decider, "documents:read")
mux.Handle("GET /docs/{id}", authn(read(getDoc)))
```

## Layer 2c — the lightweight full-stack path (Require gate + templ Checker)

The "I have a static `RoleSet` and a `role` field on the user/member row — just gate the route and toggle UI" path for server-rendered (templ) apps. No `access.Subject`, no `Decider` chain: a caller-supplied extractor pulls the subject's role names straight from the request.

```go
type Extractor func(r *http.Request) ([]string, error)
```

The `Extractor` is the whole integration seam — it reads the app's own `role` column (typically already loaded into context by the auth middleware). Single-role tables return a one-element slice.

```go
extractRoles := func(r *http.Request) ([]string, error) {
    m, ok := member.From(r.Context()) // consumer's loaded row
    if !ok {
        return nil, member.ErrNoMember
    }
    return []string{m.Role}, nil
}
```

### The Require gate

One generic middleware gates on roles, permissions, or both:

```go
func Require(rs *RoleSet, extract Extractor, opts ...GateOption) middleware.Middleware

func WithAnyRole(roles ...string)       GateOption // any-of: holds ≥1 (inheritance-aware, via HasRole)
func WithPermissions(actions ...string) GateOption // all-of: holds every listed permission (via Can)
func WithForbidden(fn func(w http.ResponseWriter, r *http.Request)) GateOption
```

- **All provided constraints must pass (AND).** A gate usually carries one, but both compose: `WithAnyRole("admin") + WithPermissions("billing:read")` → admin *and* holds `billing:read`.
- `WithAnyRole` is **any-of** (inheritance-aware — an `editor` passes a `WithAnyRole("viewer")` gate). `WithPermissions` is **all-of** (every listed capability). Any-of permissions is a future `WithAnyPermission` if ever needed (YAGNI now).
- Roles/permissions ride options (not variadic params) because Go forbids two variadic params alongside `opts`, and it lets callers pass a computed slice via spread: `WithAnyRole(allowed...)`.
- A `Require` with **neither** a role nor a permission option is a programming error → **panic at construction** (boot-time, mirroring `access.Model`'s panic-on-misuse), so an "option" gate can't silently gate nothing.
- **Fail closed.** A constraint miss *or* an extractor error rejects. Default is a `problem` 403 (works for JSON APIs too); `WithForbidden` overrides it to render an **HTML 403 page or a redirect** — the gate imports neither `templ` nor `render`, it just calls the supplied function.

```go
// full-stack HTML routes
admin := rbac.Require(rs, extractRoles,
    rbac.WithAnyRole("admin"),
    rbac.WithForbidden(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusForbidden)
        _ = views.Forbidden().Render(r.Context(), w) // your templ 403 component
    }),
)
mux.Handle("GET /admin", authn(admin(adminPage)))

// or gate on capability, or redirect instead of a page
rbac.Require(rs, extractRoles, rbac.WithPermissions("documents:delete"),
    rbac.WithForbidden(func(w http.ResponseWriter, r *http.Request) {
        http.Redirect(w, r, "/", http.StatusSeeOther)
    }))
```

Unauthenticated users are `guard`'s upstream 401/login-redirect job; by the time `Require` runs the user is authenticated and this is a genuine "you don't have access" 403.

### In-template checks (Checker + context carrier)

templ components receive a `context.Context`, so conditional rendering (`show the Delete button only if allowed`) reads a context-carried checker — no `RoleSet`/roles threaded through every component:

```go
// tiny value binding *RoleSet + the subject's role names (access-free, Layer 1)
type Checker struct{ /* rs + roles */ }
func (rs *RoleSet) CheckerFor(roleNames ...string) Checker
func (c Checker) Can(action string) bool
func (c Checker) HasRole(required string) bool

// context carrier (ctxkey seam) + middleware that reuses the SAME Extractor as Require
func WithChecker(rs *RoleSet, extract Extractor) middleware.Middleware
func FromContext(ctx context.Context) (Checker, bool)
```

```templ
templ DocActions(id string) {
	if chk, ok := rbac.FromContext(ctx); ok && chk.Can("documents:delete") {
		<button hx-delete={ "/docs/" + id }>Delete</button>
	}
}
```

`WithChecker` stashes the `Checker` once per request; every downstream component (and `web/render`/`web/htmx` view) reads it with zero param-threading. `WithChecker` and `Require` share one `Extractor`, so there is a single integration point for the app's `role` column. Route gating (`Require`) and view toggling (`Checker`) split cleanly.

**The overall split:** full-stack HTML routes use `Require` (roles or permissions, HTML 403 via `WithForbidden`) and `WithChecker` (view toggles); the `access.Decider` chain stays the JSON-API / multi-layer (`acl`/`abac`) path.

## The assignment Store (runtime subject→role data)

Read by `FromStore`; written by admin surfaces. Tenant is explicit at the Store boundary (`access`'s explicit-scope philosophy):

```go
type Store interface {
    RolesFor(ctx context.Context, tenant, subject string) ([]string, error)
    Assign(ctx context.Context, tenant, subject string, roles []string) error
    Unassign(ctx context.Context, tenant, subject string, roles []string) error
}
```

- **Tenancy** rides the mandated `WithScope` hook, applied by a thin management wrapper: `rbac.NewManager(store, rbac.WithScope(fn))` derives the tenant from context, **fails closed with `ErrScope`** when a configured scope is missing, and passes the resolved tenant to the Store. Single-tenant apps skip `WithScope` and the tenant is `""` — zero ceremony. Matches `apikey`/`otp`/`lockout`.
- The `Manager` exposes `Assign(ctx, subject, roles...)`, `Unassign(ctx, subject, roles...)`, `RolesFor(ctx, subject)` — the context-tenant-scoped surface for admin panels and CLIs.
- `FromStore` reads the raw `Store` with the authoritative `Subject.Tenant`; `TenantMatch` in the chain still vetoes cross-tenant. (Write-side derives tenant from ambient context via `WithScope`; read-side uses the request's resolved `Subject.Tenant` — both resolve to the same tenant within a request.)
- **`Assign`/`Unassign` do not validate against the `RoleSet`** — the Store is decoupled from definitions, so a role can be assigned before it is defined, or staged through a rename. Validation is the definition layer's job at `NewRoleSet`.
- **Drivers:** `NewMemoryStore()` built in (tests/dev); `rbac/pgstore` is the real backend (embedded migration via `data/migration`). Schema: `rbac_assignments(tenant text not null default '', subject text not null, role text not null, primary key (tenant, subject, role))`; `Assign` = `INSERT … ON CONFLICT DO NOTHING` (idempotent), `Unassign` = `DELETE`, `RolesFor` = `SELECT role WHERE tenant=$1 AND subject=$2`.

No `List subjects with role` reverse query in v1 (YAGNI — not needed for an authz decision).

## Change to auth/access

Add one optional field to `access.Subject`, plus a one-line doc note:

```go
type Subject struct {
    Attrs  map[string]any // abac reads these; nil-safe
    ID     string
    Tenant string
    Scopes []string       // token scopes; ScopeDecider reads these
    Roles  []string       // role names; rbac.FromSubject reads these
}
```

Rationale: `Subject` is already the shared carrier of every input the built-in deciders read — `Scopes` exists for `ScopeDecider`, `Attrs` for `abac`. A `Roles` field for `rbac` is the same established pattern, and it beats `Attrs["roles"]`: zero-alloc, zero type-assertion, no nil-map guard on the hot path; symmetric with `Scopes`; discoverable; betteralign orders it. `SubjectFromIdentity` stays zero-alloc (leaves `Roles` nil, like `Attrs`); the consumer's `WithSubject` resolver fills it from token claims at login — exactly how `Scopes` arrives.

## Package anatomy

Per design.md: `doc.go` (runnable example) · `options.go` (three option families — `RoleSetOption` for `WithRoles`/`WithRoleInheritance`; `Option` for `WithScope`; `GateOption` for `WithAnyRole`/`WithPermissions`/`WithForbidden` — all `type … func(*config)`, never builders) · `errors.go` (`errors.Is`-matchable single-line sentinels: `ErrDuplicateRole`, `ErrUnknownRole`, `ErrCycle`, `ErrScope`) · impl. `Decider` takes no options in v1 (explanation is driven by `access.WithExplain`/`ExplainContext`, not an rbac option).

Proposed files:

- `doc.go` — package doc + runnable example (engine standalone + Decider wiring + templ `Require`/`Checker`).
- `roleset.go` — `Role`, `RoleInherits`, `WithRoles`, `WithRoleInheritance`, `NewRoleSet`, `RoleSet`, `PermissionSet`, `permSet`, `Can`, `HasRole`, graph resolution, wildcard matching.
- `options.go` — the three option families + `WithScope`.
- `decider.go` — `Decider`, `RoleSource`, `FromSubject`, `FromStore` (imports `access`).
- `middleware.go` — `Extractor`, `Require`, `WithAnyRole`, `WithPermissions`, `WithForbidden` (imports `web/middleware`, `web/problem`).
- `checker.go` — `Checker`, `CheckerFor`, `WithChecker`, `FromContext` (imports `core/ctxkey`, `web/middleware`).
- `store.go` — `Store`, `Manager`, `NewManager`, `Assign`/`Unassign`/`RolesFor`.
- `memory.go` — `NewMemoryStore`.
- `errors.go` — sentinels.
- `bench_test.go` — required.
- `pgstore/` — `pgstore.go` + embedded migration.

Single Go module; two levels max (`rbac/pgstore` is the sole third-level driver leaf). Black-box tests only (`rbac_test` package); white-box only if unexported graph state needs direct assertion.

## Testing & benchmarks

- **Black-box tests:** graph validation (dup/unknown/cycle errors), inheritance expansion (single, multi-parent, diamond dedupe, deep chains), wildcard matching (exact/segment/super, non-match, colon-less action), unknown-role skip, standalone roles, `HasRole` inheritance closure (held ancestor satisfies gate; sibling/descendant does not).
- **Middleware tests:** `Require` — `WithAnyRole` any-of pass/miss, inheritance-aware pass, `WithPermissions` all-of pass/miss (one missing → deny), role+permission AND composition, extractor error → fail-closed, default problem+json 403, `WithForbidden` renders custom body/redirect, panic on empty gate (no role/permission option). `Checker`/`WithChecker`/`FromContext` — `Can`/`HasRole` true/false, context round-trip, missing checker → `ok == false`.
- **Decider tests:** `FromSubject` Allow/Abstain, `FromStore` Allow/Abstain, `RoleSource` error → fail-closed `Deny` via `Authorize`, chain composition with `TenantMatch`/`acl`-style layers, `WithExplain` trace.
- **Store/Manager tests:** idempotent `Assign`, `Unassign`, `RolesFor`; `WithScope` fail-closed (`ErrScope`); single-tenant zero-scope path; per-tenant isolation. `pgstore` covered against live Postgres (dbtest-style).
- **Benchmarks (required):** `BenchmarkCan` (wildcard match), `BenchmarkDecider_FromSubject` (**target: 0 allocs/op**), `BenchmarkHasRole` (**target: 0 allocs/op**), `BenchmarkResolve`, `BenchmarkNewRoleSet`. Post-benchmark optimization pass with before/after numbers in the PR.

## Open questions

None outstanding — all design forks resolved during brainstorming.
