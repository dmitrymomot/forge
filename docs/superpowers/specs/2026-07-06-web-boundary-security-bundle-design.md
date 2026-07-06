# Web-Boundary Security Bundle — Design

**Date:** 2026-07-06
**Packages:** `web/cookie`, `web/csrf`, `web/secheaders`, `web/cors`, `web/timeout`, `web/compress`
**Delivery:** one PR, subagent-driven-dev, one task per package; retag commit first.

## Goals & decisions

- First slice of Build order wave 1 ("Web boundary + ops glue") from
  `docs/packages.md`. Unblocks `view/flash`, `auth/session` (cookiestore),
  and the rest of the security middleware story.
- Defaults must justify themselves for **both** server-rendered htmx apps and
  pure JSON APIs — no profile bias.
- **Approach A (chosen):** `csrf` composes `web/cookie` for its token cookie
  (signed path). One dependency edge; cookie gets an in-PR consumer.
  Rejected: standalone csrf over `crypto/sign` (duplicates cookie's policy
  surface and key plumbing); stateful csrf over `cache.Store` (out of the
  roadmap's "stateless" scope).
- Config shape is **mixed by nature**: env-loadable `Config` where values are
  deployment policy (cookie, cors, secheaders, timeout, compress);
  options-only where knobs are code (csrf).
- Zero new module dependencies: stdlib + shipped forge packages only.

## Env-prefix convention (new, repo-wide)

Leaf `Config` structs bake their package prefix into every `env` tag
(`COOKIE_KEYS`, not `KEYS`). Consumers then:

```go
type AppConfig struct {
    DB     postgres.Config                     // → DB_URL, DB_MAX_CONNS (untagged nest keeps names)
    Cookie cookie.Config                       // → COOKIE_KEYS, COOKIE_DOMAIN
    AppCookie     cookie.Config `env:"APP"`     // → APP_COOKIE_KEYS (tagged nest separates instances)
    LandingCookie cookie.Config `env:"LANDING"` // → LANDING_COOKIE_KEYS
}
```

Verified against `ops/config.populateStruct`: an untagged nested struct
recurses with the parent prefix unchanged; a tagged nest prepends
`TAG_`. Direct `Populate`/`LoadEnv` is collision-safe with baked prefixes.

Defaults live in `DefaultConfig()` only — **no `default=` tags** (httpserver
precedent; the loader leaves absent keys untouched, so
`cfg := DefaultConfig(); config.Populate(&cfg)` is the canonical flow).

### Retag of existing configs (first commit of the PR)

| Package | Prefix | Example |
|---|---|---|
| `web/httpserver` | `SERVER_` | `SERVER_ADDR` |
| `ops/logger` | `LOG_` | `LOG_LEVEL` |
| `ops/logger/sentry` | `SENTRY_` | `SENTRY_DSN` |
| `ops/supervisor` | `SUPERVISOR_` | `SUPERVISOR_SHUTDOWN_TIMEOUT` |
| `data/postgres` | `DB_` | `DB_URL`, `DB_MAX_CONNS` |
| `data/redis` | `REDIS_` | `REDIS_ADDRESSES` |
| `data/mongo` | `MONGO_` | `MONGO_URI` |
| `data/opensearch` | `OPENSEARCH_` | `OPENSEARCH_ADDRESSES` |

Mechanical change: tags, doc.go examples, tests/testdata. Breaking for env
names; pre-1.0, accepted. The convention gets a line in `docs/packages.md`.

New packages ship prefixed from birth: `COOKIE_*`, `CORS_*`,
`SECURITY_HEADERS_*`, `TIMEOUT_*`, `COMPRESS_*`.

## Shared conventions

- Constructors return `middleware.Middleware`; packages that can fail
  validation return `(middleware.Middleware, error)`. No `Must*` variants.
- Env-loadable middleware follow the httpserver idiom: `New(opts ...Option)`
  with a `WithConfig(Config)` option (place first; later options override).
  `cookie` is the exception: key material is mandatory, so `New(ks, opts...)`
  takes the keyset positionally and `FromConfig(cfg, opts...)` builds it from
  `cfg.Keys`.
- Rejections go through the `problem.Responder` seam
  (`func(http.ResponseWriter, *http.Request, error)`) carrying a package
  sentinel; overridable via `WithResponder`. Defaults use `problem.JSON`.
- Context carriers are package-scope `ctxkey.Key[string]`.
- Anatomy per package: `doc.go` (runnable example) · `config.go` (where
  env-loadable) · `options.go` (`type Option func(*config)`) · `errors.go` ·
  impl. Black-box tests only (`package x_test`, `net/http/httptest`).

## Dependency graph

```
cookie     → crypto/keyset, crypto/secret, crypto/sign
csrf       → cookie, crypto/random, web/middleware, web/problem, core/ctxkey
secheaders → web/middleware, core/ctxkey, crypto/random
cors       → web/middleware
timeout    → web/middleware, web/problem
compress   → web/middleware  (stdlib compress/gzip, compress/flate)
```

Implementation order: retag → cookie → csrf; secheaders, cors, timeout,
compress in parallel any time after the retag commit.

---

## web/cookie — signed + encrypted cookie codec

```go
type Codec struct{ ... }

func New(ks *keyset.Keyset, opts ...Option) (*Codec, error)
func FromConfig(cfg Config, opts ...Option) (*Codec, error) // keyset from cfg.Keys

type Config struct {
    Keys     string        `env:"COOKIE_KEYS"`      // "version:base64,..." → keyset.WithBase64Keys
    Path     string        `env:"COOKIE_PATH"`      // default "/"
    Domain   string        `env:"COOKIE_DOMAIN"`    // default "" (host-only)
    MaxAge   time.Duration `env:"COOKIE_MAX_AGE"`   // default 0 (session cookie)
    SameSite string        `env:"COOKIE_SAME_SITE"` // lax|strict|none, default "lax"
    Secure   bool          `env:"COOKIE_SECURE"`    // default true
    HTTPOnly bool          `env:"COOKIE_HTTP_ONLY"` // default true
}
func DefaultConfig() Config
func (c Config) Validate() error

// Explicit per-call security level. Plain Set/Get exist so one API covers
// every cookie in the app (policy defaults applied even to plain writes —
// no stdlib http.SetCookie mixing with forgotten flags):
func (c *Codec) Set(w http.ResponseWriter, name, value string, opts ...WriteOption) error
func (c *Codec) Get(r *http.Request, name string) (string, error)
func (c *Codec) SetSigned(w http.ResponseWriter, name, value string, opts ...WriteOption) error
func (c *Codec) GetSigned(r *http.Request, name string) (string, error)
func (c *Codec) SetEncrypted(w http.ResponseWriter, name, value string, opts ...WriteOption) error
func (c *Codec) GetEncrypted(r *http.Request, name string) (string, error)
func (c *Codec) Delete(w http.ResponseWriter, name string)

// Codec-level options mirror Config (WithPath, WithDomain, WithMaxAge,
// WithSameSite, WithSecure, WithHTTPOnly); WriteOption overrides per write
// (WithWriteMaxAge, WithWritePath, WithWriteSameSite, ...).
```

Mechanics:

- One keyset feeds both `sign.FromKeyset` and `secret.FromKeyset`; their
  versioned wire formats give transparent rotation from one env var.
- **Security levels:** plain (policy flags only) · signed (HMAC — tamper-proof,
  client-readable) · encrypted (AEAD — tamper-proof AND opaque). The encrypted
  path is NOT additionally signed: AEAD's auth tag already provides integrity
  and authenticity; a second MAC would be pure overhead. doc.go states this.
- **Name binding:** cookie name is mixed into the MAC message (signed path)
  and bound as AAD via `secret.WithAAD` (encrypted path) so a value minted
  for cookie A cannot be replayed as cookie B.
- `__Host-` names enforce Secure, Path=/, no Domain (error at write when the
  effective policy violates the prefix; `Validate` catches the static case).
- Encoded size > 4096 bytes → `ErrTooLarge` (browsers truncate silently).
- Signed wire: value + `sign.SignString` envelope, base64url. Encrypted
  wire: `secret.EncryptString` output.
- Sentinels: `ErrInvalidCookie` (absent/tampered/undecryptable — one error,
  no oracle), `ErrTooLarge`, `ErrInvalidConfig`.

Tests: round-trip all three paths, plain writes carry policy flags, rotation
(retired key still reads), name-bind replay rejection, `__Host-` enforcement,
size guard, tamper → `ErrInvalidCookie`.

## web/csrf — stateless double-submit over signed cookie

```go
func New(codec *cookie.Codec, opts ...Option) middleware.Middleware

func Token(r *http.Request) string // token for templates/meta/hx-headers; "" outside middleware

// Options: WithCookieName (default "__Host-csrf"; falls back to "csrf" when
// codec policy can't satisfy __Host-), WithHeader (default "X-CSRF-Token"),
// WithFormField (default "csrf_token"), WithResponder (default
// problem.JSON → 403), WithSkip(pred) for exempt routes.
//
// Sentinels: ErrTokenMissing, ErrTokenInvalid.
```

- Missing/invalid token cookie → mint 32-byte `crypto/random` token, write
  via `codec.SetSigned`, expose through ctxkey so `Token(r)` works on the
  minting request.
- Safe methods (GET/HEAD/OPTIONS/TRACE) pass through.
- Unsafe methods must echo the token: header first, then form field (form
  parsed only for form content types). Constant-time compare
  (`crypto/subtle`) against the verified cookie value.
- htmx integration is documentation only: `Token(r)` into
  `<meta name="csrf-token">` or `hx-headers` — no `web/htmx` import.
- Non-goals: per-request token rotation (breaks multi-tab), Origin/Referer
  fallback (SameSite=Lax already covers it), session binding (deferred to
  `auth/session`: rotate-on-login deletes the csrf cookie).

Tests: mint-then-Token same request, safe-method matrix, header + form echo
paths, tampered cookie → 403, skip predicate, constant-time compare present
(behavioral: wrong token rejected).

## web/secheaders — security headers + CSP nonce

```go
func New(opts ...Option) middleware.Middleware
func Nonce(ctx context.Context) string // per-request CSP nonce; "" when disabled

type Config struct {
    HSTSMaxAge            time.Duration `env:"SECURITY_HEADERS_HSTS_MAX_AGE"` // 0 disables HSTS
    HSTSIncludeSubdomains bool          `env:"SECURITY_HEADERS_HSTS_SUBDOMAINS"`
    FrameOptions          string        `env:"SECURITY_HEADERS_FRAME_OPTIONS"` // DENY (default) | SAMEORIGIN | off
    CSPReportURI          string        `env:"SECURITY_HEADERS_CSP_REPORT_URI"`
}
func DefaultConfig() Config          // FrameOptions "DENY"; empty normalizes to default
func (c Config) Validate() error
// WithConfig(cfg Config) Option — httpserver idiom
```

- Always-on defaults: `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: strict-origin-when-cross-origin`,
  `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin`.
  HSTS only when configured (deployment policy, wrong in dev).
- CSP via typed builder in options (`WithCSP(Policy{...})` with `Self`,
  `NonceScript`, `UnsafeInline`, ... constants) — never env (directive sets
  don't serialize). `WithNonce()` enables a 16-byte per-request
  `crypto/random` nonce injected into `script-src`/`style-src` and exposed
  via `Nonce(ctx)` for templ. **No CSP by default** — a default CSP breaks
  real apps; secure-by-default applies to the cheap headers only.
- Handler-set headers win: middleware checks `Header().Get` before setting.

Tests: default header set, HSTS gating, nonce uniqueness per request +
presence in CSP, handler-override respected.

## web/cors — env-loadable policy, preflight + actual

```go
func New(opts ...Option) (middleware.Middleware, error) // WithConfig(cfg) supplies policy

type Config struct {
    AllowedOrigins   []string      `env:"CORS_ALLOWED_ORIGINS"`  // exact, "*", or "https://*.example.com"
    AllowedMethods   []string      `env:"CORS_ALLOWED_METHODS"`  // default GET,POST,PUT,PATCH,DELETE,HEAD
    AllowedHeaders   []string      `env:"CORS_ALLOWED_HEADERS"`
    ExposedHeaders   []string      `env:"CORS_EXPOSED_HEADERS"`
    AllowCredentials bool          `env:"CORS_ALLOW_CREDENTIALS"`
    MaxAge           time.Duration `env:"CORS_MAX_AGE"`          // preflight cache
}
func DefaultConfig() Config
func (c Config) Validate() error // rejects bare "*" + credentials; scoped wildcards OK

WithOriginFunc(fn func(origin string) bool) // dynamic origins (tenant custom domains); overrides list
```

- Origin matching: exact match, or single-label subdomain wildcard
  (`https://*.example.com` matches `https://a.example.com`, not
  `https://a.b.example.com`, not `example.com` itself). `WithOriginFunc`
  replaces list matching entirely for DB-backed cases.
- No `Origin` header → pass untouched. Preflight `OPTIONS` (with
  `Access-Control-Request-Method`) answered 204 with allow-headers and
  `Vary: Origin, Access-Control-Request-Method,
  Access-Control-Request-Headers`. Disallowed origin: CORS headers omitted,
  request still served (browser enforces) — no 403, no responder.
- Sentinel: `ErrInvalidConfig` only.

Tests: preflight matrix, wildcard-subdomain matching bounds, bare-* +
credentials rejected at Validate, origin func override, Vary correctness,
non-CORS passthrough.

## web/timeout — per-request deadline

```go
func New(opts ...Option) (middleware.Middleware, error) // WithConfig(cfg) overrides default

type Config struct {
    Timeout time.Duration `env:"TIMEOUT_DURATION"` // must be > 0
}
func DefaultConfig() Config // 30s
func (c Config) Validate() error

WithResponder(r problem.Responder) // default problem.JSON → 504 carrying ErrTimeout
```

- `context.WithTimeout` around `next`, same goroutine — no stdlib
  `http.TimeoutHandler` goroutine juggling. Enforcement is cooperative via
  ctx; handlers that ignore ctx run long (documented honestly).
- After `next` returns with expired ctx and nothing written (checked via
  `middleware.WrapWriter`), responder writes 504. Already-written → no-op.
- Streaming/SSE routes: compose with `middleware.Skip` (doc example);
  `Flusher` reaches through via `Unwrap`.
- Sentinel: `ErrTimeout` (surfaces through responder), `ErrInvalidConfig`.

Tests: slow ctx-respecting handler → 504 problem+json, fast handler
untouched, already-written response left alone, Skip example compiles/runs.

## web/compress — gzip/deflate response compression

```go
func New(opts ...Option) (middleware.Middleware, error) // WithConfig(cfg) overrides defaults

type Config struct {
    MinSize int `env:"COMPRESS_MIN_SIZE"` // default 1024 bytes
    Level   int `env:"COMPRESS_LEVEL"`    // default gzip.DefaultCompression
}
func DefaultConfig() Config
func (c Config) Validate() error

WithContentTypes(types ...string) // default: text/*, application/json, application/javascript, image/svg+xml
```

- Local `Accept-Encoding` q-value parsing (~40 LOC); gzip preferred over
  deflate; identity honored (`gzip;q=0` → none).
- Buffers up to `MinSize` before deciding; small responses skip compression.
- Sets `Content-Encoding` + `Vary: Accept-Encoding`, strips `Content-Length`.
- Skips: existing `Content-Encoding`, `Range` requests, non-matching
  content types.
- **`Flush()` flushes gzip writer then underlying** — SSE survives (the
  roadmap's named constraint). Writers pooled via `sync.Pool`.
- No brotli (stdlib only). No sentinels.

Tests: negotiation matrix (q-values, identity), MinSize threshold both
sides, content-type allowlist, SSE flush ordering, Vary/Content-Length
handling, already-encoded passthrough.

---

## Testing & CI bar

- Black-box tests only; `httptest` request/recorder level (no port binding).
- Security behaviors are the review bar: name-bind replay, `__Host-`
  enforcement, csrf constant-time + method matrix, cors
  wildcard-credentials rejection, compress SSE flush, timeout streaming
  skip.
- `just fmt ./web/...`, `just lint`, `go test -race` green; modernize-clean
  (Go 1.26 `new(expr)` idiom); CLAUDE.md PR review loop until clean.

## docs/packages.md updates (same PR, final commit)

- Move `cookie csrf secheaders cors timeout compress` from planned → shipped
  under `web/`.
- Add the env-prefix convention line to Design DNA / seams section.
- Update the state counts (61 shipped → 67, 60 roadmap → 54).
