# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Forge is a Go framework for building B2B micro-SaaS applications. It provides importable packages with pre-built features (auth, multi-tenancy, billing, background jobs).

**Status:** Active development — core packages implemented, see ROADMAP.md for planned features.

## Commands

```bash
make test               # Run tests with race detection and coverage
make lint               # Run all linters (vet, golangci-lint, nilaway, betteralign, modernize)
make fmt                # Format code and organize imports
make test-integration   # Run integration tests (starts docker, runs tests, stops docker)
```

## Architecture

```
forge/
├── forge.go      # Public API (re-exports internal types)
├── internal/     # Core framework types (App, Context, Router, Handler)
├── pkg/          # Importable utility packages (see pkg/ for full list)
└── examples/     # Usage examples
```

**Type aliasing:** `forge.go` re-exports types from `internal/`. Import `github.com/dmitrymomot/forge` for `App`, `Context`, `Router`, `Handler`.

## Design Principles

- **No magic:** Explicit code, no reflection, no service containers
- **Flat handlers:** Business logic lives in handlers, extract to services only when shared between handlers and tasks
- **Constructor injection:** Explicit wiring in main.go, all dependencies visible
- **Explicit over implicit:** Favor clear, readable code over clever abstractions
- **No redundant accessors:** Don't expose fields users already have (e.g., `Pool()` when they passed the pool to constructor)
- **No unexported returns:** Public methods must not return unexported types
- **Framework, not boilerplate:** Forge provides utility packages; business logic (auth, billing, tenants) belongs in boilerplate repos
- **No context helpers in packages:** Packages receive values via parameters, not context. Middleware handles context extraction.

## Key Patterns

- **Handlers:** Implement `Routes(Router)`, receive dependencies via constructor
- **Tasks:** River with type-safe payloads, cron syntax for scheduled tasks
- **Config:** Use `env` tags (caarlos0/env) with `envPrefix` for composable configs

## Testing

```bash
go test -v ./pkg/validator/...    # Test specific package
go test -v -run TestName ./...    # Run tests matching pattern
```

### Test Style

- **Parallel:** All tests must use `t.Parallel()` at function and subtest level
- **Assertions:** Use `require` (not `assert`) for critical checks
- **Subtests:** Use `t.Run("descriptive name", ...)`; table-driven only for simple functions
- **Integration:** Code requiring River/pgxpool uses integration tests (`make test-integration`)
- **Internal tests:** Use `requestVia()` helper in `context_interface_test.go` to exercise real `requestContext` via the App/Router. Mock dependencies with `mockSessionStore`, `mockStorage`, etc.

## Gotchas

- **requestContext is single-goroutine:** `internal.requestContext` is NOT goroutine-safe (e.g., `Can()` caches role without synchronization). Don't spawn goroutines calling Context methods in tests.
- **panic(nil) in Go 1.21+:** `recover()` returns `*runtime.PanicNilError`, not nil. Account for this in panic recovery tests.
- **Loop variables (Go 1.22+):** No need for `v := v` captures in closures; remove if found during review
- **Import ordering:** Run `make fmt` to organize imports (stdlib, external, local)
- **Struct alignment:** `betteralign` may reorder struct fields for memory efficiency
- **Examples excluded:** `make lint` excludes `examples/` from modernize checks
- **Build to /dev/null:** Never `go build` into repo; use `go build -o /dev/null ./...` to verify compilation
- **Validator tags:** Use semicolons as separators, colons for params: `validate:"required;max:100"` (not commas)
- **Sanitizer tags:** Use semicolons as separators: `sanitize:"trim;lowercase"` (same pattern as validator)
