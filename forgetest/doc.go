// Package forgetest provides a fluent testing API for forge HTTP handlers.
//
// It offers request builders, response assertions, HTML parsing, session management,
// and RBAC testing utilities backed by an in-memory session store that is safe
// for parallel tests.
//
// # Basic Usage
//
// Create a test app with NewApp, build requests with Get/Post/etc., and assert
// responses with RequireStatus, RequireRedirect, or HTML assertions:
//
//	import (
//		"testing"
//		"net/http"
//		"github.com/dmitrymomot/forge"
//		"github.com/dmitrymomot/forge/forgetest"
//	)
//
//	func TestHandler(t *testing.T) {
//		t.Parallel()
//
//		handler := func(c forge.Context) error {
//			return c.String(http.StatusOK, "Hello, World!")
//		}
//
//		app := forgetest.NewApp(t, handler)
//		resp := forgetest.Get(t, app, "/").Do()
//		resp.RequireStatus(t, http.StatusOK)
//
//		if resp.Body() != "Hello, World!" {
//			t.Errorf("unexpected body: %s", resp.Body())
//		}
//	}
//
// # Sessions and RBAC
//
// Use AsUser to create a session with a user ID, WithRole to set the role,
// and WithSessionData to store arbitrary session data. WithRoles configures
// the app with RBAC permissions:
//
//	func TestProtectedEndpoint(t *testing.T) {
//		t.Parallel()
//
//		handler := func(c forge.Context) error {
//			if !c.Can("posts:write") {
//				return c.NoContent(http.StatusForbidden)
//			}
//			return c.String(http.StatusOK, "Post created")
//		}
//
//		app := forgetest.NewApp(t, handler,
//			forgetest.WithRoles(forge.RolePermissions{
//				"admin":  {"posts:write"},
//				"viewer": {},
//			}),
//		)
//
//		// Admin user with permission.
//		resp := forgetest.Post(t, app, "/posts").
//			AsUser("user-123").
//			WithRole("admin").
//			Do()
//		resp.RequireStatus(t, http.StatusOK)
//
//		// Viewer without permission.
//		resp = forgetest.Post(t, app, "/posts").
//			AsUser("user-456").
//			WithRole("viewer").
//			Do()
//		resp.RequireStatus(t, http.StatusForbidden)
//	}
//
// # HTMX Support
//
// Use WithHTMX to mark a request as an HTMX request, and assert HTMX response
// headers with RequireHTMXTrigger, RequireHTMXRetarget, RequireHTMXReswap, etc.:
//
//	import (
//		"github.com/dmitrymomot/forge/pkg/htmx"
//	)
//
//	func TestHTMXPartial(t *testing.T) {
//		t.Parallel()
//
//		handler := func(c forge.Context) error {
//			partial := &forgetest.MockComponent{HTML: "<div>Partial content</div>"}
//			fullPage := &forgetest.MockComponent{HTML: "<html><body>Full page</body></html>"}
//
//			if c.IsHTMX() {
//				return c.Render(http.StatusOK, partial, htmx.WithTrigger("notification"))
//			}
//			return c.Render(http.StatusOK, fullPage)
//		}
//
//		app := forgetest.NewApp(t, handler)
//
//		// HTMX request gets partial content.
//		resp := forgetest.Get(t, app, "/partial").WithHTMX().Do()
//		resp.RequireStatus(t, http.StatusOK)
//		resp.RequireHTMXTrigger(t, "notification")
//		resp.HTML().RequireText(t, "div", "Partial content")
//	}
//
// # HTML Assertions
//
// Parse response HTML with resp.HTML() and assert element existence, text content,
// attributes, and counts using CSS selectors:
//
//	func TestHTMLRendering(t *testing.T) {
//		t.Parallel()
//
//		comp := &forgetest.MockComponent{
//			HTML: `<html>
//				<head><title>Dashboard</title></head>
//				<body>
//					<h1>Dashboard</h1>
//					<form action="/submit" method="post">
//						<input type="text" name="username" value="alice" />
//						<button type="submit">Submit</button>
//					</form>
//					<ul class="items">
//						<li>Item 1</li>
//						<li>Item 2</li>
//						<li>Item 3</li>
//					</ul>
//				</body>
//			</html>`,
//		}
//
//		handler := func(c forge.Context) error {
//			return c.Render(http.StatusOK, comp)
//		}
//
//		app := forgetest.NewApp(t, handler)
//		doc := forgetest.Get(t, app, "/").Do().HTML()
//
//		doc.RequireText(t, "h1", "Dashboard")
//		doc.RequireExactText(t, "title", "Dashboard")
//		doc.RequireAttr(t, "form", "action", "/submit")
//		doc.RequireValue(t, "input[name=username]", "alice")
//		doc.RequireCount(t, "ul.items li", 3)
//		doc.RequireExists(t, "button[type=submit]")
//		doc.RequireNotExists(t, ".error-message")
//	}
//
// # Form and JSON Requests
//
// Build POST requests with form data using WithForm, or JSON bodies using WithJSON:
//
//	func TestFormSubmission(t *testing.T) {
//		t.Parallel()
//
//		handler := func(c forge.Context) error {
//			username := c.Form("username")
//			if username == "" {
//				return c.NoContent(http.StatusBadRequest)
//			}
//			return c.Redirect(http.StatusSeeOther, "/dashboard")
//		}
//
//		app := forgetest.NewApp(t, handler)
//
//		resp := forgetest.Post(t, app, "/login").
//			WithForm("username", "alice").
//			WithForm("password", "secret").
//			Do()
//		resp.RequireRedirect(t, http.StatusSeeOther, "/dashboard")
//	}
//
//	func TestJSONAPI(t *testing.T) {
//		t.Parallel()
//
//		handler := func(c forge.Context) error {
//			var req struct {
//				Name string `json:"name"`
//			}
//			if _, err := c.Bind(&req); err != nil {
//				return c.NoContent(http.StatusBadRequest)
//			}
//			return c.JSON(http.StatusOK, map[string]string{"message": "Hello, " + req.Name})
//		}
//
//		app := forgetest.NewApp(t, handler)
//
//		resp := forgetest.Post(t, app, "/api/greet").
//			WithJSON(map[string]string{"name": "Alice"}).
//			Do()
//		resp.RequireStatus(t, http.StatusOK)
//		resp.RequireHeader(t, "Content-Type", "application/json")
//	}
//
// # Session Store Access
//
// Access the in-memory session store directly for advanced assertions:
//
//	import (
//		"context"
//	)
//
//	func TestSessionManagement(t *testing.T) {
//		t.Parallel()
//
//		handler := func(c forge.Context) error {
//			return c.String(http.StatusOK, "OK")
//		}
//
//		app := forgetest.NewApp(t, handler)
//
//		// Make request as user.
//		forgetest.Get(t, app, "/").AsUser("user-123").Do()
//
//		// Verify session was created.
//		count, _ := app.Store().CountByUserID(context.Background(), "user-123")
//		if count != 1 {
//			t.Errorf("expected 1 session, got %d", count)
//		}
//	}
//
// # Mock Components
//
// MockComponent renders static HTML or returns errors for testing component rendering:
//
//	func TestComponentRendering(t *testing.T) {
//		t.Parallel()
//
//		mock := &forgetest.MockComponent{
//			HTML: "<div>Mocked content</div>",
//		}
//
//		handler := func(c forge.Context) error {
//			return c.Render(http.StatusOK, mock)
//		}
//
//		app := forgetest.NewApp(t, handler)
//		resp := forgetest.Get(t, app, "/").Do()
//		resp.RequireStatus(t, http.StatusOK)
//		resp.HTML().RequireText(t, "div", "Mocked content")
//	}
package forgetest
