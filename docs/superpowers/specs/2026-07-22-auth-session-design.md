# auth/session — design

Date: 2026-07-22

## Why this document exists

Two earlier attempts at `auth/session` were abandoned. Both failed the same two ways:

- **Size.** ~12k LOC in one package, against a house limit of 250–850 LOC per package. Lifecycle, payload, expiry, five stores, four transports, a hook engine, four policies, HTTP middleware, tenancy, and device management all landed in one place, so nothing was reviewable and the bug count never converged.
- **Pseudo-extensibility.** Policies and transports lived *inside* the package, where they could reach unexported state. The hook and transport interfaces therefore never had to be sufficient. They looked pluggable and were not.

This design fixes the second problem structurally rather than by discipline, and the first by deleting scope.

## The extensibility rule

**Every policy, transport, and store ships in a package that imports `session`, never the reverse, and uses only its exported API.**

If a built-in cannot be written from outside the core, the seam is wrong and the seam gets fixed — the built-in does not get a private door. This is a compile-time check, not a review checklist. A third-party policy is indistinguishable from a first-party one.

## Scope

Session is a durable per-visitor bucket plus the token that points at it.

**In scope:** anonymous and authenticated sessions, a namespaced payload, sliding + absolute expiry, remember-me, a store seam, a transport seam, request-time policies, one automatic commit per request, device management.

**Explicitly out of scope:**

| Out | Owner |
|---|---|
| Authentication gating (401 on anonymous) | `auth/guard` |
| Authorization | `auth/access`, `rbac`, `acl`, `abac` |
| Fingerprint computation | `web/fingerprint` |
| Client IP extraction | `web/clientip` |
| Tenant resolution | `web/tenant` |
| Gating on step-up elevation | `auth/access` — session records the state and supplies a `Decider` |
| Multi-tenancy / scope isolation | dropped entirely |
| Basic-auth transport | dropped — sends credentials every request, no session to speak of |

Dropping tenancy is deliberate: tenant isolation is `web/tenant` + `access.TenantMatch()`, and a scope seam inside session bought a second isolation model that had to agree with the first.

## Architecture

Two entry points, two responsibilities.

```go
// Manager — lifecycle and storage. No HTTP, no transport, no policies, no request awareness.
sessions, err := session.New(cfg, session.WithStore(pgstore.New(pool)), ...)

// Middleware — the request layer: extract → load → validate → policies → context → commit.
mw := session.Middleware(sessions, session.WithTransport(...), session.WithPolicy(...))
```

Core defines the `Store`, `Transport`, and `Policy` interfaces and ships an in-memory `Store`. Everything else is a sibling package.

```go
type Config struct {
    Idle         time.Duration `env:"SESSION_IDLE" envDefault:"24h"`
    MaxTTL       time.Duration `env:"SESSION_MAX_TTL" envDefault:"168h"`
    RememberIdle time.Duration `env:"SESSION_REMEMBER_IDLE" envDefault:"720h"`
    RememberMax  time.Duration `env:"SESSION_REMEMBER_MAX_TTL" envDefault:"8760h"`
    Touch        time.Duration `env:"SESSION_TOUCH" envDefault:"5m"`
}
```

`Config` + `DefaultConfig` + `Validate` + `New(cfg, ...Option)` is the house idiom; every option has an env-loadable equivalent.

Middleware options: `WithTransport`, `WithPolicy`, `WithResponder(problem.Responder)` (default `problem.JSON` 401), `WithLogger`.

Core's only non-stdlib forge imports are `core/id`, `core/ctxkey`, `web/middleware`, `web/problem`, and `auth/guard` — the last solely for the `Identity`/`Verifier`/`Extractor` types used by the adapter. `guard` does not import `session`, so there is no cycle.

### Package layout and PR sequence

| # | Package | Contents |
|---|---|---|
| 1 | `auth/session` | `Session`, `Info`, `Namespace`, `Store`, `Transport`, `Policy`, `Manager`, `Middleware`, memory store, guard adapter, reaper |
| 2 | `auth/session/transport` | `Cookie`, `Bearer`, `JWT` |
| 3 | `auth/session/policy` | `MaxSessions`, `IPBinding`, `Fingerprint` |
| 4 | `auth/session/pgstore` | Postgres, full capability set |
| 5 | `auth/session/mongostore` | Mongo, native TTL index |
| 6 | `auth/session/kvstore` | over the `resilience/cache` `Store` seam |
| 7 | `auth/session/cookiestore` | stateless AEAD, no user index |

One PR each. PRs 2–7 are the extensibility proof: each is written against the exported API only.

Core is estimated at ~1300 LOC, above the 250–850 guideline. This is accepted because `session.Middleware` living beside `session.New` is the requested API; if it grows past that, the middleware and commit writer move to a subpackage without changing the manager.

## Core types

```go
type Session struct { /* unexported fields */ }

func (s *Session) ID() id.UUID          // minted by us: 16-byte uuid column, v7 time-ordered
func (s *Session) UserID() string       // owned by the app: "" means anonymous
func (s *Session) Token() string        // current raw token
func (s *Session) CreatedAt() time.Time
func (s *Session) ExpiresAt() time.Time
func (s *Session) LastSeenAt() time.Time
func (s *Session) IP() string           // pinned at creation
func (s *Session) UserAgent() string    // pinned at creation
func (s *Session) Fingerprint() string  // pinned at creation; fingerprint.Fingerprint.Hash
func (s *Session) Remembered() bool
func (s *Session) ElevatedAt() time.Time
func (s *Session) ElevatedWithin(d time.Duration) bool
func (s *Session) IsNew() bool
```

Accessors, not exported fields: a handler must not be able to write `sess.UserID = …` and bypass `Authenticate`'s rotation and store write. Mutation happens only through the manager.

`ID` is `id.UUID` because session mints it — native `uuid` column, and v7's time ordering makes newest-first `ListByUser` an index scan with no sort. `UserID` is `string` because the app owns it: it may be a UUID, a bigint, or an external IdP subject like `auth0|abc123`, and the surrounding stack (`guard.Identity.Subject`, `access.Subject.ID`, `rbac.Store.RolesFor`) is already `string`.

### Context

```go
type Info struct {
    ID         id.UUID
    UserID     string
    CreatedAt  time.Time
    ExpiresAt  time.Time
    ElevatedAt time.Time
}

func (i *Info) Authenticated() bool { return i.UserID != "" }
func FromContext(ctx context.Context) (*Info, bool)
```

One `WithValue` per request carrying a **pointer**, so a handler calling `Authenticate` mid-request does not leave a stale `UserID` behind for the rest of the chain. `Info` has no namespace access — the payload is reachable only through the manager.

### Handler access

```go
func (h *Handler) AddToCart(w http.ResponseWriter, r *http.Request) {
    sess := h.sessions.MustFor(r)  // context lookup, no I/O — record already loaded by the middleware
    cart, err := Cart.Get(sess)
    cart.Items = append(cart.Items, item)
    Cart.Set(sess, cart)
}
```

The manager is an explicit handler dependency: a handler that did not declare it cannot reach session data.

Two forms, following the standard `From`/`MustFrom` pair:

```go
func (m *Manager) For(r *http.Request) (*Session, bool)   // ok=false when the middleware isn't mounted
func (m *Manager) MustFor(r *http.Request) *Session       // panics — for handlers mounted behind it
```

Handlers whose routes carry the middleware use `MustFor`; code that may run outside it uses `For`. Only the `Must` form panics.

### Payload namespaces

```go
var Cart = session.NewNamespace[CartData]("cart")

cart, err := Cart.Get(sess)   // decodes "cart" on first read, caches on the session
Cart.Set(sess, cart)          // marks only "cart" dirty
```

The record holds `map[string]json.RawMessage`. On commit only dirty namespaces re-encode; untouched ones pass through as raw bytes, so a plugin's keys survive a save by a handler that has never heard of that plugin, and a request that reads nothing does zero JSON work.

Generics stop at the namespace. `Manager`, `Store`, `Transport`, and `Policy` are non-generic — a type parameter on the manager would propagate into every driver.

- A duplicate namespace name **panics at init**: a collision is a programming error, not a silent production overwrite.
- A namespace that fails to decode returns an **error from `Get`**, never a zero value. Corrupt or schema-drifted data fails closed rather than silently resetting a cart — or a permission set.

## Lifecycle

```go
mgr.Authenticate(ctx, sess, userID, session.Remember(bool))  // binds user + mandatory token rotation
mgr.Rotate(ctx, sess)                                        // explicit rotation
mgr.Destroy(ctx, sess)                                       // delete record + clear credential
mgr.Rebind(ctx, sess, session.Bind{IP, UserAgent, Fingerprint})
mgr.Elevate(ctx, sess)                                       // re-proof succeeded; stamps ElevatedAt
```

`Authenticate` preserves `ID` and `CreatedAt` across rotation and rolls back the in-memory token on a failed save, so a store error cannot orphan the client's credential.

An anonymous visitor with no credential costs **no store read and no row**. The row is minted on first save.

### Load model — always eager

The middleware loads the record on every request that carries a credential. Lazy loading was considered and rejected: a signed token can prove `exp`, but it cannot prove it is still the *current* token for that record, so rotation and revocation both require the read.

Consequence: the store read is on the hot path of every authenticated request. Store schema and index design matter more than JSON handling.

### Commit

One automatic commit per request, fired from a `ResponseWriter` wrapper at first byte. No handler-callable commit.

```go
func (w *commitWriter) WriteHeader(code int) {
    if !w.committed {
        w.committed = true
        w.commit()                      // store write + Transport.Embed → w.Header().Add("Set-Cookie", …)
    }
    w.ResponseWriter.WriteHeader(code)
}
```

Headers are still open when commit runs, so a failed save becomes a clean 500 with nothing leaked. If the handler never writes, commit runs after it returns.

This is why login-then-redirect works:

```
http.Redirect(w, r, "/app", 303)
  └─ w.Header().Set("Location", "/app")
  └─ w.WriteHeader(303)        ← intercepted: commit runs, Set-Cookie added to the same header map
  └─ real WriteHeader(303)     ← Location + Set-Cookie go out together
```

A `defer`-based commit after the handler returns would silently lose that `Set-Cookie` — the most common session write in a server-rendered app.

**Known sharp edge:** `http.ResponseController` walks the `Unwrap` chain, so `Flush` and `Hijack` reach the real writer without passing through `WriteHeader`. The wrapper therefore implements `Hijack()` and `FlushError()` explicitly, committing first. This deliberately departs from `middleware.WrapWriter`'s "don't re-declare optional interfaces" convention, because here a side effect is required, not just observation. Both paths need a test.

Commit never degrades to anonymous on infrastructure failure.

## Expiry

```go
session.WithIdle(24 * time.Hour)
session.WithMaxTTL(7 * 24 * time.Hour)
session.WithRememberIdle(30 * 24 * time.Hour)
session.WithRememberMaxTTL(0)          // 0 = no absolute cap: lives until logout or idle timeout
session.WithTouch(5 * time.Minute)
```

```go
exp := lastSeenAt.Add(idle)
if maxTTL > 0 {
    exp = min(exp, createdAt.Add(maxTTL))
}
```

`ExpiresAt` is always populated regardless, so Mongo's TTL index and the pg reaper keep working when `maxTTL == 0`.

Transport and JWT expiry are **always derived** from the effective deadline — never a separate knob.

`WithTouch` is required, not decorative: without it every request writes to the store; with it, one write per interval per session. Touch is metadata-only, never rotates, is fail-open, and is disabled for anonymous sessions and stateless stores.

### Remember-me

Two separable things behind one bool on the record:

1. **Which TTL pair the record gets** — transport-independent. A SPA or mobile client wants this as much as a browser.
2. **Persistent vs session-scoped client storage** — one bit, expressed differently per transport.

```go
mgr.Authenticate(ctx, sess, user.ID, session.Remember(form.Remember))   // no conditional option slices
```

```go
transport.Cookie(...)  // Embed: Remembered ? Max-Age = ExpiresAt-now : no Max-Age (dies with the browser)
transport.Bearer()     // Embed: rotated token in a response header; Remembered() in the body tells
                       //        the SPA localStorage vs sessionStorage — the exact analogue
```

## Metadata and fingerprint

`IP`, `UserAgent`, and `Fingerprint` are **first-class columns**, not payload. Device listings render without decoding the JSON blob at all.

**Pinned at creation. Only `LastSeenAt` moves.** Refreshing them per request does not weaken binding, it deletes it: a stolen cookie's first request would overwrite the row with the attacker's IP and fingerprint, and every subsequent request would match. `mgr.Rebind` is the deliberate re-pin after a successful re-authentication.

The device list therefore reads "signed in from Kyiv on Jul 3, last seen 2 minutes ago", which is what GitHub and Google show. Adding a `last_ip` column later is an additive migration; discovering binding has been silently broken for months is not.

`Fingerprint` holds the **hash only**, as an opaque `string` — session never imports `web/fingerprint`. The policy keeps `Digest.Parts` in its own namespace and decodes it only after the cheap column compare fails:

```go
if s.Fingerprint() == cur.Hash { return nil }         // 99% of requests stop here, no JSON
old, err := fpParts.Get(s)
if changed := fingerprint.Drift(old, cur.Digest()); len(changed) > tolerated {
    return session.Revoke("fingerprint drift: " + strings.Join(changed, ","))
}
```

Hash-only comparison was rejected because a Chrome auto-update and a stolen cookie are then indistinguishable, making strict mode unusable — and `web/fingerprint` documents that enabling Client Hints re-fingerprints every device once.

## Seams

### Store

```go
type Store interface {
    Load(ctx context.Context, token string) (Record, error)
    Save(ctx context.Context, token string, rec Record) (string, error)
    Delete(ctx context.Context, token string) error
}
```

The raw token is never persisted — stores key on a digest. `Save` returns the client-facing token so a stateless `cookiestore` (where the AEAD blob *is* the token) satisfies the same interface as server-side stores, which echo the token back.

Optional capabilities are separate interfaces, detected **once at construction**:

```go
type Toucher interface   { Touch(ctx context.Context, token string, expiresAt time.Time) error }
type UserIndex interface {
    ListByUser(ctx context.Context, userID string) ([]Record, error)
    DeleteByUser(ctx context.Context, userID string, keep ...id.UUID) error
    DeleteOne(ctx context.Context, userID string, sessionID id.UUID) error
}
type Expirer interface   { DeleteExpired(ctx context.Context, now time.Time) (int, error) }
```

Named `Expirer`, not `Reaper` — `session.Reaper` is already the `supervisor.Service` constructor in the same package.

`Record` is the stored shape: the first-class columns (`ID`, `UserID`, `CreatedAt`, `ExpiresAt`, `LastSeenAt`, `IP`, `UserAgent`, `Fingerprint`, `Remembered`, `ElevatedAt`) plus the payload as an opaque `[]byte`. Stores never interpret the payload and never see a `Session`.

A missing capability yields `ErrUnsupported` at the call, or a **boot error** if a configured option requires it (e.g. `WithTouch` on a store with no `Toucher`).

### Transport

```go
type Transport interface {
    Extract(r *http.Request) (token string, ok bool)
    Embed(w http.ResponseWriter, r *http.Request, s *Session) error
    Clear(w http.ResponseWriter, r *http.Request)
}
```

`Embed` receives `r` so a transport can derive cookie attributes per request (`r.TLS`, host, path) — `Extract` already gets it, and the asymmetry would be more surprising than the parameter.

Multiple transports are tried in order on extract. **Embedding goes to the matched transport, or the first listed one that supports it** — a transport that cannot write back returns `ErrNoEmbed`:

```go
session.WithTransport(QueryToken("t"), transport.Cookie(codec, "sid"))
```

That single rule makes the magic-link flow fall out: arrive on `/login?t=…`, `Authenticate` rotates, `QueryToken.Embed` returns `ErrNoEmbed`, `Cookie.Embed` sets the cookie on the handler's 303, and the token in the URL is dead — which limits the damage from the thing query tokens are bad at (leaking via Referer, access logs, browser history).

`Embed` sets headers; it must never write a response. Commit runs inside the handler's `WriteHeader`, so a transport that redirected would override every handler it is mounted under.

### Policy

```go
type Policy func(ctx context.Context, r *http.Request, s *Session) error
```

All policies are request-time — max-sessions fires at authenticate, IP binding and fingerprint at load. The manager therefore has **no hook machinery at all**; it is pure lifecycle and storage. Background jobs use the manager directly.

Three outcomes, ordered and short-circuiting:

| Return | Effect |
|---|---|
| `nil` | continue |
| `session.Deny(reason)` | 401, record survives |
| `session.Revoke(reason)` | delete record, 401 |
| any other error | fail closed, 500 |

Reasons surface to logs and to the responder. A third-party policy is a plain function:

```go
func BusinessHours(loc *time.Location) session.Policy {
    return func(ctx context.Context, r *http.Request, s *session.Session) error {
        if h := time.Now().In(loc).Hour(); h < 8 || h > 20 {
            return session.Deny("outside business hours")
        }
        return nil
    }
}
```

## Step-up elevation

Session records *that* the user recently re-proved identity. It never decides what that entitles them to.

`ElevatedAt` is a first-class column, not a namespace: the check is on the gate path of every protected request, so it must not cost a JSON decode, and it must be readable from `Info` by a decider that has no manager.

```go
mgr.Authenticate(ctx, sess, userID, ...)   // stamps ElevatedAt — the user just proved identity
mgr.Elevate(ctx, sess)                     // password/TOTP re-confirmed
// rotation preserves ElevatedAt; Destroy drops it with the record
```

Gating is an `access.Decider`, so elevation composes with roles in one chain instead of becoming a second gate beside guard's:

```go
decider := access.DenyOverrides(
    rbac.Decider(roles, rbac.FromSubject()),
    session.RequireElevation(10*time.Minute, "tenant:delete", "billing:manage"),
)

mux.Handle("DELETE /tenant", middleware.Wrap(h.DeleteTenant,
    access.RequirePermission(decider, "tenant:delete"),
))
```

**The action list is mandatory.** An unscoped elevation decider under `DenyOverrides` denies every action in the app; scoped, it abstains on everything outside its list. Both listed actions still have to satisfy `rbac` as well, since `DenyOverrides` requires agreement.

The window is per-gate, not config: delete-account and change-email justify different freshness. `sess.ElevatedWithin(d)` gives handlers the same freedom, and `access.Can` in a template hides the control exactly when the route would refuse it.

A denied elevation surfaces as **403 from `access`**, not a redirect to the re-proof page. Server-rendered apps redirect via `access.WithForbidden` + `access.DecisionFrom`, which today means matching on the decision's reason string — see Deferred.

## Integration with guard / access

Session does not gate or authorize. It exports an adapter; `guard` and `access` are unmodified.

```go
authed := guard.New(
    session.Verifier(sessions, session.WithIdentity(func(s *session.Session) guard.Identity {
        m, _ := Membership.Get(s)
        return guard.Identity{Subject: s.UserID(), Roles: m.Roles, Method: guard.MethodSession}
    })),
    guard.WithExtractors(session.Extractor()),
)
```

`session.Extractor()` and `session.Verifier()` both read the session the middleware already loaded, so guard gates without a second store read and without repeating the cookie name. `WithIdentity` is optional and lives on the adapter, never in core — the default publishes `Subject`/`Method` only, leaving roles to `rbac.FromStore`. Session core knows nothing about roles.

Downstream is then untouched: `guard.From` → `access.SubjectFromIdentity` → `rbac`/`acl`/`abac`, and `access.Can` in templates asks the same question the route gate asked.

`session.LogExtractor` mirrors `guard.LogExtractor` so request logs carry the session id.

## Device management

Manager methods — no request needed, usable from background jobs:

```go
mgr.ListByUser(ctx, userID)              // IP/UA/LastSeenAt are columns; no payload decode
mgr.Revoke(ctx, userID, sessionID)       // user-bound: cannot revoke another user's session
mgr.LogoutOthers(ctx, sess)
mgr.DeleteByUser(ctx, userID)            // GDPR
```

```go
sup.Add(session.Reaper(sessions, 15*time.Minute))   // supervisor.Service; no-op where the store reaps natively
```

## Errors

`errors.Is`-matchable single-line sentinels in `errors.go`: `ErrNotFound`, `ErrExpired`, `ErrUnsupported`, `ErrNoEmbed`, `ErrNoSession`. `Deny` and `Revoke` carry reasons.

## Testing

- Black-box (`package session_test`) throughout; white-box only to pin unexported decode primitives.
- A shared `storetest` conformance suite every store driver runs, including the capability matrix.
- DB drivers behind `//go:build integration` via `testkit`; default `go test ./...` stays Docker-free. Serial, not `t.Parallel()` — concurrent goose migrations on a shared container collide.
- `bench_test.go` required per package, with a post-benchmark optimization pass and before/after numbers in the PR. Baseline set: no-op request (load + commit, nothing dirty), single-namespace get/set, multi-namespace round-trip with unknown keys passing through, transport extract, commit with rotation.
- Named regression tests for: `Set-Cookie` on a 303 redirect, commit-before-`Hijack`, commit-before-`Flush`, rotation rollback on failed save, `ErrNoEmbed` fall-through, stale-`Info` after mid-request `Authenticate`, capability boot error, `RequireElevation` abstaining on unlisted actions, and elevation surviving rotation.

## Deferred

- **A typed reason on `access.Decision`** so an elevation denial can be matched with `errors.Is` instead of a substring check on `Decision.Reason` inside `WithForbidden`. A change to `auth/access`, not to session; kept out of this work to hold the PR scope.
- **`id.UID` constraint-only interface** in `core/id` (`var _ UID = UUID{}` conformance checks, never a field type). Standalone change.
- **Framework-wide id rule** to record in `docs/design.md`: `UUID` for internal identifiers (native column type in Postgres, ClickHouse, and Mongo; universal tooling), `Short` only where a collision is recoverable (32 random bits), `ULID` for interop only, `Prefix` for anything user-facing.
- **An auth-wide typed subject** across `guard`/`access`/`rbac`/`acl`/`session`. Worth doing; doing it in session alone buys type safety in one package and pays conversion friction in five.

## Amendment (2026-07-23): tenant scope seam restored

The "Multi-tenancy / scope isolation: dropped entirely" decision above was reversed by the project owner: every forge package must work in both single-tenant and multi-tenant apps via an optional construction-time seam, failing closed when a configured scope is missing. `auth/session` now ships `WithScope(fn func(context.Context) (string, error)) Option`, matching the idiom already shipped in `auth/apikey`, `ops/approval`, and `web/smartlink` — `web/tenant.Scope` plugs in directly via `session.WithScope(tenant.Scope)`. A `Tenant string` column was added to `Record`; `Manager.Save` resolves and stamps it before any mutation or write, `Manager.Load` post-filters on it after the expiry check (collapsing a cross-tenant token to `ErrNotFound`, the same error as truly-not-found, so tenant existence cannot be probed and a stolen cross-tenant token just yields a fresh anonymous session through the existing middleware fallback), and the `UserIndex` methods behind device management (`ListByUser`, `DeleteByUser`, `DeleteOne`) now take an explicit tenant, with `""` meaning no constraint — the same convention as `apikey.Filter.Tenant`. `DeleteExpired` stays global and unscoped: expiry is storage hygiene, not a tenant isolation boundary. With no `WithScope` configured, every record's `Tenant` stays empty and single-tenant use pays one nil check per call, with zero other behavior change. The rationale for reversing the original decision: `web/tenant` + `access.TenantMatch()` gate HTTP routes, but they do not confine store-layer device-management calls — a handler wired for tenant A could call `ListByUser`/`DeleteByUser`/`Revoke` and reach tenant B's sessions if the tenant boundary lived only at the route layer. `WithScope` closes that gap at the store boundary itself, the same reason `apikey` and `approval` scope their own bulk operations rather than relying solely on the caller's route-level tenant check.
- **`last_ip` / `last_user_agent`** columns if a live device view is wanted alongside binding.
