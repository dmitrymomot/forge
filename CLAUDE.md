# CLAUDE.md

Go framework for B2B micro-SaaS apps. Importable packages with pre-built features.
Module: `github.com/dmitrymomot/forge`

## Commands

```bash
make test             # Tests with race detection + coverage
make bench            # Benchmarks with memory stats
make lint             # vet, golangci-lint, nilaway, betteralign, modernize
make fmt              # Format + organize imports
make test-integration # Docker-based integration tests (postgres, redis, mailpit, rustfs)
```

## Architecture

`forge.go` re-exports types from `internal/` — import the root module for `App`, `Context`, `Router`, `Handler`. Utility packages live in `pkg/`. Middlewares in `middlewares/`.

## Design Rules

- No reflection, no service containers, no magic
- Packages receive values via parameters, not context; middleware handles context extraction
- Public methods must not return unexported types
- Don't expose fields users already have (no redundant accessors)
- Use `sync.Once` for lazy-initialized write-once fields
- Framework provides utility packages; business logic belongs in consumer repos

## Testing

- All tests use `t.Parallel()` at function and subtest level
- Use `require` (not `assert`) for critical checks
- Table-driven only for simple functions; use `t.Run("descriptive name", ...)` otherwise
- Integration tests for anything requiring River/pgxpool (`make test-integration`)
- Internal tests: use `requestVia()` helper to exercise real `requestContext` via App/Router

## Gotchas

- `requestContext` session methods (`Session()`, `InitSession()`, `DestroySession()`) are NOT goroutine-safe
- `recover()` returns `*runtime.PanicNilError` in Go 1.21+ (not nil)
- No `v := v` captures needed (Go 1.22+ loop variables)
- Validator/sanitizer tags use semicolons as separators, colons for params: `validate:"required;max:100"`
- Never `go build` into repo — use `go build -o /dev/null ./...`
- `make lint` excludes `examples/` from modernize
