# auth/magiclink — Design

Signed, TTL'd, single-use links over `crypto/token`: passwordless login, team
invites, verify and unsubscribe links. Stateless by default; `WithStore`
enables single-use redemption. Does not send email.

Deps: `crypto/token`, `resilience/cache` (Store seam only — no driver
imports). Also imported for API types surfaced by pass-through options:
`crypto/keyset`, `crypto/secret`, `core/clock`.

## Architecture

A thin generic layer over `crypto/token`. The manager wraps the consumer's
payload in its own envelope `{scope, payload}`, delegates
sign/encrypt/TTL/purpose enforcement to `token.Codec[envelope[T]]`, and adds
three things on top:

1. **Single-use redemption** — atomic `SetNX` claim on the
   `resilience/cache.Store` seam.
2. **Tenant scope binding** — the forge-wide `WithScope(ctx)` hook pattern,
   fail-closed.
3. **URL construction** — optional convenience for turning tokens into
   drop-in-an-email links.

Rejected alternatives:

- **Self-contained token format** — duplicates `crypto/token`, violates the
  catalog's pinned deps.
- **Extending `crypto/token` with jti/scope** — leaks auth concerns into a
  crypto brick; magiclink's envelope wrapper achieves the same without
  touching the brick.
- **HTTP surface (`FromRequest`, handlers, middleware)** — explicitly
  excluded (user decision). Consumers extract the token themselves
  (`r.URL.Query().Get("token")`); the two-step flow is a documented recipe,
  not shipped handlers. Session creation and redirects are consumer domain.

## API surface

```go
// Purpose is positional and required: two managers with the same key but
// different flows must never accept each other's tokens. Empty purpose is a
// constructor error.
func New[T any](key []byte, purpose string, opts ...Option) (*Manager[T], error)
func FromKeyset[T any](ks *keyset.Keyset, purpose string, opts ...Option) (*Manager[T], error)

// Options
WithTTL(d time.Duration)                        // default 15m; d <= 0 is a constructor error
WithStore(s cache.Store)                        // enables single-use redemption; nil is a constructor error
WithScope(fn func(context.Context) (string, error)) // tenancy hook; nil is a constructor error
WithBaseURL(u string)                           // default base for the single-domain case
WithParam(name string)                          // query param name, default "token"
WithEncrypt(box *secret.Box)                    // pass-through: hide payload (PII) from the URL
WithClock(c clock.Clock)                        // pass-through, tests; nil is a constructor error

// Core
func (m *Manager[T]) Issue(ctx context.Context, payload T) (string, error)
func (m *Manager[T]) IssueURL(ctx context.Context, base string, payload T) (string, error)
func (m *Manager[T]) Peek(ctx context.Context, token string) (T, error)   // non-consuming
func (m *Manager[T]) Redeem(ctx context.Context, token string) (T, error) // consuming
```

- `Issue` takes `ctx` because the scope hook reads tenant identity from it.
- `IssueURL`: per-call `base` wins; empty base falls back to `WithBaseURL`;
  both empty → error. The token is appended as a query parameter via
  `url.Parse`, preserving any existing query on the base.
- The envelope is internal and becomes the codec's payload type:

  ```go
  type envelope[T any] struct {
      Scope   string `json:"scp,omitempty"`
      Payload T      `json:"pld"`
  }
  ```

## Tenancy — scope binding

Works single- and multi-tenant per the forge-wide rule: optional
construction-time hook, fail-closed, zero ceremony single-tenant. Payload
claims (tenant ID, role) are the consumer's data; the scope hook is the
enforcement boundary — they are orthogonal.

- **Issue**: hook resolves `(scope, error)` from ctx. Error aborts issuance.
  Empty scope = deliberately global link (a hook that wants to forbid global
  issuance returns an error when ctx lacks a tenant).
- **Redeem/Peek**: scope is recomputed from the incoming ctx and matched
  against the token's stamped scope.

Matching (one rule, no special cases):

| token scope | ctx scope    | result           |
|-------------|--------------|------------------|
| `""`        | anything     | valid (global link, usable while switching tenants) |
| `"acme"`    | `"acme"`     | valid            |
| `"acme"`    | other / `""` | `ErrScopeMismatch` |

No hook configured means ctx scope is always `""`, so a scoped token hitting
an unscoped manager is rejected automatically — config drift fails closed.

## Redemption semantics

Verification order (shared by Peek/Redeem): **codec parse first** —
constant-time signature check, decrypt, expiry, purpose — so junk tokens are
rejected before any store I/O (cheap HMAC rejection; no store DoS). Then
scope check. Then store.

- **Redeem** (with store): `SetNX(key, 0x1, TTL=linkTTL)`; `cache.ErrExists`
  ⇒ `ErrUsed`. Atomicity comes from the store's `SetNX` — two concurrent
  redeems, exactly one wins, no locks. Any other store error ⇒ `ErrStore`
  (fail-closed).
- **Peek**: best-effort `Has(key)` ⇒ `ErrUsed` for the "already used" page;
  non-atomic is fine because the consuming POST is authoritative. A store
  error during Peek's Has is also `ErrStore` (fail-closed).
- **Stateless mode** (no `WithStore`): `Redeem` ≡ verify-only, multi-use by
  design — documented as suited to unsubscribe/verify flows where replay is
  harmless.
- **Store key**: `magiclink:<purpose>:<base64url(sha256(token))>`. Token
  hash because `crypto/token` does not expose its nonce; the hash is
  globally unique, so scope is not needed in the key. The entry may outlive
  the token by up to one TTL — harmless, the token expires first.
- **Memory-store caveat** (doc.go): the LRU memory store can evict live
  keys; production single-use needs `cache/redis` or a durable Store.

## TTL policy

Default 15 minutes. TTL is mandatory: `WithTTL(d <= 0)` is a constructor
error. Never-expiring magic links are a footgun and would leak store
entries.

## Errors

`errors.Is`-matchable single-line sentinels in `errors.go`:

- `ErrInvalid` — bad signature, malformed, or wrong purpose. Mapped from
  `crypto/token` errors and joined (`errors.Join`) so the underlying cause
  stays inspectable for logs without consumers importing `crypto/token`.
- `ErrExpired` — mapped from `token.ErrExpired`; distinct because the UX
  differs ("request a new link").
- `ErrUsed` — single-use claim already taken.
- `ErrScopeMismatch` — token scope does not match redeem context.
- `ErrStore` — store failure during Peek/Redeem; fail-closed, consumer
  treats as 500.

Constructor misuse (empty purpose, non-positive TTL, nil store/hook/clock)
returns plain errors from `New`/`FromKeyset`, collected via `errors.Join`
(mirrors `crypto/token`'s option-error pattern).

## Package anatomy

`auth/magiclink`, single package, no subpackages, ~350–450 LOC:

- `doc.go` — runnable example: passwordless login with the two-step
  scanner-safe recipe (GET → Peek → confirm button; POST → Redeem), the
  multi-tenant invite variant with `WithScope`, memory-store caveat, "does
  not send email" statement.
- `options.go` — `type Option func(*config)`, option errors collected.
- `errors.go` — sentinels above.
- `magiclink.go` — Manager, envelope, Issue/IssueURL/Peek/Redeem.
- `magiclink_test.go` — black-box tests.
- `bench_test.go` — benchmarks (repo rule).

## Usage example (abridged; full version in doc.go)

```go
type LoginClaims struct {
	UserID string `json:"uid"`
	Email  string `json:"eml"`
}

links, err := magiclink.New[LoginClaims](cfg.MagicLinkKey, "login",
	magiclink.WithTTL(15*time.Minute),
	magiclink.WithStore(redisStore),
	magiclink.WithBaseURL("https://app.example.com/auth/verify"),
)

// Request: issue and email the link (sending is the consumer's job).
url, _ := links.IssueURL(r.Context(), "", LoginClaims{UserID: u.ID, Email: u.Email})

// GET /auth/verify?token=... — Peek verifies without consuming, so email
// scanners that prefetch the URL cannot burn the link.
claims, err := links.Peek(r.Context(), r.URL.Query().Get("token"))

// POST /auth/verify — Redeem atomically claims single-use.
claims, err = links.Redeem(r.Context(), r.FormValue("token"))
```

Multi-tenant white-label:

```go
invites, _ := magiclink.New[InviteClaims](key, "invite",
	magiclink.WithStore(redisStore),
	magiclink.WithScope(func(ctx context.Context) (string, error) {
		return tenant.FromContext(ctx), nil // "" when issued from global context
	}),
)
url, _ := invites.IssueURL(ctx, "https://acme.example.com/join",
	InviteClaims{Email: "new@hire.com", Role: "admin"})
```

## Testing

Black-box (`package magiclink_test`):

- Issue → Redeem happy path; payload round-trip.
- Double redeem → `ErrUsed`; concurrent redeem race (N goroutines, exactly
  one winner).
- Peek non-consuming (Redeem still succeeds after); Peek after Redeem →
  `ErrUsed`.
- Stateless mode: repeated Redeem succeeds.
- Expiry via `clock.Mock` advance → `ErrExpired`.
- Cross-purpose rejection: token from manager A fails on manager B →
  `ErrInvalid`.
- Tampered token → `ErrInvalid`.
- Scope matrix: global-in-tenant OK; tenant-in-same-tenant OK;
  tenant-in-other-tenant → `ErrScopeMismatch`; tenant-in-global →
  `ErrScopeMismatch`; hook error propagates at issue and at redeem.
- URL building: per-call base; default base; both empty → error; base with
  existing query preserved; `WithParam` rename.
- `WithEncrypt` round-trip.
- Store failure fail-closed via an erroring `cache.Store` fake →
  `ErrStore`.

Benchmarks (`bench_test.go`): `Issue`, `Redeem` — allocation-conscious per
design.md; before/after numbers in the PR.

## Decisions log

| Decision | Choice | Why |
|----------|--------|-----|
| URL building | Layered: core = tokens, URL optional with per-call base | White-label needs per-tenant bases; single-domain gets `WithBaseURL` |
| Tenancy | Payload claims (data) + `WithScope` hook (enforcement) | Forge-wide pattern; consumers can't forget the check |
| Redemption | `Peek` + `Redeem` | Email scanners prefetch links; two-step recipe is the production-safe default |
| Stateless `Redeem` | Succeeds, multi-use, documented | Unsubscribe/verify links are legit stateless; ceremony buys nothing |
| HTTP surface | None | User decision; extraction is one line, flows are consumer domain |
| Purpose | Required positional arg | Cross-flow token confusion is a security bug, not a config nicety |
| TTL | Mandatory, default 15m | Never-expiring links are a footgun; store entries must expire |
