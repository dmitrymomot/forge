# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Forge is a Go framework and template repository for building B2B micro-SaaS applications. It provides production-ready code with pre-built features (auth, multi-tenancy, billing, background jobs) that you clone and own completely.

**Status:** Concept stage — architecture documented, implementation pending.

## Tech Stack

- Go 1.25+
- PostgreSQL with pgx/v5
- sqlc for type-safe SQL queries
- Goose for migrations
- chi/v5 router
- River for background/scheduled tasks (Postgres-native queue)
- templ or html/template for SSR
- Tailwind CSS

## Planned Architecture

```
forge/                          # This repository (framework + template)
├── pkg/                        # Importable runtime packages
│   ├── app/                    # Bootstrap, config, lifecycle
│   ├── web/                    # Router, context, middleware types
│   ├── db/                     # Connection, transactions
│   ├── mail/                   # Mailer interface + SMTP
│   ├── task/                   # Queue + scheduled (both via River)
│   ├── auth/                   # Password hashing, tokens
│   └── validate/               # Validation helpers
├── cmd/app/                    # Application entry point
├── config/                     # Configuration
├── db/                         # Migrations and queries
├── internal/                   # Application code (handlers, tasks, etc.)
└── web/                        # Templates and static assets
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
