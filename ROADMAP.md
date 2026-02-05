# Forge Framework Roadmap

This document outlines planned features for the Forge framework.

**Current Status:** Active development — core framework functional, foundational packages and middlewares implemented.

---

## Implemented

### Core Framework

- `HTTPError` — structured error type with title, detail, error code, request ID
- Error handling helpers (`NewHTTPError`, `BadRequest`, `NotFound`, etc.)

### Middlewares (`middlewares/`)

- `requestid` — inject unique request ID
- `recover` — panic recovery with logging
- `timeout` — request timeout enforcement

### Utility Packages (`pkg/`)

- `binder` — request binding (form, JSON, query, path)
- `validator` — input validation with struct tags
- `sanitizer` — input sanitization (strings, HTML, collections)
- `htmx` — HTMX response helpers
- `session` — session management
- `cookie` — cookie helpers
- `db` — database connection, transactions, migrations
- `logger` — structured logging with slog
- `health` — health check endpoints
- `hostrouter` — multi-domain routing
- `id` — ID generation (UUID, etc.)
- `storage` — file storage abstraction (local filesystem, S3)

---

## Planned

### Utility Packages (`pkg/`)

| Package       | Description                                               | Reference |
| ------------- | --------------------------------------------------------- | --------- |
| `sse`         | SSE writer, event encoding, flush helpers                 |           |
| `websocket`   | Upgrader wrapper, connection management                   |           |
| `ratelimit`   | Token bucket, sliding window + memory/Redis stores        |           |
| `rbac`        | `Checker` interface, permission primitives (no DB)        |           |
| `featureflag` | `Provider` interface, strategies, memory impl             | [ref][1]  |
| `webhook`     | Sender with retries, signatures, circuit breaker, backoff | [ref][2]  |
| `oauth`       | `Provider` interface, Google/GitHub implementations       | [ref][3]  |
| `cache`       | `Cache` interface + memory/Redis implementations          |           |

[1]: /Users/dmitrymomot/Dev/boilerplate/pkg/feature
[2]: /Users/dmitrymomot/Dev/boilerplate/pkg/webhook
[3]: /Users/dmitrymomot/Dev/boilerplate/pkg/oauth

### Standard Middlewares

Part of framework core, configurable via options:

| Middleware  | Description                                |
| ----------- | ------------------------------------------ |
| `cors`      | CORS headers                               |
| `errorlog`  | Log 5xx errors with request context        |
| `audit`     | Audit log writer (configurable sink)       |
| `ratelimit` | Rate limiting (uses `pkg/ratelimit`)       |
| `rbac`      | Permission check (uses `pkg/rbac.Checker`) |

---

## Out of Scope

_Boilerplate responsibility, not framework:_

- Services (auth, billing, tenant, members, profile)
- DB implementations for RBAC, feature flags, audit storage
- Tenant-aware middlewares
- API key authentication middleware
- OAuth2/passwordless auth flows
