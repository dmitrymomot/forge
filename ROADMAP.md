# Forge Framework Roadmap

This document outlines planned features for the Forge framework.

**Current Status:** Concept stage — core architecture documented, foundational packages implemented (`binder`, `validator`, `sanitizer`, `htmx`, `session`, `cookie`, `db`, `logger`, `health`, `hostrouter`, `id`).

---

## Priority Legend

- **High** — Core features needed for MVP micro-SaaS applications
- **Medium** — Important features that enhance developer experience
- **Future** — Nice-to-have features for later consideration

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

## Future

_Features to consider after core functionality is stable._

- SSE/WebSocket support
- Rate limiting middleware
- RBAC package to manage roles and permissions
- Caching layer (Redis adapter)
- Feature flags
- Audit logging
- Middlewares collection (default like request id, ratelimiter, etc and specialized: tenant, ARBAC, API key, webhook verifier, etc)
- Services collection: profile, tenant, members, billing/subscription, auth (passwordless, email+password, oauth2, api keys)
- Add predefined task to send webhook
