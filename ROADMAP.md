# Forge Framework Roadmap

This document outlines planned features for the Forge framework.

**Current Status:** Concept stage — core architecture documented, foundational packages implemented (`binder`, `validator`, `sanitizer`, `htmx`, `session`, `cookie`, `db`, `logger`, `health`, `hostrouter`, `id`).

---

## Priority Legend

- **High** — Core features needed for MVP micro-SaaS applications
- **Medium** — Important features that enhance developer experience
- **Future** — Nice-to-have features for later consideration

---

## High Priority

### [ ] Jobs/Queue

**Package:** `pkg/jobs/`

Background job processing with River (Postgres-native queue).

**Design principles:**

- No River leaks outside wrapper (internal implementation detail)
- Registration via options pattern (like HTTP handlers)
- Automatic payload marshal/unmarshal

**Scheduled Jobs:**

```go
// Handler signature - context only
type ScheduledHandler func(ctx context.Context) error

// Registration via app options
forge.WithScheduledJob("@daily", cleanup.Handler)
forge.WithScheduledJob("*/5 * * * *", metrics.Collect)
```

**One-time Tasks:**

```go
// Handler signature - context + typed payload
type TaskHandler[T any] func(ctx context.Context, payload T) error

// Registration via app options (auto-registers by payload type name)
forge.WithTaskHandler(email.SendWelcome)  // uses fmt.Sprintf("%T", WelcomePayload{})

// Enqueue via context
c.EnqueueTask(WelcomePayload{UserID: "123", Email: "user@example.com"})
```

**Payload auto-registration:**

- Handler registered by `fmt.Sprintf("%T", Payload{})`
- Example: `email.WelcomePayload` registers as `"email.WelcomePayload"`

---

### [ ] Mailer

**Package:** `pkg/mailer/`

Email sending with provider abstraction.

**Design:**

- Resend as initial provider (adapter pattern for future extensibility)
- Sync or async depends on adapter initialization
- Email handlers registered by payload type (like tasks)

**Provider interface:**

```go
type Provider interface {
    Send(ctx context.Context, msg Message) error
}

// Resend implementation
func NewResendProvider(apiKey string) Provider
```

**App initialization determines sync/async:**

```go
// Sync - direct send
forge.WithMailer(mailer.NewResendProvider(apiKey))

// Async - via queue decorator
forge.WithMailer(mailer.WithQueue(resendProvider, jobsClient))
```

**Email handler pattern:**

```go
// Handler registration by payload type
forge.WithEmailHandler(sendWelcomeEmail)  // auto-registers "WelcomeEmail"

// Handler signature - receives recipient + payload for template data
func sendWelcomeEmail(ctx context.Context, to string, payload WelcomeEmail) (mailer.Message, error) {
    return mailer.Message{
        To:      to,
        Subject: "Welcome to Our App!",
        HTML:    renderWelcomeTemplate(payload),
    }, nil
}

// Send via context - recipient explicit, payload for template
c.SendEmail("user@mail.dev", WelcomeEmail{Name: "John", Plan: "Pro"})
```

**Design rationale:**

- Recipient passed separately (explicit)
- Payload struct = template data only
- Handler owns: subject, template selection, message building

---

## Medium Priority

### [ ] Storage

**Package:** `pkg/storage/`

File storage with S3-compatible backends.

**Design:**

- S3-compatible only (works with AWS S3, MinIO, Cloudflare R2, etc.)
- Returns file info after upload
- ACL support (private, public-read)

```go
type Storage interface {
    Put(ctx context.Context, key string, r io.Reader, opts ...PutOption) (*FileInfo, error)
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    URL(key string, ttl time.Duration) (string, error)  // presigned URL
}

type FileInfo struct {
    Key         string
    Size        int64
    ContentType string
    URL         string
    ACL         ACL
}

type ACL string

const (
    ACLPrivate ACL = "private"
    ACLPublic  ACL = "public-read"
)

// Put options
WithACL(acl ACL) PutOption
WithContentType(ct string) PutOption
```

---

### [ ] Host Router Extensions

**Package:** `pkg/hostrouter/`

Additional helpers for multi-domain routing and subdomain handling:

```go
// Get full domain from request
GetDomain(r *http.Request) string        // "app.example.com"

// Get subdomain from request
GetSubdomain(r *http.Request) string     // "app" from "app.example.com"

// Build full domain URL
BuildURL(subdomain, path string) string  // "https://app.example.com/path"

// Set subdomain in context for tenant identification
WithSubdomain(subdomain string) // middleware or context helper
```

---

## Future

_Features to consider after core functionality is stable._

- WebSocket support
- Rate limiting middleware
- Caching layer (Redis adapter)
- Feature flags
- Audit logging
- Multi-tenant database isolation patterns
