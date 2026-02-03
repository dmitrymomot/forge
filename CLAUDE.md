# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Forge is a Go framework for building B2B micro-SaaS applications. It provides importable packages with pre-built features (auth, multi-tenancy, billing, background jobs).

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
- Goose for migrations
- chi/v5 router
- River for background/scheduled tasks (Postgres-native queue)
- templ or html/template for SSR
- Tailwind CSS

## Architecture

```
forge/
├── forge.go                    # Public API entry point (re-exports internal types)
├── internal/                   # Core framework types (App, Context, Router, Handler)
├── pkg/                        # Importable utility packages
│   ├── binder/                 # Request binding (form, JSON, query, path)
│   ├── cookie/                 # Cookie helpers
│   ├── db/                     # Database connection, transactions, migrations
│   ├── health/                 # Health check endpoints
│   ├── hostrouter/             # Multi-domain routing
│   ├── htmx/                   # HTMX response helpers
│   ├── id/                     # ID generation (UUID, etc.)
│   ├── logger/                 # Structured logging with slog
│   ├── sanitizer/              # Input sanitization (strings, HTML, collections)
│   ├── session/                # Session management
│   └── validator/              # Input validation with struct tags
└── examples/                   # Usage examples (full-app, multi-domain, simple)
```

**Type aliasing pattern:** `forge.go` re-exports types from `internal/` as the public API. Import `github.com/dmitrymomot/forge` for `App`, `Context`, `Router`, `Handler`, etc.

## Design Principles

- **No magic:** Explicit code, no reflection, no service containers
- **Flat handlers:** Business logic lives in handlers, extract to services only when shared between handlers and tasks
- **Constructor injection:** Explicit wiring in main.go, all dependencies visible
- **Explicit over implicit:** Favor clear, readable code over clever abstractions
- **No redundant accessors:** Don't expose fields users already have (e.g., `Pool()` when they passed the pool to constructor)
- **No unexported returns:** Public methods must not return unexported types

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

## Testing

```bash
go test -v ./pkg/validator/...              # Test specific package
go test -v -run TestValidate ./pkg/...      # Run tests matching pattern
go test -race -cover ./...                  # Full test suite (same as make test)
```

### Test Style Requirements

- **Parallel execution:** All tests must use `t.Parallel()` at function and subtest level
- **Assertions:** Use `require` (not `assert`) for critical checks that should fail fast
- **Test cases:** Use `t.Run("descriptive name", ...)` subtests; table-driven tests only for simple functions
- **Integration tests:** Code requiring River/pgxpool is tested via integration tests, not unit tests

For integration tests, use `httptest.NewServer` with `app.Router()`:

```go
app := forge.New(forge.WithHandlers(myHandler))
ts := httptest.NewServer(app.Router())
defer ts.Close()
```

## Gotchas

- **Loop variables (Go 1.22+):** No need for `v := v` captures in closures; remove if found during review
- **Import ordering:** Run `make fmt` to organize imports (stdlib, external, local)
- **Struct alignment:** `betteralign` may reorder struct fields for memory efficiency
- **Examples excluded:** `make lint` excludes `examples/` from modernize checks
- **Build to /dev/null:** Never `go build` into repo; use `go build -o /dev/null ./...` to verify compilation
- **Validator tags:** Use semicolons as separators, colons for params: `validate:"required;max:100"` (not commas)
- **Sanitizer tags:** Use semicolons as separators: `sanitize:"trim;lowercase"` (same pattern as validator)
- **Framework, not template:** Forge is an importable library; template repos are separate and unknown to this codebase
