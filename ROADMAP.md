# Forge Framework Roadmap

This document outlines planned features for the Forge framework.

**Current Status:** Active development — core framework functional, foundational packages and middlewares implemented.

---

## Implemented

### Core Framework

- `HTTPError` — structured error type with title, detail, error code, request ID
- Error handling helpers (`NewHTTPError`, `BadRequest`, `NotFound`, etc.)
- Pre-defined error responses (`ErrForbidden`, `ErrUnauthorized`, etc.)
- `Context.UserID()` — session user ID shortcut (empty string if unauthenticated)
- `Context.IsAuthenticated()` — checks session exists with user ID
- `Context.IsCurrentUser(id)` — compares `UserID()` to a given ID
- `Context.Can(permission)` — checks if current user's role has the permission (lazy role extraction, cached per request)
- `WithRoles(permissions, extractorFn)` — app option to configure role-to-permission map and role extractor function
- `Param[T](ctx, name)`, `Query[T](ctx, name)`, `QueryDefault[T](ctx, name, default)` — type-safe generic helpers for route params and query values
- `Context` implements `context.Context` — pass Forge context directly to stdlib functions without `.Context()` unwrapping

### Middlewares (`middlewares/`)

- `requestid` — inject unique request ID
- `recover` — panic recovery with logging
- `timeout` — request timeout enforcement
- `cors` — Cross-Origin Resource Sharing headers

### Utility Packages (`pkg/`)

- `binder` — request binding (form, JSON, query, path)
- `clientip` — client IP extraction with CDN header support
- `cookie` — cookie helpers
- `db` — database connection, transactions, migrations
- `dnsverify` — domain ownership verification via DNS TXT records
- `fingerprint` — device fingerprinting for session security
- `health` — health check endpoints
- `hostrouter` — multi-domain routing
- `htmx` — HTMX response helpers
- `id` — ID generation (UUID, etc.)
- `job` — background job scheduling
- `logger` — structured logging with slog
- `mailer` — template-based email rendering and sending
- `randomname` — human-readable random name generation
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

- `cache` — `Cache` interface + memory/Redis implementations
- `featureflag` — `Provider` interface, strategies, memory impl
- `i18n` — Translations: JSON/YAML/embed.FS loaders, CLDR plural rules, templ helpers via `t(ctx, key)`
- `jwt` — JWT generation and validation (HMAC-SHA256)
- `locale` — Locale-aware formatting: numbers, currency, dates, percentages
- `oauth` — `Provider` interface, Google/GitHub implementations
- `ratelimit` — Token bucket, sliding window + memory/Redis stores
- `redis` — Redis connection helper with retry logic
- `secrets` — AES-256-GCM encryption with key derivation
- `sse` — SSE writer, event encoding, flush helpers
- `webhook` — Sender with retries, signatures, circuit breaker, backoff
- `websocket` — Upgrader wrapper, connection management
- `extractor` — Composable value extraction from headers, cookies, query, form — chainable `FromHeader()`, `FromCookie()`, `FromQuery()` pattern for auth middleware, API key lookup, etc.
- `compress` — Response compression with `gzip` and `zstd` support, `Accept-Encoding` negotiation, min-size threshold

### Standard Middlewares

Part of framework core, configurable via options:

- `csrf` — CSRF protection (double-submit cookie)
- `errorlog` — Log 5xx errors with request context
- `audit` — Audit log writer (configurable sink)
- `ratelimit` — Rate limiting (uses `pkg/ratelimit`)
- `rbac` — Coarse-grained role/permission gate for route groups (uses `WithRoles` config)

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
