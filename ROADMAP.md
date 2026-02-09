# Forge Framework Roadmap

This document outlines planned features for the Forge framework.

**Current Status:** Active development — core framework functional, foundational packages and middlewares implemented.

---

## Implemented

### Core Framework (`forge.go` + `internal/`)

- **Errors:** structured `HTTPError`, helpers (`BadRequest`, `NotFound`, etc.), pre-defined responses
- **Context:** stdlib-compatible, type-safe route/query access (`Param[T]`, `Query[T]`), composable extractors
- **Identity & Auth:** `UserID()`, `IsAuthenticated()`, `IsCurrentUser()`, `Can()` + role-based permission checks
- **Request/Response:** JSON/string/redirect writers, HTMX detection, request binding with validation, templ component rendering
- **Cookies & Sessions:** plain/signed/encrypted cookies, session lifecycle, session store operations
- **Logging:** request-scoped structured logger with level shortcuts
- **Background Jobs:** enqueue with transactional support
- **File Storage:** upload/download/delete operations with configurable backends
- **i18n:** translations, pluralization, locale-aware formatting (numbers, currency, dates)
- **App Config:** middleware/handler registration, static files, health checks, lifecycle hooks, multi-domain routing

### Middlewares (`middlewares/`)

- `requestid`, `recover`, `cors`, `i18n`, `jwt`, `auth` (`RequireAuthenticated`), `rbac` (`RequirePermission`, `RequireAnyPermission`)

### Utility Packages (`pkg/`)

**Core:** `binder`, `id`, `logger`, `db`, `redis`, `cache` (memory, Redis)
**Auth & Security:** `cookie`, `jwt`, `session`, `oauth`, `totp`, `fingerprint`, `dnsverify`
**Request/Response:** `clientip`, `hostrouter`, `htmx`, `useragent`
**Data:** `validator`, `sanitizer`, `slug`, `randomname`
**Integration:** `mailer`, `storage` (local, S3), `job` scheduling

---

## Planned

### Utility Packages (`pkg/`)

- `featureflag` — `Provider` interface with strategy pattern; memory store implementation
- `sse` — Server-sent events writer, event marshaling, flush helpers, client reconnection
- `websocket` — HTTP upgrader wrapper, connection lifecycle, message routing
- `compress` — Response compression negotiation (`gzip`, `zstd`), `Accept-Encoding` parsing, min-size threshold

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
