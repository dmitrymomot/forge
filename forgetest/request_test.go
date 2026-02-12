package forgetest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
)

// testAllMethodsHandler is a handler that supports all HTTP methods.
type testAllMethodsHandler struct {
	fn forge.HandlerFunc
}

func (h *testAllMethodsHandler) Routes(r forge.Router) {
	r.GET("/test", h.fn)
	r.POST("/test", h.fn)
	r.PUT("/test", h.fn)
	r.DELETE("/test", h.fn)
	r.PATCH("/test", h.fn)
}

// testPostHandler wraps a HandlerFunc to implement forge.Handler for POST routes.
type testPostHandler struct {
	fn forge.HandlerFunc
}

func (h *testPostHandler) Routes(r forge.Router) {
	r.POST("/test", h.fn)
}

func TestGet_SimpleRequest(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			return c.String(http.StatusOK, "GET response")
		},
	}

	app := NewApp(t, handler)

	resp := Get(t, app, "/test").Do()

	resp.RequireStatus(t, http.StatusOK)
	require.Equal(t, "GET response", resp.Body())
}

func TestPost_WithForm(t *testing.T) {
	t.Parallel()

	handler := &testPostHandler{
		fn: func(c forge.Context) error {
			name := c.Form("name")
			email := c.Form("email")
			return c.String(http.StatusOK, "name="+name+",email="+email)
		},
	}

	app := NewApp(t, handler)

	resp := Post(t, app, "/test").
		WithForm("name", "John Doe").
		WithForm("email", "john@example.com").
		Do()

	resp.RequireStatus(t, http.StatusOK)
	require.Equal(t, "name=John Doe,email=john@example.com", resp.Body())
}

func TestPost_WithJSON(t *testing.T) {
	t.Parallel()

	type requestBody struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	handler := &testPostHandler{
		fn: func(c forge.Context) error {
			var body requestBody
			_, err := c.BindJSON(&body)
			if err != nil {
				return err
			}
			return c.String(http.StatusOK, "name="+body.Name+",email="+body.Email)
		},
	}

	app := NewApp(t, handler)

	resp := Post(t, app, "/test").
		WithJSON(requestBody{Name: "Jane Doe", Email: "jane@example.com"}).
		Do()

	resp.RequireStatus(t, http.StatusOK)
	require.Equal(t, "name=Jane Doe,email=jane@example.com", resp.Body())
}

func TestRequest_AsUser(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			userID := c.UserID()
			isAuth := c.IsAuthenticated()

			if isAuth {
				return c.String(http.StatusOK, "authenticated:"+userID)
			}
			return c.String(http.StatusOK, "not authenticated")
		},
	}

	app := NewApp(t, handler)

	t.Run("authenticated user", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-123").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "authenticated:user-123", resp.Body())
	})

	t.Run("no user", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "not authenticated", resp.Body())
	})
}

func TestRequest_WithRole(t *testing.T) {
	t.Parallel()

	roles := forge.RolePermissions{
		"editor": {"posts:create", "posts:edit"},
		"viewer": {"posts:view"},
	}

	handler := &testHandler{
		fn: func(c forge.Context) error {
			canEdit := c.Can("posts:edit")
			canView := c.Can("posts:view")

			if canEdit {
				return c.String(http.StatusOK, "editor")
			}
			if canView {
				return c.String(http.StatusOK, "viewer")
			}
			return c.String(http.StatusOK, "none")
		},
	}

	app := NewApp(t, handler, WithRoles(roles))

	t.Run("editor role", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-123").
			WithRole("editor").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "editor", resp.Body())
	})

	t.Run("viewer role", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-456").
			WithRole("viewer").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "viewer", resp.Body())
	})
}

func TestRequest_WithSessionData(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			val, err := c.SessionValue("custom_key")
			if err != nil {
				return c.String(http.StatusOK, "no value")
			}
			if val == nil {
				return c.String(http.StatusOK, "no value")
			}
			str, ok := val.(string)
			if !ok {
				return c.String(http.StatusOK, "wrong type")
			}
			return c.String(http.StatusOK, "value="+str)
		},
	}

	app := NewApp(t, handler)

	t.Run("with session data", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-123").
			WithSessionData("custom_key", "custom_value").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "value=custom_value", resp.Body())
	})

	t.Run("without session data", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			AsUser("user-456").
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "no value", resp.Body())
	})
}

func TestRequest_WithHTMX(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			isHTMX := c.IsHTMX()
			if isHTMX {
				return c.String(http.StatusOK, "htmx request")
			}
			return c.String(http.StatusOK, "regular request")
		},
	}

	app := NewApp(t, handler)

	t.Run("HTMX request", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			WithHTMX().
			Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "htmx request", resp.Body())
	})

	t.Run("regular request", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").Do()

		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "regular request", resp.Body())
	})
}

func TestRequest_WithHeader(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			customHeader := c.Header("X-Custom-Header")
			return c.String(http.StatusOK, "header="+customHeader)
		},
	}

	app := NewApp(t, handler)

	resp := Get(t, app, "/test").
		WithHeader("X-Custom-Header", "custom-value").
		Do()

	resp.RequireStatus(t, http.StatusOK)
	require.Equal(t, "header=custom-value", resp.Body())
}

func TestRequest_WithCookie(t *testing.T) {
	t.Parallel()

	handler := &testHandler{
		fn: func(c forge.Context) error {
			cookieVal, err := c.Cookie("test_cookie")
			if err != nil {
				return c.String(http.StatusOK, "no cookie")
			}
			return c.String(http.StatusOK, "cookie="+cookieVal)
		},
	}

	app := NewApp(t, handler)

	resp := Get(t, app, "/test").
		WithCookie("test_cookie", "cookie-value").
		Do()

	resp.RequireStatus(t, http.StatusOK)
	require.Equal(t, "cookie=cookie-value", resp.Body())
}

func TestRequest_RenderHTML(t *testing.T) {
	t.Parallel()

	comp := &MockComponent{
		HTML: "<html><body><h1>Test Page</h1><div class='content'>Hello World</div></body></html>",
	}

	handler := &testHandler{
		fn: func(c forge.Context) error {
			return c.Render(http.StatusOK, comp)
		},
	}

	app := NewApp(t, handler)

	resp := Get(t, app, "/test").Do()

	resp.RequireStatus(t, http.StatusOK)

	doc := resp.HTML()
	doc.RequireExists(t, "h1")
	doc.RequireText(t, "h1", "Test Page")
	doc.RequireText(t, ".content", "Hello World")
}

func TestRequest_RenderPartial_HTMX(t *testing.T) {
	t.Parallel()

	fullPage := &MockComponent{
		HTML: "<html><body><h1>Full Page</h1><div id='content'>Full content</div></body></html>",
	}

	partial := &MockComponent{
		HTML: "<div id='content'>Partial content</div>",
	}

	handler := &testHandler{
		fn: func(c forge.Context) error {
			return c.RenderPartial(http.StatusOK, fullPage, partial)
		},
	}

	app := NewApp(t, handler)

	t.Run("HTMX request gets partial", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").
			WithHTMX().
			Do()

		// HTMX requests always get 200 for swapping
		resp.RequireStatus(t, http.StatusOK)

		// Check raw body to verify it's actually the partial
		body := resp.Body()
		require.Equal(t, "<div id='content'>Partial content</div>", body)

		// The HTML parser will still parse and add wrapper tags, so we check the content exists
		doc := resp.HTML()
		doc.RequireText(t, "#content", "Partial content")
		// The h1 from full page should NOT be in the partial
		doc.RequireNotExists(t, "h1")
	})

	t.Run("regular request gets full page", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").Do()

		resp.RequireStatus(t, http.StatusOK)

		doc := resp.HTML()
		doc.RequireExists(t, "html")
		doc.RequireExists(t, "body")
		doc.RequireText(t, "h1", "Full Page")
		doc.RequireText(t, "#content", "Full content")
	})
}

func TestRequest_AllMethods(t *testing.T) {
	t.Parallel()

	handlerFn := func(c forge.Context) error {
		method := c.Request().Method
		return c.String(http.StatusOK, "method="+method)
	}

	app := NewApp(t, &testAllMethodsHandler{fn: handlerFn})

	t.Run("GET", func(t *testing.T) {
		t.Parallel()
		resp := Get(t, app, "/test").Do()
		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "method=GET", resp.Body())
	})

	t.Run("POST", func(t *testing.T) {
		t.Parallel()
		resp := Post(t, app, "/test").Do()
		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "method=POST", resp.Body())
	})

	t.Run("PUT", func(t *testing.T) {
		t.Parallel()
		resp := Put(t, app, "/test").Do()
		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "method=PUT", resp.Body())
	})

	t.Run("DELETE", func(t *testing.T) {
		t.Parallel()
		resp := Delete(t, app, "/test").Do()
		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "method=DELETE", resp.Body())
	})

	t.Run("PATCH", func(t *testing.T) {
		t.Parallel()
		resp := Patch(t, app, "/test").Do()
		resp.RequireStatus(t, http.StatusOK)
		require.Equal(t, "method=PATCH", resp.Body())
	})
}

func TestRequest_WithRoleWithoutAsUser(t *testing.T) {
	t.Parallel()

	roles := forge.RolePermissions{
		"admin": {"users:delete"},
	}

	handler := &testHandler{
		fn: func(c forge.Context) error {
			// Without AsUser, no session is created, so Can() should return false.
			_ = c.Can("users:delete")
			_ = c.IsAuthenticated()

			return c.String(http.StatusOK, "auth=false,can=false")
		},
	}

	app := NewApp(t, handler, WithRoles(roles))

	// WithRole without AsUser should be a no-op.
	resp := Get(t, app, "/test").
		WithRole("admin").
		Do()

	resp.RequireStatus(t, http.StatusOK)
	// Should not crash and should not have permissions
	require.Equal(t, "auth=false,can=false", resp.Body())
}

func TestRequest_MultipleFormValues(t *testing.T) {
	t.Parallel()

	handler := &testPostHandler{
		fn: func(c forge.Context) error {
			name := c.Form("name")
			email := c.Form("email")
			age := c.Form("age")

			return c.JSON(http.StatusOK, map[string]string{
				"name":  name,
				"email": email,
				"age":   age,
			})
		},
	}

	app := NewApp(t, handler)

	resp := Post(t, app, "/test").
		WithForm("name", "Alice").
		WithForm("email", "alice@example.com").
		WithForm("age", "30").
		Do()

	resp.RequireStatus(t, http.StatusOK)

	var result map[string]string
	err := json.Unmarshal([]byte(resp.Body()), &result)
	require.NoError(t, err)
	require.Equal(t, "Alice", result["name"])
	require.Equal(t, "alice@example.com", result["email"])
	require.Equal(t, "30", result["age"])
}
