# Forge Framework Roadmap

This document outlines planned features for the Forge framework.

**Current Status:** Active development — core framework functional, foundational packages and middlewares implemented.

---

## Implemented

### Core Framework

- `HTTPError` — structured error type with title, detail, error code, request ID
- Error handling helpers (`NewHTTPError`, `BadRequest`, `NotFound`, etc.)
- Pre-defined error responses (`ErrForbidden`, `ErrUnauthorized`, etc.)
- `Context` implements `context.Context` — pass Forge context directly to stdlib functions without `.Context()` unwrapping
- `Param[T](ctx, name)`, `Query[T](ctx, name)`, `QueryDefault[T](ctx, name, default)` — type-safe generic helpers for route params and query values
- `Extractor` — composable value extraction — chainable `FromHeader()`, `FromCookie()`, `FromQuery()`, `FromParam()`, `FromForm()`, `FromSession()`, `FromBearerToken()` pattern

**Context — Identity & Authorization:**
- `Context.UserID()` — session user ID shortcut (empty string if unauthenticated)
- `Context.IsAuthenticated()` — checks session exists with user ID
- `Context.IsCurrentUser(id)` — compares `UserID()` to a given ID
- `Context.Can(permission)` — checks if current user's role has the permission (lazy role extraction, cached per request)
- `Context.Role()` — returns the resolved role string (lazy extraction, cached per request, shared with `Can()`)
- `WithRoles(permissions, extractorFn)` — app option to configure role-to-permission map and role extractor function

**Context — Request & Response:**
- `Context.Header(name)` / `Context.SetHeader(name, value)` — read/write response headers
- `Context.Domain()` / `Context.Subdomain()` — host parsing helpers
- `Context.JSON(code, v)`, `Context.String(code, s)`, `Context.NoContent(code)`, `Context.Redirect(code, url)` — response writers
- `Context.Error(code, message, opts...)` — structured error response
- `Context.Written()` — check if response has been sent
- `Context.Bind(v)`, `Context.BindQuery(v)`, `Context.BindJSON(v)` — request binding with automatic validation
- `Context.IsHTMX()` — detect HTMX requests
- `Context.Render(code, component, opts...)` / `Context.RenderPartial(...)` — templ component rendering with HTMX partial support

**Context — Cookies & Flash Messages:**
- `Context.Cookie(name)` / `Context.SetCookie(name, value, maxAge)` / `Context.DeleteCookie(name)` — plain cookies
- `Context.CookieSigned(name)` / `Context.SetCookieSigned(...)` — HMAC-signed cookies
- `Context.CookieEncrypted(name)` / `Context.SetCookieEncrypted(...)` — AES-encrypted cookies
- `Context.Flash(key, dest)` / `Context.SetFlash(key, value)` — flash messages via session

**Context — Sessions:**
- `Context.Session()` / `Context.InitSession()` / `Context.DestroySession()` — session lifecycle
- `Context.AuthenticateSession(userID)` — bind user ID to session
- `Context.SessionValue(key)` / `Context.SetSessionValue(key, val)` / `Context.DeleteSessionValue(key)` — session key-value store

**Context — Logging:**
- `Context.Logger()` — request-scoped structured logger
- `Context.LogDebug(msg, attrs...)`, `Context.LogInfo(...)`, `Context.LogWarn(...)`, `Context.LogError(...)` — level shortcuts

**Context — Background Jobs:**
- `Context.Enqueue(name, payload, opts...)` / `Context.EnqueueTx(tx, name, payload, opts...)` — enqueue jobs (transactional support)

**Context — File Storage:**
- `Context.Storage()` — access configured storage backend
- `Context.Upload(field, opts...)` / `Context.UploadFromURL(sourceURL, opts...)` / `Context.Download(key)` / `Context.DeleteFile(key)` / `Context.FileURL(key, opts...)` — file operations

**Context — Internationalization:**
- `Context.T(key, placeholders...)` / `Context.Tn(key, n, placeholders...)` — translation with plural support
- `Context.Language()` — resolved locale
- `Context.FormatNumber(n)`, `Context.FormatCurrency(amount)`, `Context.FormatPercent(n)`, `Context.FormatDate(date)`, `Context.FormatTime(t)`, `Context.FormatDateTime(dt)` — locale-aware formatting

**App Configuration:**
- `WithMiddleware(mw...)`, `WithHandlers(h...)` — register middleware and route handlers
- `WithStaticFiles(pattern, fsys, subDir)` — serve embedded/static files
- `WithErrorHandler(h)`, `WithNotFoundHandler(h)`, `WithMethodNotAllowedHandler(h)` — custom error handling
- `WithHealthChecks(checks...)` — liveness/readiness endpoints
- `WithLogger(component, extractors...)` / `WithCustomLogger(l)` — structured logging
- `WithCookieConfig(cfg)` — global cookie settings (domain, secure, signing/encryption keys)
- `WithSession(store, cfg)` — session store and config
- `WithJobs(pool, cfg, opts...)` / `WithJobEnqueuer(...)` / `WithJobWorker(...)` — background job processing
- `WithTask[P, T](...)` / `WithScheduledTask[T](...)` — typed task registration
- `WithStorage(s)` — file storage backend
- `WithDomain(pattern, app)` / `WithFallback(app)` — multi-domain routing
- `WithStartupHook(fn)` / `WithShutdownHook(fn)` — lifecycle hooks
- `WithContext(ctx)` — custom root context

### Middlewares (`middlewares/`)

- `requestid` — inject unique request ID
- `recover` — panic recovery with logging
- `cors` — Cross-Origin Resource Sharing headers
- `i18n` — language resolution from Accept-Language/cookie/query, translator injection into context
- `jwt` — JWT authentication middleware with generic claims, configurable token extraction, context storage
- `auth` — Authentication gate (`RequireAuthenticated`)
- `rbac` — AND-permission gate (`RequirePermission`), OR-permission gate (`RequireAnyPermission`); uses `WithRoles` config

### Utility Packages (`pkg/`)

- `binder` — request binding (form, JSON, query, path)
- `cache` — Generic `Cache` interface + in-memory (LRU) and Redis implementations
- `clientip` — client IP extraction with CDN header support
- `cookie` — cookie helpers
- `db` — database connection, transactions, migrations
- `dnsverify` — domain ownership verification via DNS TXT records
- `fingerprint` — device fingerprinting for session security
- `hostrouter` — multi-domain routing
- `htmx` — HTMX response helpers
- `i18n` — Translations: JSON/YAML/embed.FS loaders, CLDR plural rules, locale-aware formatting (numbers, currency, dates, percentages), templ helpers via context
- `id` — ID generation (UUID, etc.)
- `job` — background job scheduling
- `jwt` — RFC 7519 JWT generation and validation (HMAC-SHA256), standard/custom claims, constant-time signature verification
- `logger` — structured logging with slog
- `mailer` — template-based email rendering and sending
- `oauth` — OAuth2 authorization code flow with `Provider` interface, Google/GitHub implementations, custom HTTP client injection
- `randomname` — human-readable random name generation
- `redis` — Redis connection helper with retry logic
- `sanitizer` — input sanitization (strings, HTML, collections)
- `session` — session management
- `slug` — URL-safe slug generation with diacritic normalization
- `storage` — file storage abstraction (local filesystem, S3)
- `totp` — Time-based One-Time Passwords (RFC 6238)
- `useragent` — User-Agent parsing with bot detection
- `validator` — input validation with struct tags

---

## Planned

### Utility Packages (`pkg/`)

- `featureflag` — `Provider` interface, strategies, memory impl
- `sse` — SSE writer, event encoding, flush helpers
- `websocket` — Upgrader wrapper, connection management
- `compress` — Response compression with `gzip` and `zstd` support, `Accept-Encoding` negotiation, min-size threshold

#### `ratelimit` — Sliding window rate limiter with pluggable storage

Sliding window algorithm using a weighted blend of current + previous window counts for smooth rate enforcement.

**Core types:**
- `Limiter` — main rate limiter with `Allow(ctx, key)`, `AllowN(ctx, key, n)`, `Peek(ctx, key)` methods
- `Info` struct — response from limiter: `Limit`, `Remaining`, `ResetAt`, `RetryAfter`
- `Counter` interface — pluggable storage backend for window counts
- `KeyFunc` type — function that extracts rate-limit keys from requests

**Storage backends:**
- `MemoryCounter` — in-memory with background cleanup of expired windows
- `RedisCounter` — Redis-backed with automatic fallback to memory on connection failure

**Key extractors** (composable via `KeyComposite`):
- `KeyByIP` — client IP address
- `KeyByFingerprint` — device fingerprint
- `KeyByHeader(name)` — arbitrary request header
- `KeyByPath` — request URL path
- `KeyComposite(funcs...)` — combine multiple extractors into a single key

**Errors:** `ErrRateLimited`, `ErrInvalidLimit`, `ErrInvalidWindow`, `ErrNilCounter`

> Reference: `boilerplate/pkg/ratelimiter/`

#### `secrets` — AES-256-GCM encryption with compound key derivation

Compound key model: app key + workspace key → HKDF derivation for tenant-isolated encryption. Automatic nonce generation prepended to ciphertext. Sensitive key material cleaned via `defer clearBytes(key)`.

**Constants:** `KeySize = 32` (AES-256)

**Functions:**
- `GenerateKey() ([]byte, error)` — generate a random encryption key
- `ValidateKeys(appKey, workspaceKey []byte) error` — validate key sizes
- `EncryptString(plaintext string, appKey, workspaceKey []byte) (string, error)`
- `DecryptString(ciphertext string, appKey, workspaceKey []byte) (string, error)`
- `EncryptBytes(data, appKey, workspaceKey []byte) ([]byte, error)`
- `DecryptBytes(data, appKey, workspaceKey []byte) ([]byte, error)`

**Errors:** `ErrInvalidAppKey`, `ErrInvalidWorkspaceKey`, `ErrEncryptionFailed`, `ErrDecryptionFailed`, `ErrInvalidCiphertext`, `ErrKeyDerivationFailed`

**Dependency:** `golang.org/x/crypto/hkdf`

> Reference: `boilerplate/pkg/secrets/`

#### `webhook` — Webhook delivery with retries, signatures, and circuit breaking

**Sender:**
- `Sender` — main entry point with `Send(ctx, url, data, opts...) (*DeliveryResult, error)`
- JSON marshaling, configurable retries, circuit breaking, payload signing

**Signature verification:**
- `SignPayload(payload, secret []byte) string` — HMAC-SHA256 signature
- `VerifySignature(payload []byte, signature string, secret []byte) bool` — constant-time verification
- `ExtractSignatureHeaders(r *http.Request) (signature, timestamp string)` — extract headers for replay prevention

**Circuit breaker:**
- `CircuitBreaker` — 3-state machine: Closed → Open → HalfOpen
- Methods: `Allow() bool`, `RecordSuccess()`, `RecordFailure()`, `Stats() Stats`

**Backoff strategies:**
- `BackoffStrategy` interface with `NextDelay(attempt int) time.Duration`
- `ExponentialBackoff` — exponential with jitter
- `LinearBackoff` — linear increase
- `FixedBackoff` — constant delay

**Result type:**
- `DeliveryResult` struct — `Error`, `StatusCode`, `Attempt`, `Duration`, `Success`
- `DeliveryHook` callback type — invoked after each delivery attempt

**Functional options:** `WithTimeout`, `WithMaxRetries`, `WithBackoff`, `WithSignature`, `WithCircuitBreaker`, `WithOnDelivery`, `WithMaxPayloadSize`, `WithHeaders`

**Errors:** `ErrWebhookDeliveryFailed`, `ErrCircuitOpen`, `ErrPermanentFailure`, `ErrTemporaryFailure`

**Dependency:** `github.com/google/uuid`

> Reference: `boilerplate/pkg/webhook/`

#### `token` — Compact URL-safe signed tokens

Compact URL-safe signed tokens with truncated HMAC-SHA256 (8-byte signature). Format: `base64url-payload.base64url-signature`. Constant-time signature comparison. Zero external dependencies (stdlib only).

**Generic functions:**
- `GenerateToken[T any](payload T, secret []byte) (string, error)` — encode + sign any JSON-serializable payload
- `ParseToken[T any](token string, secret []byte) (*T, error)` — verify signature + decode payload

**Use cases:** email verification links, invite tokens, magic links, password reset tokens

**Errors:** `ErrInvalidToken`, `ErrSignatureInvalid`

> Reference: `boilerplate/pkg/token/`

#### `qrcode` — QR code generation

**Functions:**
- `Generate(content string, size ...int) ([]byte, error)` — PNG bytes, default 256px, medium error correction
- `GenerateBase64Image(content string, size ...int) (string, error)` — data URI string for HTML `<img src="">`

Content validation rejects empty or whitespace-only input. Pairs with existing `totp` package for authenticator app QR codes.

**Errors:** `ErrEmptyContent`, `ErrFailedToGenerateQRCode`

**Dependency:** `github.com/skip2/go-qrcode`

> Reference: `boilerplate/pkg/qrcode/`

#### `geolocation` — IP geolocation via MaxMind databases

**Types:**
- `Location` struct — `Country` (ISO 3166-1), `City`, `Region`, `Timezone` (IANA) + `String()` method
- `Provider` interface — `Lookup(ctx context.Context, ip string) (*Location, error)` + `Close() error`

**Implementations:**
- `MaxMindProvider` — memory-mapped GeoIP2/GeoLite2 database reader, thread-safe via `sync.RWMutex`
- Graceful degradation: returns nil (not error) for private/loopback IPs

**Errors:** `ErrClosed`

**Dependency:** `github.com/oschwald/geoip2-golang`

> Reference: `boilerplate/pkg/geolocation/`

### Standard Middlewares

Part of framework core, configurable via options:

- `csrf` — CSRF protection (double-submit cookie)
- `audit` — Audit log writer (configurable sink)
- `ratelimit` — Rate limiting (uses `pkg/ratelimit`)


---

## Under Consideration

- `render` — Template rendering integration based on [MiniJinja-Go](https://github.com/dmitrymomot/minijinja/tree/main/minijinja-go) — Jinja2 templates with dev-mode hot-reload, production caching, and middleware for template globals
- `negotiate` — Content negotiation helpers — auto-format response based on `Accept` header (JSON, XML, plain text)
- `streaming` — General streaming response support — chunked transfers, large downloads, live data (complements `sse`)

---

## Out of Scope

_Boilerplate responsibility, not framework:_

- Services (auth, billing, tenant, members, profile)
- DB implementations for RBAC, feature flags, audit storage
- Tenant-aware middlewares
- API key authentication middleware
- OAuth2/passwordless auth flows
