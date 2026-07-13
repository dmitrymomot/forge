# auth/guard — design

Date: 2026-07-13
Status: approved for planning

## Purpose

Request-authentication middleware: turn a credential on the request into an
`Identity` in request context, or reject with 401. **Authentication only** —
authorization (403, `RequirePermission`, the rbac/acl/abac decision seam)
arrives later with `auth/rbac`; guard's `Identity` carries what that future
seam will consume (Subject, Tenant, Scopes).

Primary consumers: every authenticated route group. Named verifier providers:
`auth/jwt` (shipped — adapter closure), `auth/session` and `auth/apikey`
(planned — both catalogued as satisfying `guard.Verifier`). Built-in Basic
Auth gates pprof/metrics/staging/admin. `ops/debug` and `auth/scim` list
guard as a dep.

## Placement

`auth/guard`, single package, no subpackages. Imports: `web/middleware`,
`web/problem`, `crypto/consttime`, `core/ctxkey`, `core/errorsx`,
`ops/logger` (LogExtractor only). No external deps. ~350–450 LOC plus
black-box tests. Standard anatomy: `doc.go` (runnable example), `options.go`,
`errors.go`, `guard.go`, `extractors.go`, `basicauth.go`, `context.go`.

When this ships: delete the `auth/guard` entry from docs/packages.md and
un-tag `auth/guard (planned)` in the `auth/apikey`, `auth/scim`, and
`ops/debug` entries.

## Decisions (resolved during brainstorming)

1. **Scope: authn only.** No decision-seam interface, no `RequirePermission`
   in v1 — rbac/acl/abac don't exist yet; designing the Allow/Deny seam
   against zero implementations was rejected. `Identity` is deliberately
   rich enough (Subject/Tenant/Scopes) for the 401-vs-403 split later.
2. **`Identity` is a concrete struct** — not generic, not an interface. One
   type all verifiers agree on; no generics ceremony in middleware, options,
   or context reads.
3. **One `Verifier` per guard + chained `Extractor`s.** Extractors pull the
   raw credential string; the single verifier validates it. Multi-scheme
   apps (session UI + API keys) mount separate guards per route group —
   `middleware.When`/`Skip` already cover routing. A `Scheme` pairing
   concept was rejected for v1 (second concept, ambiguous try-next
   semantics); it can be added later without breaking the seam.
4. **`WithOptional()` ships.** Missing credential → anonymous pass-through;
   present-but-invalid credential → 401 even in optional mode (silently
   ignoring bad tokens masks expired sessions and probing).
5. **Basic Auth is a dedicated constructor**, not forced through the
   Verifier seam — the credential would be an awkward `user:pass` blob and
   the middleware needs Basic-specific challenge knowledge anyway. It still
   writes an `Identity` to context so `guard.From` works uniformly.
6. **Default extractor chain is `BearerHeader()` only.** The catalog's
   "header → cookie → query" describes the available chain, not defaults:
   there is no universal cookie/param name to guess, and query-string
   credentials leak into access logs. `Cookie`/`Query` are explicit opt-ins;
   `Query`'s doc comment carries the leak warning.
7. **Context reader is `guard.From`/`guard.MustFrom`**, deviating from the
   catalog's `IdentityFromContext` — every shipped context reader in the
   repo is bare `From` (`requestid.From`, `geoip.From`); the catalog entry
   is deleted at ship time anyway.
8. **No `Config` type.** Guard has no env-tunable knobs; BasicAuth
   credentials arrive as a `map[string]string` (with a `ParseUsers` helper
   for `"u1:p1,u2:p2"` env strings) — consumers load env via `ops/config`
   in their own structs. doc.go shows the recipe.

## Core types

```go
// Identity is the authenticated principal a Verifier resolved.
type Identity struct {
    Subject string            // principal id — never empty on success
    Tenant  string            // optional tenant id
    Scopes  []string          // carried for the future authz decision seam
    Method  string            // "bearer", "session", "apikey", "basic", …
    Meta    map[string]string // verifier-specific extras (email, key id, …)
}

// Verifier turns an extracted credential into an Identity.
type Verifier interface {
    Verify(ctx context.Context, credential string) (Identity, error)
}

// VerifierFunc adapts a func to Verifier; doubles as the test fake
// (test doubles live with the seam owner).
type VerifierFunc func(ctx context.Context, credential string) (Identity, error)

// Extractor pulls a raw credential from a request. ok=false means
// "not present" (try the next extractor), never "present but bad".
type Extractor func(r *http.Request) (credential string, ok bool)
```

Verifier contract: a returned error means the credential is rejected —
guard maps it to 401 and never surfaces detail to the client. `Verify` must
be safe for concurrent use. A success with empty `Subject` is a verifier
bug; guard treats it as a rejection — 401 with `ErrInvalidCredential`
(defense against a zero-value `Identity, nil` return).

## Middleware

```go
func New(v Verifier, opts ...Option) middleware.Middleware
```

Per-request flow:

1. Run extractors in order; first `ok=true` wins. Extraction stops there —
   later extractors are not fallbacks for a failed verify.
2. No extractor hit: `WithOptional` → `next` untouched (no Identity in
   context); otherwise respond 401 with `ErrNoCredential`.
3. Credential found: `v.Verify(r.Context(), cred)`. Error → 401 with the
   verifier error wrapped in `ErrInvalidCredential` (both `errors.Is`
   matchable), **also in optional mode**. Success → store `Identity` via
   the package ctxkey, `next.ServeHTTP(w, r.WithContext(...))`.

On every 401, if a challenge is configured (`WithChallenge`), set
`WWW-Authenticate: <value>` before invoking the responder.

`New` panics on nil verifier (wiring bug — csrf/ipfilter precedent).
Options:

```go
func WithExtractors(xs ...Extractor) Option // replaces default; panics if empty
func WithOptional() Option
func WithResponder(r problem.Responder) Option // default problem.JSON(problem.WithStatus(401)); nil ignored
func WithChallenge(v string) Option // e.g. `Bearer realm="api"`; default none
```

## Shipped extractors

```go
func BearerHeader() Extractor  // Authorization: Bearer <token> (case-insensitive scheme)
func Header(name string) Extractor // raw header value, e.g. X-API-Key
func Cookie(name string) Extractor // cookie value
func Query(name string) Extractor  // query param — doc warning: leaks into access logs
```

All return `ok=false` on absent or empty values. `BearerHeader` returns
`ok=false` for a non-Bearer `Authorization` header (a Basic header on a
bearer-guarded route reads as "no credential", not a malformed one).
Custom extraction (signed cookies via `web/cookie.Codec`, etc.) is a
consumer closure — `Extractor` is just a func.

## Basic Auth

```go
func BasicAuth(users map[string]string, opts ...Option) middleware.Middleware
func WithRealm(realm string) Option // default "restricted"
func ParseUsers(s string) (map[string]string, error) // "u1:p1,u2:p2"
```

- Parses `Authorization: Basic` via `r.BasicAuth()`.
- Password check via `consttime.StringEqual`. Unknown username compares
  against a fixed dummy password so user existence doesn't leak via timing.
- Every failure → 401 with `WWW-Authenticate: Basic realm="<realm>",
  charset="UTF-8"` through the responder (default problem.JSON 401).
  Unknown user and wrong password are indistinguishable to the client —
  same `ErrInvalidCredential` (`auth_invalid`), same timing (dummy
  compare). Missing/malformed header → `ErrNoCredential` (`auth_missing`);
  that distinction leaks nothing (the client knows what it sent).
- Success → `Identity{Subject: username, Method: "basic"}` in context.
- Panics on empty/nil users map (a guard with no valid credentials is a
  wiring bug). Accepted options: `WithRealm`, `WithResponder`; `BasicAuth`
  ignores extractor/optional/challenge options (documented) — its scheme
  is fixed.
- `ParseUsers` rejects empty input, entries without `:`, empty usernames,
  and duplicate usernames. Realm values are sanitized (quotes/control
  chars rejected) since they're echoed into a header.

## Context & logging

```go
var identityKey = ctxkey.New[Identity]("guard")

func From(ctx context.Context) (Identity, bool)
func MustFrom(ctx context.Context) Identity // panics if absent — for handlers behind the guard

var LogExtractor logger.ContextExtractor // adds subject (+ tenant when set)
```

## Errors

`errors.go`, coded via `errorsx` so problem+json carries machine-readable
codes (`errorsx.Code`):

```go
var ErrNoCredential      = errorsx.New("auth_missing", "no credential provided")
var ErrInvalidCredential = errorsx.New("auth_invalid", "credential rejected")
```

Verify failures reach the responder as
`fmt.Errorf("%w: %w", ErrInvalidCredential, verifierErr)` — custom
responders can branch on `guard.Err*` or on provider sentinels
(`jwt.ErrExpired`) via `errors.Is`; `problem` resolves the code from the
outermost coded error (`auth_invalid`). The default responder forces
status 401 (`problem.WithStatus`) — never rely on error→status inference
(`request.StatusCode` maps unknown errors to 400). Response bodies stay
generic; verifier detail is server-side only.

## Usage (doc.go example, abridged)

```go
// JWT-backed API guard (auth/jwt shipped):
type appClaims struct {
    jwt.Claims
    TenantID string   `json:"tid"`
    Scopes   []string `json:"scopes"`
}
verifier := guard.VerifierFunc(func(ctx context.Context, token string) (guard.Identity, error) {
    c, err := jwt.Verify[appClaims](ctx, jwtVerifier, token)
    if err != nil {
        return guard.Identity{}, err
    }
    return guard.Identity{Subject: c.Subject, Tenant: c.TenantID, Scopes: c.Scopes, Method: "bearer"}, nil
})
authn := guard.New(verifier, guard.WithChallenge(`Bearer realm="api"`))

// Session-cookie browser guard (auth/session planned; redirect responder):
authn := guard.New(sessionVerifier,
    guard.WithExtractors(guard.Cookie("sid")),
    guard.WithResponder(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
    }),
)

// Staging gate:
users, _ := guard.ParseUsers(os.Getenv("ADMIN_BASIC_USERS"))
mux.Handle("/debug/pprof/", guard.BasicAuth(users, guard.WithRealm("staging"))(pprof))
```

Boundary note (documented in doc.go): guard answers "who is this request
from" only — session rotation, sliding TTLs, and Save-on-response are
`auth/session` middleware concerns; scope/permission checks are the future
authz seam.

## Performance

Hot path (valid credential): one extractor scan (header reads, no
allocation), one Verify call (verifier-owned cost), one context value.
No regex, no per-request option evaluation (config resolved in `New`).
Zero-alloc target for the guard's own work per design.md §Performance.

**Benchmarks are required** (project policy, all packages): `bench_test.go`
covers the valid-bearer flow, the 401 path, each shipped extractor,
BasicAuth success/failure, and LogExtractor. After the baseline run, a
post-benchmark optimization pass applies only measured wins (expected:
`Query` swaps `r.URL.Query()`'s full-map alloc for a `RawQuery` scan).
Inherent costs are documented, not optimized away: ~2 allocs/op for the
context store (`WithContext` + `WithValue`, requestid-identical), stdlib
cookie parsing in `Cookie`, and base64+HMAC in BasicAuth (constant-time is
the point). Before/after numbers go in the PR description.

## Testing

Black-box `guard_test` package, `httptest` + `VerifierFunc` fakes:

- Extractor chain: order, first-hit-wins, no-fallback-after-extract,
  each shipped extractor (present/absent/empty/malformed header forms).
- `New`: missing vs invalid vs valid credential; optional-mode matrix
  (missing→anonymous, invalid→401); challenge header set on both 401
  paths; responder override; wrapped-error `errors.Is` matching (guard
  sentinel + fake verifier sentinel); empty-Subject success treated as
  rejection; nil-verifier and empty-extractors panics.
- `BasicAuth`: correct login → Identity in context; wrong password,
  unknown user, malformed header → identical 401 + WWW-Authenticate with
  realm; `ParseUsers` accept/reject table; realm sanitization; empty
  users panic.
- Context: `From` ok/absent, `MustFrom` panic, `LogExtractor` attrs.

No fuzz targets — parsing is stdlib (`r.BasicAuth`, `strings.Cut` on the
Authorization header). No integration/external deps.

## Anti-scope

- No authorization: no Allow/Deny seam, no `RequirePermission`, no scope
  checks — `auth/rbac`+friends later.
- No credential issuance, no login/logout handlers, no session lifecycle.
- No multi-scheme `Scheme` pairing in v1 (separate guards per route group).
- No rate limiting / lockout on failures (`auth/lockout`'s job; composes
  as an outer middleware or inside a consumer's Verifier).
- No htpasswd/bcrypt file support in BasicAuth — static env credentials
  for internal gates only (documented; real user login belongs to
  session/password flows).
