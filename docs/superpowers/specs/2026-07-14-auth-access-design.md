# auth/access — Design

> The authorization decision seam. Owns the `Subject`/`Action`/`Resource` question vocabulary and a three-valued `Decider` interface, ships generic combinators and a small set of stdlib-only built-in deciders, and provides the `RequirePermission` middleware (the 403 half of guard's 401-vs-403 split) plus a typed `Model[T]` handler for load-then-authorize flows. `rbac`/`acl`/`abac` will implement `Decider` later; `access` never imports them.

## Purpose & role

`access` answers one question — *"can this actor do this action on this resource?"* — behind a single interface that every authorization strategy plugs into. It is the framework-wide **authorization decision seam** named in `docs/design.md`: `rbac`, `acl`, and `abac` are future bricks that each implement `Decider`; `guard`/`RequirePermission` consume it. Authentication (who you are → 401) is guard's job; authorization (what you may do → 403) is this package's.

The package is a **brick** with two or more real consumers (rbac, acl, abac, and every consumer's `RequirePermission` mount), and simultaneously a **product**: shipping the `ScopeDecider` built-in makes scope-based authorization usable on day one, before rbac exists.

### Anti-scope (fixed by the catalog)

- **No policy DSL.** Predicates are Go functions (`DeciderFunc`), never a parsed rule language.
- **No policy storage.** `access` owns no `Store`. Each future layer (rbac/acl) keeps its own.
- **No resource fetching.** `access` never queries a database. Resource attributes are caller-supplied. The `Model[T].Load` closure that fetches is *consumer code* invoked by `access`, not a query `access` owns.
- **Not a Zanzibar/OPA clone.** External policy engines (casbin/OPA) plug in behind the same `Decider` interface; they are not built here.

## Dependencies

- **Seam, combinators, built-in deciders, `Authorize`:** stdlib only (`context`, `slices`, `fmt`).
- **Middleware side:** `core/ctxkey` (decision stash), `auth/guard` (identity in), `web/middleware` (`Middleware` type out), `web/problem` (403 body).

No real third-party dependency; nothing to isolate in a driver subpackage.

## Vocabulary

```go
// Subject is the actor a decision is made about. Its own type (not a reuse of
// guard.Identity) so the seam works off the HTTP hot path too — a background
// job authorizing an action has no guard.Identity. Attrs is the abac bag.
type Subject struct {
    ID     string         // principal id
    Tenant string         // optional tenant scope
    Scopes []string       // token scopes; ScopeDecider reads these
    Attrs  map[string]any // abac reads these; nil-safe
}

// Action is a verb-on-noun permission string, e.g. "documents:read".
type Action string

// Resource is the thing acted upon. Attrs are caller-supplied — access never
// fetches them. ID == "" means a type-level / collection check.
type Resource struct {
    Type   string
    ID     string
    Tenant string         // enables the built-in cross-tenant veto (TenantMatch)
    Attrs  map[string]any
}

// SubjectFromIdentity adapts a guard.Identity into a Subject. Deliberately
// zero-alloc: it copies ID, Tenant, and the Scopes slice header (shared,
// read-only) and leaves Attrs nil. guard.Identity.Meta is NOT promoted into
// Attrs — that would allocate a map on every request, and the common
// scope/tenant/role checks never read it. Consumers that need identity
// metadata in abac predicates populate Attrs themselves in a WithSubject
// resolver.
func SubjectFromIdentity(id guard.Identity) Subject
```

`Subject` deliberately carries **no `Roles` field** — role resolution (subject → roles, with nesting and wildcards) is rbac's job via its own Store. Putting roles in the vocabulary would leak rbac's model into the seam.

## Decision seam

Layers need **three** answers, not two: a layer must distinguish "I don't apply" (`Abstain`) from "deny," or the fixed precedence cannot work — an abstaining rbac layer has to fall through to default-deny, not actively deny.

```go
type Effect uint8

const (
    Abstain Effect = iota // 0 — no opinion; the zero value is the safe default
    Allow
    Deny
)

// Because builds a Decision carrying this effect and a human reason. Keeps
// custom DeciderFuncs terse: `return access.Allow.Because("admin role"), nil`.
func (e Effect) Because(reason string) Decision

// Decision is the explanation record — the fxrate/formula "record the
// evaluation" philosophy applied to authorization. Decider names the layer
// that spoke; Reason says why. Trace is populated only under WithExplain.
type Decision struct {
    Effect  Effect
    Decider string     // "acl", "rbac", "scope", "tenant", ... (via Named)
    Reason  string     // "scope documents:read absent", "role admin grants"
    Trace   []Decision // full per-layer trace; nil unless the ctx enables it
}

// Decider answers the authorization question. A non-nil error is fail-closed:
// combinators stop and yield Deny (see below). Implementations must be safe
// for concurrent use.
type Decider interface {
    Decide(ctx context.Context, s Subject, a Action, r Resource) (Decision, error)
}

// DeciderFunc adapts a function to Decider — the package's test fake and the
// shape future rbac/acl/abac deciders and consumer predicates take. Mirrors
// guard.VerifierFunc.
type DeciderFunc func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error)

func (f DeciderFunc) Decide(ctx context.Context, s Subject, a Action, r Resource) (Decision, error)

// Named wraps a Decider so its decisions carry `name` in Decision.Decider when
// the wrapped decider left it empty. Built-in deciders name themselves; Named
// labels ad-hoc closures for the explanation record.
func Named(name string, d Decider) Decider
```

## Combinators

```go
// FirstDecisive returns the first decider's decision that is Allow or Deny;
// Abstain falls through. All-abstain returns Abstain. Passing
// [TenantMatch(), acl, abac, rbac] reproduces the documented precedence
// exactly: a single acl decider returning Deny is "acl deny wins", returning
// Allow is "acl grant", each ahead of abac and rbac.
func FirstDecisive(deciders ...Decider) Decider

// DenyOverrides evaluates deciders in order: any Deny vetoes regardless of
// position; otherwise the first Allow wins; otherwise Abstain. The
// "any layer can hard-block" semantic.
func DenyOverrides(deciders ...Decider) Decider
```

**Fail-closed error handling.** If any child `Decide` returns a non-nil error, the combinator stops immediately and returns `Deny` (`Decider` = the failing child's name or `"access"`, `Reason` = the error text) **and** returns the wrapped error so the caller can log it. This holds for both combinators — an unevaluable layer that might have denied means we must deny.

### Explanation trace (opt-in, context-gated)

The winner's `Decider`+`Reason` is always present and cheap (built-in deciders return constant strings). The **full per-layer trace** is opt-in to keep the hot path free of a per-request slice allocation, and it is gated by context rather than a combinator option (which would spoil the clean variadic signature):

```go
// WithExplain marks ctx so combinators accumulate Decision.Trace. Off by
// default. RequirePermission/Model expose WithExplain() to turn it on for a
// debug request or an "explain" endpoint.
func WithExplain(ctx context.Context) context.Context
```

When enabled, each combinator sets `Trace` on its returned `Decision` to the ordered decisions of every layer it consulted.

## Built-in deciders (stdlib-only)

```go
// ScopeDecider: Allow if the subject's Scopes contain the action string
// verbatim, else Abstain. Exact match only — wildcard/prefix grants are rbac's
// job. The day-one usable-product path: put permissions in token scopes.
func ScopeDecider() Decider // Decider name: "scope"

// TenantMatch: Deny when Subject.Tenant and Resource.Tenant are both set and
// differ; Abstain otherwise. Placed first in a chain, cross-tenant access is
// impossible by construction. Single-tenant apps leave both empty → it always
// abstains → zero ceremony.
func TenantMatch() Decider // Decider name: "tenant"

// Terminals for tests and explicit closing.
func AllowAll() Decider           // always Allow
func DenyAll(reason string) Decider // always Deny
```

## Resolving a decision: `Authorize`

The shared core that turns a raw (possibly-`Abstain`) `Decide` into a decisive, fail-closed `Decision`. The middleware and `Model[T].Handle` both use it; handlers call it directly for fine-grained checks after loading an object.

```go
// Authorize runs d.Decide and closes it: Abstain becomes Deny
// (Decider "access", Reason "no layer granted"); a decider error becomes a
// fail-closed Deny with the error returned separately. The returned Decision
// is always decisive (Allow or Deny). A non-nil error means a decider failed
// (Decision is Deny) — in-handler callers may answer 503 instead of 403.
func Authorize(ctx context.Context, d Decider, s Subject, a Action, r Resource) (Decision, error)
```

## Middleware

```go
// RequirePermission gates a route on a static action (the REST common case:
// one route = one action). Subject comes from guard.From; Resource from the
// WithResource resolver (default: zero Resource, a type-level check).
func RequirePermission(d Decider, action Action, opts ...Option) middleware.Middleware

// Require is the escape hatch for a dynamic action (e.g. method-derived): the
// resolver returns both action and resource.
func Require(d Decider, resolve func(r *http.Request) (Action, Resource), opts ...Option) middleware.Middleware
```

Behavior:

- **Allow** → stash the final `Decision` in context, call `next`.
- **Deny / Abstain / no resolvable Subject / decider error** → stash the `Decision`, respond **403** via `problem` (never 401 — that split is guard's), do not call `next`. Decider errors are additionally logged (fail-closed 403, infra detail never leaks to the client).
- Mounting `access` without `guard` denies everything (no subject → 403) — safe by construction.

`WithResource` resolvers stay I/O-free (they read path/query). Attribute checks that need the loaded object belong in-handler via `Authorize` or the `Model[T]` handler, not in a fetching resolver.

### Options

```go
type Option func(*config)

func WithResource(fn func(r *http.Request) Resource)      // resolve the Resource (RequirePermission)
func WithSubject(fn func(r *http.Request) (Subject, bool)) // override subject source (default guard.From)
func WithResponder(p problem.Responder)                   // customize the 403 body
func WithLoadError(fn func(w http.ResponseWriter, r *http.Request, err error)) // Model load failure (default 404)
func WithExplain()                                        // enable Decision.Trace for this mount
```

### Context stash

```go
// DecisionFrom returns the Decision the middleware/Model recorded, for
// auditlog and downstream handlers. Backed by ctxkey.New[Decision]("access").
func DecisionFrom(ctx context.Context) (Decision, bool)
```

## Typed load-then-authorize handler: `Model[T]`

Optional sugar layered over the seam — the core (`Decider`/combinators/`RequirePermission`/`Authorize`) stands alone without it. It collapses the per-handler "load object → build subject → build resource → authorize → 403/404 branches" boilerplate into a typed closure that only runs on the authorized path, with the loaded object handed in. `access` still owns no storage: `Load` is a consumer closure.

```go
type Model[T any] struct {
    Load     func(r *http.Request) (T, error) // you fetch; access never queries
    Describe func(obj T) Resource             // map loaded object → authz Resource
}

// NewModel builds a Model with type inference (T comes from Load's return
// type). Panics at construction if either func is nil — a wiring bug caught
// at startup, in the spirit of guard.MustFrom. The struct literal remains
// valid for tests that need only one field.
func NewModel[T any](load func(r *http.Request) (T, error), describe func(obj T) Resource) Model[T]

// Handle produces an http.Handler running: resolve Subject (else 403, without
// loading) → Load (error → 404, cloaks existence; override via WithLoadError)
// → Describe → Authorize(action) (deny → 403; decider error → 403 + logged)
// → stash Decision → fn(w, r, obj) with the already-loaded object.
func (m Model[T]) Handle(d Decider, action Action, fn func(w http.ResponseWriter, r *http.Request, obj T), opts ...Option) http.Handler
```

The handler signature is inject-only (`func(w, r, T)`, no error return): `access` stays out of the handler-error-to-HTTP mapping business — the closure calls `problem`/`render` directly. Coarse gating still composes in front (`RequirePermission(ScopeDecider())` rejects on scope before the load); `Model.Handle` does the fine-grained, post-load ownership check.

## Consumer patterns

Shared setup: `authn := guard.New(verifier)` is the 401 layer; `access` mounts behind it.

### 1. Simplest — scope/role gate

Zero custom code when the token carries the permission as a scope:

```go
admin := access.RequirePermission(access.ScopeDecider(), "admin:access")
mux.Handle("/admin/", authn(admin(adminHandler)))
```

A role that differs from a scope is a small named closure — the exact shape `rbac.Decider(store)` drops into later:

```go
adminOnly := access.Named("role", access.DeciderFunc(
    func(ctx context.Context, s access.Subject, a access.Action, r access.Resource) (access.Decision, error) {
        if slices.Contains(s.Scopes, "admin") {
            return access.Allow.Because("admin role"), nil
        }
        return access.Decision{}, nil // abstain → default-deny → 403
    }))
admin := access.RequirePermission(adminOnly, "admin:access")
mux.Handle("/admin/", authn(admin(adminHandler)))
```

### 2. Scope + tenant isolation across routes

```go
decider := access.FirstDecisive(
    access.TenantMatch(),  // cross-tenant → hard Deny, evaluated first
    access.ScopeDecider(), // Allow if token scope == action, else Abstain
)
docRes := access.WithResource(func(r *http.Request) access.Resource {
    return access.Resource{Type: "document", ID: r.PathValue("id"), Tenant: r.PathValue("tenant")}
})
read  := access.RequirePermission(decider, "documents:read", docRes)
write := access.RequirePermission(decider, "documents:write", docRes)
mux.Handle("GET /t/{tenant}/docs/{id}", authn(read(getDoc)))
mux.Handle("PUT /t/{tenant}/docs/{id}", authn(write(putDoc)))
```

### 3. Resource ownership (abac-style) via `Model[T]`

"A user may edit their own document; admins may edit any; never cross-tenant." Ownership needs the loaded object, so it runs after the fetch — with no per-handler boilerplate:

```go
var editDecider = access.FirstDecisive(
    access.TenantMatch(),
    access.Named("role", access.DeciderFunc(func(ctx context.Context, s access.Subject, a access.Action, r access.Resource) (access.Decision, error) {
        if slices.Contains(s.Scopes, "admin") {
            return access.Allow.Because("admin role"), nil
        }
        return access.Decision{}, nil // abstain; let ownership decide
    })),
    access.Named("owner", access.DeciderFunc(func(ctx context.Context, s access.Subject, a access.Action, r access.Resource) (access.Decision, error) {
        if id, _ := r.Attrs["owner_id"].(string); id == s.ID {
            return access.Allow.Because("subject owns resource"), nil
        }
        return access.Deny.Because("not owner"), nil // last real layer decides
    })),
)

var docs = access.NewModel(
    func(r *http.Request) (Document, error) {
        return store.Load(r.Context(), r.PathValue("id"))
    },
    func(d Document) access.Resource {
        return access.Resource{
            Type: "document", ID: d.ID, Tenant: d.Tenant,
            Attrs: map[string]any{"owner_id": d.OwnerID},
        }
    },
)

r.Handle("PUT /docs/{id}", authn(docs.Handle(editDecider, "documents:write",
    func(w http.ResponseWriter, r *http.Request, d Document) {
        // reached only if authorized; d is already loaded — just do the edit
    })))
r.Handle("GET /docs/{id}", authn(docs.Handle(readDecider, "documents:read",
    func(w http.ResponseWriter, r *http.Request, d Document) { render(w, d) })))
```

### How rbac/acl/abac plug in later

Each ships a constructor returning a `Decider` over its own Store; consumers drop them into a `FirstDecisive` chain in the documented precedence:

```go
decider := access.FirstDecisive(
    access.TenantMatch(),
    acl.Decider(aclStore),   // deny/grant overrides (deny wins)
    abac.Decider(predicates),// relationship predicates
    rbac.Decider(roleStore), // role → permission resolution
)
```

No change to `access`; the chain order is the precedence.

## Tenancy

`access` is stateless and stores nothing, so it needs **no `WithScope` construction seam**. Tenancy is vocabulary data: `Subject.Tenant` (from the token via `SubjectFromIdentity`) and `Resource.Tenant` (caller-supplied). The `TenantMatch` built-in is the fail-closed cross-tenant veto. Single-tenant apps leave both tenant fields empty; `TenantMatch` abstains and the whole mechanism costs nothing. Cross-tenant scoping of *stored* data (role assignments, acl grants) is the future rbac/acl Stores' responsibility, not this seam's.

## File layout

```
auth/access/
  doc.go          // package doc + runnable example
  access.go       // Subject, Action, Resource, Effect, Decision, Decider,
                  // DeciderFunc, Named, SubjectFromIdentity, Authorize
  combinators.go  // FirstDecisive, DenyOverrides, WithExplain (+ trace plumbing)
  deciders.go     // ScopeDecider, TenantMatch, AllowAll, DenyAll
  middleware.go   // RequirePermission, Require
  model.go        // Model[T], NewModel, Handle
  options.go      // Option, WithResource/WithSubject/WithResponder/WithLoadError/WithExplain
  context.go      // DecisionFrom + ctxkey stash
  errors.go       // minimal internal fail-closed reason constants
  *_test.go       // black-box access_test
  bench_test.go   // required
```

## Performance

- **Zero-alloc targets:** `FirstDecisive` + `ScopeDecider`/`TenantMatch` on the `Decide` path; the `RequirePermission` Allow path. `Decision` is a value struct of three strings + a nil slice; `Because` returns a value with constant-string reasons; `SubjectFromIdentity` copies scalars and a slice header only. A zero-alloc gate test enforces this (fsm precedent).
- **Trace** is the one intentional allocation and is off unless `WithExplain` — never on the hot path.
- **Interface dispatch** through `Decider` is the deliberate cost of the seam; combinators iterate a `[]Decider` with no per-call allocation.

## Testing policy

Black-box `access_test` package; `DeciderFunc`, `AllowAll`, `DenyAll` as the fakes (no external doubles). Coverage:

- **Combinator precedence:** `FirstDecisive` first-decisive + abstain fall-through + all-abstain; `DenyOverrides` veto-regardless-of-order; error → fail-closed `Deny` in both.
- **Built-ins:** `ScopeDecider` exact-match allow/abstain; `TenantMatch` cross-tenant deny, same-tenant abstain, either-empty abstain (single-tenant).
- **`Authorize`:** abstain → deny "no layer granted"; error → deny + error returned.
- **Middleware:** Allow → next + `DecisionFrom` populated; Deny/Abstain → 403 + no next; missing identity → 403; decider error → 403 + logged; `Require` dynamic action; `WithExplain` populates `Trace`.
- **`Model[T]`:** subject-absent → 403 without calling `Load`; `Load` error → 404 (and `WithLoadError` override); deny → 403; allow → `fn` called with the loaded object; `NewModel` nil-func panic.
- **`SubjectFromIdentity`:** field mapping, and Attrs left nil (no Meta promotion).

## Amendments applied during design

1. **Scope (decision B):** ship built-in stdlib deciders (`ScopeDecider`, `TenantMatch`, `AllowAll`, `DenyAll`) so `access` is a usable product before rbac; skip a static permission-map decider (rbac's territory).
2. **`Effect.Because` + `Named`:** ergonomic decision construction and layer labelling, so custom deciders avoid verbose struct literals and still feed the explanation record.
3. **`Authorize` + in-handler pattern:** a shared fail-closed resolver used by the middleware and by handlers; coarse checks in middleware, fine-grained ownership in-handler after load.
4. **`Model[T]` + `NewModel`:** typed load-then-authorize handler collapsing ownership boilerplate; inject-only handler signature keeps `access` out of error mapping.
5. **Context-gated trace** instead of a combinator option, preserving the variadic combinator signature.
6. **Zero-alloc `SubjectFromIdentity`:** does not promote `guard.Identity.Meta` into `Attrs`, avoiding a per-request map allocation on the middleware hot path.
