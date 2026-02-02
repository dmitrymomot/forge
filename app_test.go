package forge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge"
)

// testHandler is a simple handler for testing.
type testHandler struct {
	message string
}

func (h *testHandler) Routes(r forge.Router) {
	r.GET("/", h.index)
	r.GET("/json", h.jsonResponse)
	r.GET("/user/{id}", h.getUser)
	r.POST("/echo", h.echo)
	r.Route("/api", func(r forge.Router) {
		r.GET("/health", h.health)
	})
}

func (h *testHandler) index(c forge.Context) error {
	return c.String(http.StatusOK, h.message)
}

func (h *testHandler) jsonResponse(c forge.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *testHandler) getUser(c forge.Context) error {
	id := c.Param("id")
	return c.JSON(http.StatusOK, map[string]string{"id": id})
}

func (h *testHandler) echo(c forge.Context) error {
	body, _ := io.ReadAll(c.Request().Body)
	return c.String(http.StatusOK, string(body))
}

func (h *testHandler) health(c forge.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "healthy"})
}

// testMiddleware adds a header to all responses.
func testMiddleware(headerName, headerValue string) forge.Middleware {
	return func(next forge.HandlerFunc) forge.HandlerFunc {
		return func(c forge.Context) error {
			c.SetHeader(headerName, headerValue)
			return next(c)
		}
	}
}

func TestNew(t *testing.T) {
	app := forge.New()
	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewWithOptions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := forge.New(
		forge.WithLogger(logger),
		forge.WithAddress(":9090"),
		forge.WithShutdownTimeout(10*time.Second),
	)
	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestHandler(t *testing.T) {
	app := forge.New(
		forge.WithHandlers(&testHandler{message: "hello"}),
	)

	// Create a test server using the app's router
	// Since we can't access the router directly, we test via Run
	// For unit tests, we'll verify the handler interface

	h := &testHandler{message: "hello"}
	var routesCalled bool
	mockRouter := &mockRouter{onGet: func(path string, _ forge.HandlerFunc, _ ...forge.Middleware) {
		routesCalled = true
	}}
	h.Routes(mockRouter)

	if !routesCalled {
		t.Error("Routes was not called")
	}
	_ = app // ensure app compiles
}

func TestMiddleware(t *testing.T) {
	var called bool
	mw := func(next forge.HandlerFunc) forge.HandlerFunc {
		return func(c forge.Context) error {
			called = true
			return next(c)
		}
	}

	handler := func(c forge.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	wrapped := mw(handler)

	// Create mock context
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &mockContext{w: w, r: r}

	err := wrapped(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("middleware was not called")
	}
}

func TestShutdownHook(t *testing.T) {
	var hookCalled atomic.Bool

	app := forge.New(
		forge.WithAddress(":0"), // random port
		forge.WithShutdownHook(func(ctx context.Context) error {
			hookCalled.Store(true)
			return nil
		}),
		forge.WithShutdownTimeout(1*time.Second),
	)

	// Start the app in a goroutine
	done := make(chan error, 1)
	go func() {
		done <- app.Run()
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop it
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Wait for Run to return
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Run to complete")
	}

	if !hookCalled.Load() {
		t.Error("shutdown hook was not called")
	}
}

func TestContextMethods(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test?foo=bar", nil)
	r.Header.Set("X-Custom", "custom-value")

	c := &mockContext{w: w, r: r}

	// Test Query
	if got := c.Query("foo"); got != "bar" {
		t.Errorf("Query('foo') = %q, want %q", got, "bar")
	}

	// Test Header
	if got := c.Header("X-Custom"); got != "custom-value" {
		t.Errorf("Header('X-Custom') = %q, want %q", got, "custom-value")
	}

	// Test Context
	if c.Context() == nil {
		t.Error("Context() returned nil")
	}
}

func TestContextJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &mockContext{w: w, r: r}

	data := map[string]string{"key": "value"}
	if err := c.JSON(http.StatusOK, data); err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if got["key"] != "value" {
		t.Errorf("got key = %q, want %q", got["key"], "value")
	}
}

func TestContextString(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &mockContext{w: w, r: r}

	if err := c.String(http.StatusOK, "hello world"); err != nil {
		t.Fatalf("String() error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	if got := w.Body.String(); got != "hello world" {
		t.Errorf("body = %q, want %q", got, "hello world")
	}
}

func TestContextRedirect(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &mockContext{w: w, r: r}

	if err := c.Redirect(http.StatusFound, "/new-location"); err != nil {
		t.Fatalf("Redirect() error: %v", err)
	}

	if w.Code != http.StatusFound {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusFound)
	}

	if loc := w.Header().Get("Location"); loc != "/new-location" {
		t.Errorf("Location = %q, want %q", loc, "/new-location")
	}
}

func TestContextNoContent(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &mockContext{w: w, r: r}

	if err := c.NoContent(http.StatusNoContent); err != nil {
		t.Fatalf("NoContent() error: %v", err)
	}

	if w.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusNoContent)
	}

	if w.Body.Len() != 0 {
		t.Errorf("body length = %d, want 0", w.Body.Len())
	}
}

func TestContextError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &mockContext{w: w, r: r}

	if err := c.Error(http.StatusBadRequest, "bad request"); err != nil {
		t.Fatalf("Error() error: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRouterGroup(t *testing.T) {
	var groupCalled bool
	mockRouter := &mockRouter{
		onGroup: func(fn func(forge.Router)) {
			groupCalled = true
			fn(&mockRouter{})
		},
	}

	mockRouter.Group(func(r forge.Router) {
		// group defined
	})

	if !groupCalled {
		t.Error("Group was not called")
	}
}

func TestRouterRoute(t *testing.T) {
	var routeCalled bool
	var routePattern string
	mockRouter := &mockRouter{
		onRoute: func(pattern string, fn func(forge.Router)) {
			routeCalled = true
			routePattern = pattern
			fn(&mockRouter{})
		},
	}

	mockRouter.Route("/api", func(r forge.Router) {
		// routes defined
	})

	if !routeCalled {
		t.Error("Route was not called")
	}
	if routePattern != "/api" {
		t.Errorf("Route pattern = %q, want %q", routePattern, "/api")
	}
}

// mockRouter implements forge.Router for testing.
type mockRouter struct {
	onGet     func(string, forge.HandlerFunc, ...forge.Middleware)
	onPost    func(string, forge.HandlerFunc, ...forge.Middleware)
	onPut     func(string, forge.HandlerFunc, ...forge.Middleware)
	onPatch   func(string, forge.HandlerFunc, ...forge.Middleware)
	onDelete  func(string, forge.HandlerFunc, ...forge.Middleware)
	onHead    func(string, forge.HandlerFunc, ...forge.Middleware)
	onOptions func(string, forge.HandlerFunc, ...forge.Middleware)
	onGroup   func(func(forge.Router))
	onRoute   func(string, func(forge.Router))
	onUse     func(...forge.Middleware)
	onMount   func(string, http.Handler)
}

func (m *mockRouter) GET(path string, h forge.HandlerFunc, mw ...forge.Middleware) {
	if m.onGet != nil {
		m.onGet(path, h, mw...)
	}
}
func (m *mockRouter) POST(path string, h forge.HandlerFunc, mw ...forge.Middleware) {
	if m.onPost != nil {
		m.onPost(path, h, mw...)
	}
}
func (m *mockRouter) PUT(path string, h forge.HandlerFunc, mw ...forge.Middleware) {
	if m.onPut != nil {
		m.onPut(path, h, mw...)
	}
}
func (m *mockRouter) PATCH(path string, h forge.HandlerFunc, mw ...forge.Middleware) {
	if m.onPatch != nil {
		m.onPatch(path, h, mw...)
	}
}
func (m *mockRouter) DELETE(path string, h forge.HandlerFunc, mw ...forge.Middleware) {
	if m.onDelete != nil {
		m.onDelete(path, h, mw...)
	}
}
func (m *mockRouter) HEAD(path string, h forge.HandlerFunc, mw ...forge.Middleware) {
	if m.onHead != nil {
		m.onHead(path, h, mw...)
	}
}
func (m *mockRouter) OPTIONS(path string, h forge.HandlerFunc, mw ...forge.Middleware) {
	if m.onOptions != nil {
		m.onOptions(path, h, mw...)
	}
}
func (m *mockRouter) Group(fn func(forge.Router)) {
	if m.onGroup != nil {
		m.onGroup(fn)
	}
}
func (m *mockRouter) Route(pattern string, fn func(forge.Router)) {
	if m.onRoute != nil {
		m.onRoute(pattern, fn)
	}
}
func (m *mockRouter) Use(mw ...forge.Middleware) {
	if m.onUse != nil {
		m.onUse(mw...)
	}
}
func (m *mockRouter) Mount(pattern string, h http.Handler) {
	if m.onMount != nil {
		m.onMount(pattern, h)
	}
}

// mockContext implements forge.Context for testing.
type mockContext struct {
	w http.ResponseWriter
	r *http.Request
}

func (c *mockContext) Request() *http.Request        { return c.r }
func (c *mockContext) Response() http.ResponseWriter { return c.w }
func (c *mockContext) Context() context.Context      { return c.r.Context() }
func (c *mockContext) Param(name string) string      { return "" }
func (c *mockContext) Query(name string) string      { return c.r.URL.Query().Get(name) }
func (c *mockContext) QueryDefault(name, def string) string {
	if v := c.r.URL.Query().Get(name); v != "" {
		return v
	}
	return def
}
func (c *mockContext) Header(name string) string    { return c.r.Header.Get(name) }
func (c *mockContext) SetHeader(name, value string) { c.w.Header().Set(name, value) }
func (c *mockContext) JSON(code int, v any) error {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.WriteHeader(code)
	return json.NewEncoder(c.w).Encode(v)
}
func (c *mockContext) String(code int, s string) error {
	c.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.w.WriteHeader(code)
	_, err := c.w.Write([]byte(s))
	return err
}
func (c *mockContext) NoContent(code int) error {
	c.w.WriteHeader(code)
	return nil
}
func (c *mockContext) Redirect(code int, url string) error {
	http.Redirect(c.w, c.r, url, code)
	return nil
}
func (c *mockContext) Error(code int, message string) error {
	http.Error(c.w, message, code)
	return nil
}
func (c *mockContext) IsHTMX() bool {
	return c.r.Header.Get("HX-Request") == "true"
}
func (c *mockContext) Render(code int, component forge.Component) error {
	c.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if c.IsHTMX() {
		code = http.StatusOK
	}
	c.w.WriteHeader(code)
	return component.Render(c.r.Context(), c.w)
}
func (c *mockContext) RenderPartial(code int, fullPage, partial forge.Component) error {
	if c.IsHTMX() {
		return c.Render(code, partial)
	}
	return c.Render(code, fullPage)
}
func (c *mockContext) Bind(v any) (forge.ValidationErrors, error)      { return nil, nil }
func (c *mockContext) BindQuery(v any) (forge.ValidationErrors, error) { return nil, nil }
func (c *mockContext) BindJSON(v any) (forge.ValidationErrors, error)  { return nil, nil }
func (c *mockContext) Written() bool                                   { return false }

// Integration tests

func TestIntegration(t *testing.T) {
	app := forge.New(
		forge.WithAddress(":0"), // random port
		forge.WithHandlers(&testHandler{message: "hello"}),
		forge.WithMiddleware(testMiddleware("X-Test", "test-value")),
	)

	// Start the app
	done := make(chan error, 1)
	go func() {
		done <- app.Run()
	}()

	// Wait for server to start and get address
	time.Sleep(50 * time.Millisecond)
	baseURL := "http://" + app.Addr()

	// Make requests
	t.Run("GET /", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/")
		if err != nil {
			t.Fatalf("GET / error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "hello" {
			t.Errorf("body = %q, want %q", string(body), "hello")
		}

		if got := resp.Header.Get("X-Test"); got != "test-value" {
			t.Errorf("X-Test header = %q, want %q", got, "test-value")
		}
	})

	t.Run("GET /json", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/json")
		if err != nil {
			t.Fatalf("GET /json error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var data map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("json decode error: %v", err)
		}

		if data["status"] != "ok" {
			t.Errorf("status = %q, want %q", data["status"], "ok")
		}
	})

	t.Run("GET /user/{id}", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/user/123")
		if err != nil {
			t.Fatalf("GET /user/123 error: %v", err)
		}
		defer resp.Body.Close()

		var data map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("json decode error: %v", err)
		}

		if data["id"] != "123" {
			t.Errorf("id = %q, want %q", data["id"], "123")
		}
	})

	t.Run("POST /echo", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/echo", "text/plain", bytes.NewReader([]byte("echo me")))
		if err != nil {
			t.Fatalf("POST /echo error: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "echo me" {
			t.Errorf("body = %q, want %q", string(body), "echo me")
		}
	})

	t.Run("GET /api/health", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/health")
		if err != nil {
			t.Fatalf("GET /api/health error: %v", err)
		}
		defer resp.Body.Close()

		var data map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("json decode error: %v", err)
		}

		if data["status"] != "healthy" {
			t.Errorf("status = %q, want %q", data["status"], "healthy")
		}
	})

	// Stop the app
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Wait for Run to return
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Run to complete")
	}
}
