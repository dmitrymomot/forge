# Contributing to Forge

Thank you for your interest in contributing to Forge.

## Getting Started

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Run `make fmt && make lint && make test`
5. Open a pull request

## Development Commands

| Command               | Description                                |
|-----------------------|--------------------------------------------|
| `make test`           | Tests with race detection + coverage       |
| `make bench`          | Benchmarks with memory stats               |
| `make lint`           | vet, golangci-lint, nilaway, betteralign, modernize |
| `make fmt`            | Format + organize imports                  |
| `make test-integration` | Docker-based integration tests           |

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — New features
- `fix:` — Bug fixes
- `docs:` — Documentation changes
- `chore:` — Build, CI, tooling changes
- `refactor:` — Code changes that neither fix bugs nor add features
- `test:` — Adding or updating tests

## Code Standards

- All tests use `t.Parallel()` at both function and subtest level
- Use `require` (not `assert`) for critical checks
- No reflection, no service containers, no magic
- Packages receive values via parameters, not context
- All IDs must be generated using `pkg/id/` — no exceptions
- Public methods must not return unexported types
- Validator/sanitizer tags use semicolons as separators: `validate:"required;max:100"`

## Pull Request Checklist

- [ ] `make lint` passes
- [ ] `make test` passes
- [ ] New code has tests
- [ ] Commit messages follow conventional commits

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
