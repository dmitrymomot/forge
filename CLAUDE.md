# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Forge is a Go framework and template repository for building B2B micro-SaaS applications. It provides production-ready code with pre-built features (auth, multi-tenancy, billing, background jobs) that you clone and own completely.

**Status:** Concept stage — architecture documented, implementation pending.

## Commands

```bash
make test    # Run tests with race detection and coverage
make bench   # Run benchmarks
make lint    # Run all linters (vet, golangci-lint, nilaway, betteralign, modernize)
make fmt     # Format code and organize imports
```

## Tech Stack

- Go 1.25+
- PostgreSQL with pgx/v5
- sqlc for type-safe SQL queries
- Goose for migrations
- chi/v5 router
- River for background/scheduled tasks (Postgres-native queue)
- templ or html/template for SSR
- Tailwind CSS

## Architecture

```
forge/
├── pkg/                        # Importable runtime packages
│   ├── binder/                 # Request binding (form, JSON, query)
│   ├── cookie/                 # Cookie helpers
│   ├── db/                     # Database connection, transactions
│   ├── health/                 # Health check endpoints
│   ├── hostrouter/             # Multi-domain routing
│   ├── htmx/                   # HTMX response helpers
│   ├── id/                     # ID generation (UUID, etc.)
│   ├── logger/                 # Structured logging with slog
│   ├── sanitizer/              # HTML sanitization
│   ├── session/                # Session management
│   └── validator/              # Input validation
└── examples/                   # Usage examples
```

## Design Principles

- **No magic:** Explicit code, no reflection, no service containers
- **SQL-first:** Use sqlc-generated types directly (no internal/models layer)
- **Flat handlers:** Business logic lives in handlers, extract to services only when shared between handlers and tasks
- **Constructor injection:** Explicit wiring in main.go, all dependencies visible
- **Your code:** Users clone the template and own all code completely

## Key Patterns

### Handler Pattern

Handlers implement `Routes(Router)` to declare routes and receive dependencies via constructor.

### Task Pattern

Background tasks use River with type-safe payloads. Scheduled tasks use cron syntax. Both use the same queue system.

### Middleware Pattern

Standard `func(next) next` pattern using repo types directly.

## Go Tools

Uses Go 1.25 tool directives (`go.mod`). Install with `go tool <name>`:

- `golangci-lint` — linter aggregator
- `nilaway` — nil safety analysis
- `betteralign` — struct field alignment
- `goimports` — import organization
- `modernize` — Go idiom updates
- `mockery` — mock generation

## Gotchas

- **Import ordering:** Run `make fmt` to organize imports (stdlib, external, local)
- **Struct alignment:** `betteralign` may reorder struct fields for memory efficiency
- **Examples excluded:** `make lint` excludes `examples/` from modernize checks
- **Build to /dev/null:** Never `go build` into repo; use `go build -o /dev/null ./...` to verify compilation
- **Validator tags:** Use semicolons as separators, colons for params: `validate:"required;max:100"` (not commas)
