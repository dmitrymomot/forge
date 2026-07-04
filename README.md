# Forge

[![CI](https://github.com/dmitrymomot/forge/actions/workflows/ci.yml/badge.svg)](https://github.com/dmitrymomot/forge/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dmitrymomot/forge.svg)](https://pkg.go.dev/github.com/dmitrymomot/forge)
[![License](https://img.shields.io/github/license/dmitrymomot/forge)](LICENSE)

A simple, opinionated Go framework for building micro-SaaS applications.

Forge is designed around the principle of "no magic" — it uses explicit, readable code with no reflection or service containers. The framework provides a thin orchestration layer while keeping business logic in plain Go handlers.

## Installation

```bash
go get github.com/dmitrymomot/forge
```

## Documentation

Full API documentation is available via:

```bash
go doc -all github.com/dmitrymomot/forge
```

Or online at [pkg.go.dev/github.com/dmitrymomot/forge](https://pkg.go.dev/github.com/dmitrymomot/forge)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Design Principles

- No reflection, no service containers, no magic
- Packages receive values via parameters, not context
- Public methods must not return unexported types
- Framework provides utility packages; business logic belongs in consumer repos
- All IDs generated using `core/id` package exclusively

## License

Apache 2.0 — see [LICENSE](LICENSE) for details.
