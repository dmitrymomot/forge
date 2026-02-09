# Forge

[![CI](https://github.com/dmitrymomot/forge/actions/workflows/ci.yml/badge.svg)](https://github.com/dmitrymomot/forge/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dmitrymomot/forge.svg)](https://pkg.go.dev/github.com/dmitrymomot/forge)
[![Go Report Card](https://goreportcard.com/badge/github.com/dmitrymomot/forge)](https://goreportcard.com/report/github.com/dmitrymomot/forge)
[![License](https://img.shields.io/github/license/dmitrymomot/forge)](LICENSE)

A simple, opinionated Go framework for building micro-SaaS applications.

Forge is designed around the principle of "no magic" — it uses explicit, readable code with no reflection or service containers. The framework provides a thin orchestration layer while keeping business logic in plain Go handlers.

## Installation

```bash
go get github.com/dmitrymomot/forge
```

## Quick Start

```go
package main

import (
    "log"
    "github.com/dmitrymomot/forge"
)

func main() {
    app := forge.New(
        forge.AppConfig{},
        forge.WithHandlers(&MyHandler{}),
    )

    if err := forge.Run(
        forge.RunConfig{Address: ":8080"},
        forge.WithFallback(app),
    ); err != nil {
        log.Fatal(err)
    }
}

type MyHandler struct{}

func (h *MyHandler) Routes(r forge.Router) {
    r.GET("/", h.index)
}

func (h *MyHandler) index(c forge.Context) error {
    return c.JSON(200, map[string]string{"message": "Hello, World!"})
}
```

## Core Concepts

### Handlers

Handlers implement the `Handler` interface to declare routes:

```go
type AuthHandler struct {
    repo *repository.Queries
}

func NewAuth(repo *repository.Queries) *AuthHandler {
    return &AuthHandler{repo: repo}
}

func (h *AuthHandler) Routes(r forge.Router) {
    r.GET("/login", h.showLogin)
    r.POST("/login", h.handleLogin)
    r.POST("/logout", h.handleLogout)
}

func (h *AuthHandler) showLogin(c forge.Context) error {
    return c.Render(http.StatusOK, views.LoginPage())
}
```

### Context

The `Context` interface embeds `context.Context`, so it can be passed directly to any function expecting a standard library context. It also provides built-in helpers for common tasks:

```go
func (h *Handler) getUser(c forge.Context) error {
    // c satisfies context.Context — pass it to DB calls, HTTP clients, etc.
    user, err := h.repo.GetUser(c, c.UserID())
    if err != nil {
        return err
    }
    return c.JSON(200, user)
}
```

Context carries everything you need for a request — logging, cookies, flash messages, domain info:

```go
func (h *Handler) updateSettings(c forge.Context) error {
    c.LogInfo("updating settings", "user", c.UserID(), "domain", c.Domain())

    // Flash messages for post-redirect-get
    c.SetFlash("success", "Settings saved!")
    return c.Redirect(http.StatusSeeOther, "/settings")
}

func (h *Handler) showSettings(c forge.Context) error {
    var msg string
    c.Flash("success", &msg) // reads and deletes flash

    return c.Render(http.StatusOK, views.Settings(msg))
}
```

### Type-Safe Parameters

Generic helpers provide type-safe access to URL and query parameters:

```go
func (h *Handler) listItems(c forge.Context) error {
    page := forge.QueryDefault[int](c, "page", 1)
    limit := forge.QueryDefault[int](c, "limit", 20)
    id := forge.Param[int64](c, "id")

    items, err := h.repo.ListItems(c, page, limit)
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, items)
}
```

Supported types: `~string`, `~int`, `~int64`, `~float64`, `~bool`.

### Data Binding & Validation

Bind request data into structs with automatic sanitization and validation:

```go
type CreateContact struct {
    Name  string `form:"name"  validate:"required;max:100"`
    Email string `form:"email" validate:"required;email"`
    Phone string `form:"phone" sanitize:"trim;numeric"`
}

func (h *Handler) createContact(c forge.Context) error {
    var req CreateContact
    if errs, err := c.Bind(&req); err != nil {
        return err
    } else if errs != nil {
        return c.Render(http.StatusUnprocessableEntity, views.Form(errs))
    }
    // req is sanitized and validated
    return h.repo.CreateContact(c, req.Name, req.Email, req.Phone)
}
```

Also available: `c.BindJSON()` for API endpoints and `c.BindQuery()` for query parameters.

### Sessions & Authentication

Enable server-side session management with automatic creation:

```go
app := forge.New(
    forge.AppConfig{},
    forge.WithSession(postgresStore,
        forge.WithSessionTTL(7 * 24 * time.Hour),
        forge.WithMaxSessionsPerUser(3),
        forge.WithSessionFingerprint(
            forge.FingerprintCookie,
            forge.FingerprintWarn,
        ),
    ),
)
```

Authenticate users with `AuthenticateSession` — it sets the user ID, rotates the session token, and marks the session as authenticated in one call:

```go
func (h *Handler) login(c forge.Context) error {
    // ...validate credentials...
    if err := c.AuthenticateSession(user.ID); err != nil {
        return err
    }
    return c.Redirect(http.StatusSeeOther, "/dashboard")
}
```

Then use the built-in identity methods — no need to manually read session keys:

```go
func (h *Handler) dashboard(c forge.Context) error {
    if !c.IsAuthenticated() {
        return c.Redirect(http.StatusSeeOther, "/login")
    }

    // c.UserID() returns the authenticated user's ID
    user, err := h.repo.GetUser(c, c.UserID())
    if err != nil {
        return err
    }

    canEdit := c.IsCurrentUser(user.ID)
    return c.Render(http.StatusOK, views.Dashboard(user, canEdit))
}
```

For custom session data, use `SessionGet` and `SessionSet`:

```go
forge.SessionSet(c, "theme", "dark")
theme, ok := forge.SessionGet[string](c, "theme")
```

Session management:

```go
c.DestroySession()                // Logout current device
c.DestroyOtherSessions()          // Logout all other devices
c.DestroyAllSessions(c.UserID())  // Logout everywhere
sessions, _ := c.ListSessions(c.UserID()) // Show active sessions
```

### RBAC

Configure role-based access control:

```go
app := forge.New(
    forge.AppConfig{},
    forge.WithRoles(
        forge.RolePermissions{
            "admin":  {"users.read", "users.write", "billing.manage"},
            "member": {"users.read"},
        },
        func(c forge.Context) string {
            return forge.ContextValue[string](c, roleKey{})
        },
    ),
)
```

Check permissions in handlers:

```go
func (h *Handler) deleteUser(c forge.Context) error {
    if !c.Can("users.write") {
        return forge.ErrForbidden("You do not have permission")
    }
    return h.repo.DeleteUser(c, forge.Param[string](c, "id"))
}

func (h *Handler) adminPanel(c forge.Context) error {
    c.LogInfo("admin access", "role", c.Role(), "user", c.UserID())
    return c.Render(http.StatusOK, views.Admin())
}
```

### Background Jobs

Enable background job processing with River:

```go
app := forge.New(
    forge.AppConfig{},
    forge.WithJobs(pgxPool,
        forge.JobConfig{Workers: 2},
        forge.WithTask(EmailTask{}),
        forge.WithScheduledTask(CleanupTask{}),
    ),
)
```

Define tasks using structural typing:

```go
type EmailTask struct{}

func (EmailTask) Name() string { return "send_email" }

func (EmailTask) Handle(ctx context.Context, p struct{ Email string }) error {
    // Send email...
    return nil
}
```

Enqueue jobs from handlers:

```go
func (h *Handler) signup(c forge.Context) error {
    err := c.Enqueue("send_email",
        struct{ Email string }{Email: user.Email},
        forge.WithQueue("emails"),
        forge.WithScheduledIn(1*time.Minute),
    )
    if err != nil {
        return err
    }
    return c.Redirect(http.StatusSeeOther, "/signup/confirm")
}
```

### File Storage

Enable S3-compatible file storage:

```go
storage, err := forge.NewS3Storage(forge.StorageConfig{
    Endpoint:  "s3.amazonaws.com",
    AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
    SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
    Bucket:    "myapp-uploads",
    Region:    "us-east-1",
})
if err != nil {
    log.Fatal(err)
}

app := forge.New(
    forge.AppConfig{},
    forge.WithStorage(storage),
)
```

Upload, download, and manage files directly from handlers:

```go
func (h *Handler) uploadAvatar(c forge.Context) error {
    info, err := c.Upload("avatar",
        forge.WithStoragePrefix("avatars"),
        forge.WithStorageTenant(c.UserID()),
        forge.WithStorageValidation(
            forge.MaxFileSize(5*1024*1024),
            forge.ImageFilesOnly(),
        ),
    )
    if err != nil {
        return err
    }
    // Save info.Key to database, generate URLs later
    return c.JSON(http.StatusOK, map[string]string{"key": info.Key})
}

func (h *Handler) getAvatarURL(c forge.Context) error {
    url, err := c.FileURL(avatarKey, forge.WithURLExpiry(1*time.Hour))
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, map[string]string{"url": url})
}
```

### HTMX-Aware Rendering

Context automatically detects HTMX requests and renders accordingly:

```go
func (h *Handler) contacts(c forge.Context) error {
    contacts, err := h.repo.ListContacts(c)
    if err != nil {
        return err
    }

    // HTMX request → renders just the partial
    // Regular request → renders the full page with layout
    return c.RenderPartial(http.StatusOK,
        views.ContactsPage(contacts), // full page
        views.ContactsList(contacts), // partial for HTMX
    )
}
```

Check HTMX state: `c.IsHTMX()` returns `true` for HTMX-initiated requests. Redirects automatically use `HX-Redirect` headers when appropriate.

### Server-Sent Events

Stream events to clients using channels:

```go
func (h *Handler) streamEvents(c forge.Context) error {
    ch := make(chan forge.SSEEvent)
    go func() {
        defer close(ch)
        for {
            select {
            case <-c.Done():
                return
            case event := <-eventChan:
                ch <- forge.SSEString("message", event.Data)
            }
        }
    }()
    return c.SSE(ch)
}
```

### Error Handling

Return errors from handlers using convenience constructors or the context helper:

```go
func (h *Handler) getUser(c forge.Context) error {
    id := forge.Param[string](c, "id")
    user, err := h.repo.GetUser(c, id)
    if err == sql.ErrNoRows {
        return forge.ErrNotFound("User not found")
    }
    if err != nil {
        // c.Error() creates an HTTPError with the given status and message
        return c.Error(500, "Failed to fetch user", forge.WithError(err))
    }
    return c.JSON(http.StatusOK, user)
}
```

Customize error handling globally:

```go
app := forge.New(
    forge.AppConfig{},
    forge.WithErrorHandler(func(c forge.Context, err error) error {
        if httpErr := forge.AsHTTPError(err); httpErr != nil {
            return c.JSON(httpErr.StatusCode(), httpErr)
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{
            "message": "Something went wrong",
        })
    }),
)
```

## Built-In Middlewares

Import from `github.com/dmitrymomot/forge/middlewares`:

- **RequestID** — Request tracking IDs
- **Recover** — Panic recovery
- **I18n** — Internationalization and localization
- **JWT** — Token-based authentication
- **CSRF** — Cross-site request forgery protection
- **RateLimit** — Request rate limiting
- **AuditLog** — Request and action logging
- **CORS** — Cross-origin resource sharing
- **Auth** — Authentication checks
- **RBAC** — Role-based access control

## Utility Packages

Available in `github.com/dmitrymomot/forge/pkg`:

- `binder` — Request binding with validation
- `cache` — Caching utilities
- `clientip` — Client IP extraction
- `cookie` — Secure cookie management
- `db` — Database utilities
- `dnsverify` — DNS verification helpers
- `fingerprint` — Browser fingerprinting
- `geolocation` — IP geolocation
- `hostrouter` — Multi-domain routing
- `htmx` — HTMX helpers
- `i18n` — Internationalization
- `id` — ID generation (ULID, ShortID)
- `job` — Background job processing
- `jwt` — JWT utilities
- `logger` — Structured logging
- `mailer` — Email sending
- `oauth` — OAuth 2.0 helpers
- `qrcode` — QR code generation
- `randomname` — Random name generation
- `ratelimit` — Rate limiting
- `redis` — Redis utilities
- `sanitizer` — HTML sanitization
- `secrets` — Secrets management
- `slug` — URL slug generation
- `storage` — File storage
- `token` — Token generation
- `totp` — TOTP/2FA
- `useragent` — User agent parsing
- `validator` — Input validation
- `webhook` — Webhook utilities

## Configuration

Load environment variables into structs:

```go
type Config struct {
    DatabaseURL string `env:"DATABASE_URL,required"`
    Port        string `env:"PORT" envDefault:":8080"`
    Debug       bool   `env:"DEBUG"`
}

var cfg Config
if err := forge.LoadConfig(&cfg); err != nil {
    log.Fatal(err)
}
```

## Multi-Domain Routing

Compose multiple apps with host-based routing:

```go
api := forge.New(
    forge.AppConfig{BaseDomain: "acme.com"},
    forge.WithHandlers(handlers.NewAPIHandler()),
)

website := forge.New(
    forge.AppConfig{BaseDomain: "acme.com"},
    forge.WithHandlers(handlers.NewLandingHandler()),
)

if err := forge.Run(
    forge.RunConfig{Address: ":8080"},
    forge.WithDomain("api.acme.com", api),
    forge.WithDomain("*.acme.com", website),
); err != nil {
    log.Fatal(err)
}
```

## Commands

```bash
make test             # Tests with race detection + coverage
make bench            # Benchmarks with memory stats
make lint             # vet, golangci-lint, nilaway, betteralign, modernize
make fmt              # Format + organize imports
make test-integration # Docker-based integration tests
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
- All IDs generated using `pkg/id/` package exclusively

## License

Apache 2.0 — see [LICENSE](LICENSE) for details.
