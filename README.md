# Forge

A Go framework for building B2B micro-SaaS applications with Rails-like developer experience but without magic.

## Why Forge?

**Problem:** Starting a new micro-SaaS requires the same boilerplate every time — auth, multi-tenancy, billing, background jobs, emails. Copy-pasting from previous projects is error-prone and tedious.

**Solution:** A code generator that scaffolds production-ready applications with pre-built features, while keeping all code in your project and fully modifiable.

## Goals

- **Maximum scaffolding, minimum runtime magic** — generate explicit code, no reflection
- **Convention over configuration** — sensible defaults, escape hatches when needed
- **Own your code** — generated code lives in your repo, modify anything
- **B2B SaaS focused** — multi-tenancy, team management, billing built-in
- **SSR-first** — optimized for server-rendered apps with templ/html templates

## Tech Stack

| Layer      | Technology                  |
| ---------- | --------------------------- |
| Language   | Go 1.25+                    |
| Database   | PostgreSQL (pgx/v5)         |
| Queries    | sqlc (SQL-first, type-safe) |
| Migrations | Goose                       |
| Router     | chi/v5                      |
| Queue      | River (Postgres-native)     |
| Config     | caarlos0/env (env vars)     |
| Templates  | templ or html/template      |
| CSS        | Tailwind                    |
| Dev Tools  | Air, Taskfile, Docker       |

## Architecture

```
forge/                          # Framework repository
├── cmd/forge/                  # CLI tool (generator)
├── pkg/                        # Importable runtime packages
│   ├── app/                    # Bootstrap, config, lifecycle
│   ├── web/                    # Router, context, middleware types
│   ├── db/                     # Connection, transactions
│   ├── mail/                   # Mailer interface + SMTP
│   ├── task/                   # Queue + scheduled (both via River)
│   ├── auth/                   # Password hashing, tokens
│   └── validate/               # Validation helpers
└── templates/                  # Code generation templates
```

```
your-app/                       # Generated project
├── forge.yaml                  # Forge project configuration
├── Taskfile.yml                # Task runner (dev, build, test, etc.)
├── docker-compose.yml          # Postgres + Mailpit for local dev
├── .air.toml                   # Hot reload config
├── .env.example                # Environment variables documentation
├── .env                        # Local overrides (gitignored)
├── cmd/app/main.go             # Thin entry point
├── config/
│   └── config.go               # Config struct + LoadConfig()
├── db/
│   ├── migrations/             # SQL migrations
│   └── queries/                # sqlc query definitions
├── internal/
│   ├── repository/             # sqlc generated
│   ├── handlers/               # HTTP handlers (with business logic)
│   ├── middlewares/            # Custom middleware
│   └── tasks/                  # Background + scheduled tasks (River)
└── web/templates/              # UI templates
```

## Main Concept

Thin `main.go` with explicit wiring — you see all dependencies:

```go
func main() {
    cfg, err := config.Load()
    if err != nil {
        log.Fatal(err)
    }

    pool := db.Connect(cfg.Database.URL)
    repo := repository.New(pool)
    mailer := mail.New(cfg.Mail)
    stripe := billing.NewStripe(cfg.Stripe)
    queue := task.NewQueue(pool)

    app.Run(
        app.WithMiddleware(
            middlewares.LoadTenant(repo),
        ),
        app.WithHandlers(
            handlers.NewAuth(repo, queue),
            handlers.NewBilling(repo, stripe),
            handlers.NewPages(repo),
        ),
        app.WithTasks(
            task.Wrap(tasks.NewSendWelcome(repo, mailer)),
        ),
        app.WithSchedule(
            task.Every(1*time.Hour, tasks.NewCleanupSessions(repo)),
            task.Cron("0 9 * * MON", tasks.NewWeeklyDigest(repo, mailer)),
        ),
    )
}
```

No hidden initialization. No service containers. No reflection.

## Configuration

Environment variables via [caarlos0/env](https://github.com/caarlos0/env) — no YAML configs, no preprocessing:

```go
// config/config.go
type Config struct {
    Server   ServerConfig
    Database DatabaseConfig
    Mail     MailConfig
    Stripe   StripeConfig
}

type ServerConfig struct {
    Host string `env:"HOST" envDefault:"0.0.0.0"`
    Port int    `env:"PORT" envDefault:"8080"`
}

type DatabaseConfig struct {
    URL      string `env:"DATABASE_URL,required"`
    MaxConns int    `env:"DB_MAX_CONNS" envDefault:"10"`
}

func Load() (*Config, error) {
    cfg := &Config{}
    return cfg, env.Parse(cfg)
}
```

**Files:**

| File               | Purpose                             |
| ------------------ | ----------------------------------- |
| `.env.example`     | Documents all env vars (checked in) |
| `.env`             | Local overrides (gitignored)        |
| `config/config.go` | Struct definitions + `Load()`       |

Taskfile loads `.env` automatically via `dotenv:` option — no `godotenv` in Go code.

## Dev Tools

### Taskfile

```yaml
version: "3"
dotenv: [".env"]

tasks:
    dev:
        desc: Run with hot reload
        cmd: air

    build:
        desc: Build binary
        cmd: go build -o bin/app ./cmd/app

    test:
        desc: Run tests
        cmd: go test -race -cover ./...

    lint:
        desc: Run linters
        cmd: golangci-lint run

    sqlc:
        desc: Generate sqlc code
        cmd: sqlc generate

    migrate:
        desc: Run migrations
        cmd: goose -dir db/migrations postgres $DATABASE_URL up

    migrate:new:
        desc: Create new migration
        cmd: goose -dir db/migrations create {{.CLI_ARGS}} sql

    docker:up:
        desc: Start local services
        cmd: docker compose up -d

    docker:down:
        desc: Stop local services
        cmd: docker compose down
```

### Docker Compose

```yaml
services:
    postgres:
        image: postgres:18-alpine
        environment:
            POSTGRES_USER: postgres
            POSTGRES_PASSWORD: postgres
            POSTGRES_DB: app_dev
        ports:
            - "5432:5432"
        volumes:
            - postgres_data:/var/lib/postgresql/data

    mailpit:
        image: axllent/mailpit
        ports:
            - "1025:1025" # SMTP
            - "8025:8025" # Web UI

volumes:
    postgres_data:
```

### Air (Hot Reload)

```toml
[build]
cmd = "go build -o ./tmp/app ./cmd/app"
bin = "./tmp/app"
include_ext = ["go", "templ"]
exclude_dir = ["tmp", "node_modules", "web/static"]

[log]
time = false

[misc]
clean_on_exit = true
```

## Components

### HTTP Handlers

Handlers implement `Routes(Router)` to declare their routes:

```go
type AuthHandler struct {
    repo  *repository.Queries
    queue jobs.Queue
}

func (h *AuthHandler) Routes(r web.Router) {
    r.GET("/login", h.LoginPage)
    r.POST("/login", h.Login)
    r.POST("/logout", h.Logout, web.RequireAuth)
}

func (h *AuthHandler) Login(c web.Context) error {
    // Handle login, return error or redirect
}
```

### Background Tasks

Powered by [River](https://riverqueue.com/) — Postgres-native queue with transactional guarantees.

**Why River:**

- No extra infrastructure (uses your existing Postgres)
- Transactional enqueueing — task inserted atomically with your data
- Failed tasks kept 7 days for inspection, then auto-cleaned
- Built-in retries with exponential backoff

Type-safe payloads with automatic marshaling:

```go
type SendWelcomePayload struct {
    UserID string `json:"user_id"`
}

type SendWelcome struct {
    repo   *repository.Queries
    mailer mail.Mailer
}

func (t *SendWelcome) Name() string { return "send_welcome" }

func (t *SendWelcome) Handle(ctx context.Context, p SendWelcomePayload) error {
    user, _ := t.repo.GetUserByID(ctx, p.UserID)
    return t.mailer.Send(ctx, "welcome", user.Email, user)
}
```

Enqueue from handlers (transactional):

```go
// Task only exists if DB transaction commits
task.EnqueueTx(tx, "send_welcome", SendWelcomePayload{UserID: user.ID})
```

### Scheduled Tasks

Periodic tasks powered by River. Different interface — no payload:

```go
type CleanupSessions struct {
    repo *repository.Queries
}

func (t *CleanupSessions) Schedule() string { return "0 * * * *" }  // Every hour

func (t *CleanupSessions) Handle(ctx context.Context) error {  // No payload
    return t.repo.DeleteExpiredSessions(ctx)
}
```

Both task types use River — one queue system, one dashboard, same retry logic.

### Middleware

Standard `func(next) next` pattern, uses your repo types:

```go
func LoadTenant(repo *repository.Queries) web.Middleware {
    return func(next web.HandlerFunc) web.HandlerFunc {
        return func(c web.Context) error {
            slug := extractSlug(c.Request())
            tenant, _ := repo.GetTenantBySlug(c.Context(), slug)
            c.Set("tenant", tenant)
            return next(c)
        }
    }
}
```

### Services (Manual, Not Generated)

Extract to `internal/services/` only when you have shared logic between handlers and tasks:

```go
// internal/services/user.go
func CreateUser(ctx context.Context, q *repository.Queries, input RegisterInput) (*repository.User, error) {
    hash, _ := auth.HashPassword(input.Password)
    return q.CreateUser(ctx, repository.CreateUserParams{
        Email:        input.Email,
        PasswordHash: hash,
    })
}
```

## CLI Workflow

```bash
# Initialize project with config file
forge init myapp --module=github.com/you/myapp
cd myapp

# Edit forge.yaml to enable features
vim forge.yaml

# Generate all code
forge generate

# Start local services (Postgres, Mailpit)
task docker:up

# Setup database
task sqlc
task migrate

# Run development server (hot reload)
task dev
```

### Incremental Generation

```bash
forge g model post title:string body:text user:belongs_to
forge g handler posts
forge g task send_notification
forge g task cleanup_files --schedule="0 2 * * *"
```

## Pre-built Features

Enable via `forge.yaml`:

| Feature    | What You Get                                                           |
| ---------- | ---------------------------------------------------------------------- |
| `auth`     | User registration, login, password reset, email verification, sessions |
| `tenants`  | Multi-tenancy, team members, roles, invitations                        |
| `billing`  | Stripe integration, subscriptions, webhooks, customer portal           |
| `api_keys` | API key generation, authentication, scopes                             |
| `webhooks` | Outgoing webhooks, delivery tracking, retries                          |

### Presets

```bash
forge init myapp --preset=b2b-saas  # auth + tenants + billing
forge init myapp --preset=minimal   # auth only
```

## Escape Hatches

### Add Custom Middleware

```go
app.Run(
    app.WithMiddleware(myMiddleware),
    app.WithMiddlewareBefore("auth", tenantResolver),
)
```

### Access Router Directly

```go
a := app.New(opts...)
a.Router().Mount("/legacy", legacyHandler)
a.Run()
```

### Use Packages Individually

```go
// Don't use app.Run(), build your own
router := chi.NewRouter()
router.Use(web.Logger, web.Recoverer)
// ...
```

## Design Principles

1. **No `internal/models/`** — use sqlc-generated types directly
2. **No `internal/services/`** — logic lives in handlers, extract manually when needed
3. **No repository interfaces** — sqlc generates concrete types
4. **No service containers** — constructor injection, explicit wiring
5. **No reflection** — all registration is explicit
6. **Generated code is yours** — modify, delete, extend freely

## File Ownership

| Location                | Owner     | Regenerate Safe?                  |
| ----------------------- | --------- | --------------------------------- |
| `forge/pkg/*`           | Framework | N/A (imported)                    |
| `internal/repository/*` | sqlc      | Yes (regenerate with `task sqlc`) |
| `internal/handlers/*`   | You       | No (your modifications preserved) |
| `config/config.go`      | You       | No                                |
| `db/migrations/*`       | You       | No                                |
| `db/queries/*`          | You       | No                                |
| `Taskfile.yml`          | Generated | Yes (can customize)               |
| `docker-compose.yml`    | Generated | Yes (can customize)               |
| `.air.toml`             | Generated | Yes (can customize)               |

## Status

**Concept stage** — this document describes the planned architecture.

## Inspiration

- [Loco](https://loco.rs/) (Rust) — Rails-like framework
- [Buffalo](https://gobuffalo.io/) (Go) — web framework with generators
- [Rails](https://rubyonrails.org/) — convention over configurations
