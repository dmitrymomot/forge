# forgetest

Fluent testing API for Forge HTTP handlers with request builders, response assertions, HTML parsing, session management, and RBAC testing.

## Installation

```bash
go get github.com/dmitrymomot/forge/forgetest
```

## Usage

Create a test app with `NewApp`, build requests with `Get`/`Post`/etc., and assert responses:

```go
package example_test

import (
	"net/http"
	"testing"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/forgetest"
)

type Handler struct{}

func (h *Handler) Routes(r forge.Router) {
	r.GET("/", h.index)
}

func (h *Handler) index(c forge.Context) error {
	return c.String(http.StatusOK, "Hello, World!")
}

func TestHandler(t *testing.T) {
	t.Parallel()

	app := forgetest.NewApp(t, &Handler{})
	resp := forgetest.Get(t, app, "/").Do()
	resp.RequireStatus(t, http.StatusOK)

	if resp.Body() != "Hello, World!" {
		t.Errorf("unexpected body: %s", resp.Body())
	}
}
```

## Common Operations

### Form Submission

```go
func TestLogin(t *testing.T) {
	t.Parallel()

	app := forgetest.NewApp(t, &LoginHandler{})
	resp := forgetest.Post(t, app, "/login").
		WithForm("username", "alice").
		WithForm("password", "secret").
		Do()
	resp.RequireRedirect(t, http.StatusSeeOther, "/dashboard")
}
```

### JSON API

```go
func TestAPI(t *testing.T) {
	t.Parallel()

	app := forgetest.NewApp(t, &APIHandler{})
	resp := forgetest.Post(t, app, "/api/greet").
		WithJSON(map[string]string{"name": "Alice"}).
		Do()
	resp.RequireStatus(t, http.StatusOK)
	resp.RequireHeader(t, "Content-Type", "application/json")
}
```

### Authenticated Requests

```go
func TestProtected(t *testing.T) {
	t.Parallel()

	app := forgetest.NewApp(t, &ProtectedHandler{})
	resp := forgetest.Get(t, app, "/dashboard").
		AsUser("user-123").
		Do()
	resp.RequireStatus(t, http.StatusOK)
}
```

### RBAC Testing

```go
func TestPermissions(t *testing.T) {
	t.Parallel()

	roles := forge.RolePermissions{
		"admin":  {"posts:write"},
		"viewer": {},
	}

	app := forgetest.NewApp(t, &PostHandler{}, forgetest.WithRoles(roles))

	// Admin succeeds
	resp := forgetest.Post(t, app, "/posts").
		AsUser("user-123").
		WithRole("admin").
		Do()
	resp.RequireStatus(t, http.StatusOK)

	// Viewer fails
	resp = forgetest.Post(t, app, "/posts").
		AsUser("user-456").
		WithRole("viewer").
		Do()
	resp.RequireStatus(t, http.StatusForbidden)
}
```

### HTML Assertions

```go
func TestHTML(t *testing.T) {
	t.Parallel()

	app := forgetest.NewApp(t, &DashboardHandler{})
	doc := forgetest.Get(t, app, "/").Do().HTML()

	doc.RequireText(t, "h1", "Dashboard")
	doc.RequireExactText(t, "title", "Dashboard")
	doc.RequireAttr(t, "form", "action", "/submit")
	doc.RequireValue(t, "input[name=username]", "alice")
	doc.RequireCount(t, "ul.items li", 3)
	doc.RequireExists(t, "button[type=submit]")
	doc.RequireNotExists(t, ".error-message")
}
```

### HTMX Testing

```go
func TestHTMX(t *testing.T) {
	t.Parallel()

	app := forgetest.NewApp(t, &HTMXHandler{})
	resp := forgetest.Get(t, app, "/partial").WithHTMX().Do()
	resp.RequireStatus(t, http.StatusOK)
	resp.RequireHTMXTrigger(t, "notification")
	resp.RequireHTMXRetarget(t, "#content")
	resp.RequireHTMXReswap(t, "innerHTML")
}
```

## API Documentation

Run `go doc -all ./forgetest` for complete API documentation.
