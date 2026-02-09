package middlewares_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/middlewares"
)

type mockAuditStore struct {
	entries chan *forge.AuditEntry
	err     error
	delay   time.Duration
}

func (s *mockAuditStore) Log(ctx context.Context, entry *forge.AuditEntry) error {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.err != nil {
		return s.err
	}
	s.entries <- entry
	return nil
}

func waitForEntry(t *testing.T, store *mockAuditStore, timeout time.Duration) *forge.AuditEntry {
	t.Helper()
	select {
	case entry := <-store.entries:
		return entry
	case <-time.After(timeout):
		t.Fatal("timeout waiting for audit entry")
		return nil
	}
}

type logCapture struct {
	records []slog.Record
	mu      sync.Mutex
}

func (h *logCapture) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *logCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *logCapture) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *logCapture) WithGroup(_ string) slog.Handler      { return h }

func (h *logCapture) hasMessage(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

// handlerWithRouteMiddleware is a handler that applies middleware at route level
type handlerWithRouteMiddleware struct {
	handlerFunc forge.HandlerFunc
	middleware  forge.Middleware
}

func (h *handlerWithRouteMiddleware) Routes(r forge.Router) {
	r.GET("/test", h.handlerFunc, h.middleware)
}

func TestAuditLog_BasicEntry(t *testing.T) {
	t.Parallel()

	t.Run("basic request captures method, path, status code, IP, user-agent", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		req.Header.Set("User-Agent", "TestAgent/1.0")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.NotEmpty(t, entry.ID)
		require.False(t, entry.Timestamp.IsZero())
		require.Equal(t, http.MethodGet, entry.Method)
		require.Equal(t, "/test", entry.Path)
		require.Equal(t, http.StatusOK, entry.StatusCode)
		require.Equal(t, "192.168.1.1:12345", entry.IPAddress)
		require.Equal(t, "TestAgent/1.0", entry.UserAgent)
	})
}

func TestAuditLog_SuccessNoError(t *testing.T) {
	t.Parallel()

	t.Run("entry.Error is empty on handler success", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "success")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Empty(t, entry.Error)
		require.Equal(t, http.StatusOK, entry.StatusCode)
	})
}

func TestAuditLog_HandlerError(t *testing.T) {
	t.Parallel()

	t.Run("entry.Error populated when handler returns error", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		testErr := errors.New("test error")

		// Apply middleware at route level to capture handler errors
		handler := &handlerWithRouteMiddleware{
			handlerFunc: func(c forge.Context) error {
				return testErr
			},
			middleware: middlewares.AuditLog(store),
		}

		app := forge.New(forge.AppConfig{},
			forge.WithErrorHandler(testErrorHandler),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Equal(t, "test error", entry.Error)
		// Status is 200 because ResponseWriter defaults to 200 and error handler writes response
		// after audit middleware captures the entry
		require.Equal(t, http.StatusOK, entry.StatusCode)
	})
}

func TestAuditLog_StatusCodeFromHTTPError(t *testing.T) {
	t.Parallel()

	t.Run("status code captured when handler writes response", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}

		// Apply middleware at route level
		handler := &handlerWithRouteMiddleware{
			handlerFunc: func(c forge.Context) error {
				// Write response directly so status is captured
				return c.String(http.StatusNotFound, "not found")
			},
			middleware: middlewares.AuditLog(store),
		}

		app := forge.New(forge.AppConfig{},
			forge.WithErrorHandler(testErrorHandler),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Equal(t, http.StatusNotFound, entry.StatusCode)
		require.Empty(t, entry.Error) // No error since c.String returns nil
	})
}

func TestAuditLog_UserID(t *testing.T) {
	t.Parallel()

	t.Run("UserID is empty when no session configured", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Empty(t, entry.UserID)
	})
}

func TestAuditLog_RequestIDIntegration(t *testing.T) {
	t.Parallel()

	t.Run("RequestID populated when used with RequestID middleware", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(
				middlewares.RequestID(middlewares.RequestIDConfig{}),
				middlewares.AuditLog(store),
			),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.NotEmpty(t, entry.RequestID)
	})
}

func TestAuditLog_SetAuditMetadata(t *testing.T) {
	t.Parallel()

	t.Run("handler enriches metadata via SetAuditMetadata", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				middlewares.SetAuditMetadata(c, "key1", "value1")
				middlewares.SetAuditMetadata(c, "key2", "value2")
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Equal(t, "value1", entry.Metadata["key1"])
		require.Equal(t, "value2", entry.Metadata["key2"])
	})
}

func TestAuditLog_SkipFunc(t *testing.T) {
	t.Parallel()

	t.Run("skip function bypasses store.Log entirely", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store,
				middlewares.WithAuditSkipFunc(func(c forge.Context) bool {
					return c.Request().URL.Path == "/test"
				}),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		// No entry should be logged
		select {
		case <-store.entries:
			t.Fatal("expected no audit entry due to skip function")
		case <-time.After(100 * time.Millisecond):
			// Success - no entry was logged
		}
	})
}

func TestAuditLog_CustomActionFunc(t *testing.T) {
	t.Parallel()

	t.Run("custom action function overrides default HTTP method", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store,
				middlewares.WithAuditActionFunc(func(c forge.Context) string {
					return "CUSTOM_ACTION"
				}),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Equal(t, "CUSTOM_ACTION", entry.Action)
		require.Equal(t, http.MethodPost, entry.Method) // Method should still be POST
	})
}

func TestAuditLog_CustomResourceFunc(t *testing.T) {
	t.Parallel()

	t.Run("custom resource function overrides default URL path", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store,
				middlewares.WithAuditResourceFunc(func(c forge.Context) string {
					return "custom-resource"
				}),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Equal(t, "custom-resource", entry.Resource)
		require.Equal(t, "/test", entry.Path) // Path should still be /test
	})
}

func TestAuditLog_AsyncStoreCall(t *testing.T) {
	t.Parallel()

	t.Run("verify store.Log called in goroutine - response returns immediately, entry arrives after", func(t *testing.T) {
		t.Parallel()

		// Store with 100ms delay
		store := &mockAuditStore{
			entries: make(chan *forge.AuditEntry, 1),
			delay:   100 * time.Millisecond,
		}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store)),
			forge.WithHandlers(handler),
		)

		start := time.Now()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		responseDuration := time.Since(start)

		require.Equal(t, http.StatusOK, rec.Code)
		// Response should complete before the store delay
		require.Less(t, responseDuration, 50*time.Millisecond)

		// Entry should arrive after the delay
		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
	})
}

func TestAuditLog_StoreFailureLogged(t *testing.T) {
	t.Parallel()

	t.Run("store.Log failure doesn't affect response, just logs warning", func(t *testing.T) {
		t.Parallel()

		logHandler := &logCapture{}
		logger := slog.New(logHandler)

		store := &mockAuditStore{
			entries: make(chan *forge.AuditEntry, 1),
			err:     errors.New("store failure"),
		}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store,
				middlewares.WithAuditLogger(logger),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		// Give goroutine time to log the error
		time.Sleep(100 * time.Millisecond)

		require.True(t, logHandler.hasMessage("audit: failed to log entry"))
	})
}

func TestAuditLog_Timeout(t *testing.T) {
	t.Parallel()

	t.Run("slow store.Log gets canceled via context timeout", func(t *testing.T) {
		t.Parallel()

		logHandler := &logCapture{}
		logger := slog.New(logHandler)

		// Store with 2 second delay, but timeout is 50ms
		store := &mockAuditStore{
			entries: make(chan *forge.AuditEntry, 1),
			delay:   2 * time.Second,
		}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store,
				middlewares.WithAuditTimeout(50*time.Millisecond),
				middlewares.WithAuditLogger(logger),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		// Give goroutine time to timeout and log
		time.Sleep(200 * time.Millisecond)

		require.True(t, logHandler.hasMessage("audit: failed to log entry"))
	})
}

func TestAuditLog_MetadataFuncMerge(t *testing.T) {
	t.Parallel()

	t.Run("MetadataFunc values merge with handler values, handler takes precedence", func(t *testing.T) {
		t.Parallel()

		store := &mockAuditStore{entries: make(chan *forge.AuditEntry, 1)}
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				middlewares.SetAuditMetadata(c, "key1", "handler-value")
				middlewares.SetAuditMetadata(c, "key3", "handler-only")
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithMiddleware(middlewares.AuditLog(store,
				middlewares.WithAuditMetadataFunc(func(c forge.Context) map[string]string {
					return map[string]string{
						"key1": "global-value",
						"key2": "global-only",
					}
				}),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		entry := waitForEntry(t, store, time.Second)
		require.NotNil(t, entry)
		require.Equal(t, "handler-value", entry.Metadata["key1"]) // Handler takes precedence
		require.Equal(t, "global-only", entry.Metadata["key2"])   // Global value used
		require.Equal(t, "handler-only", entry.Metadata["key3"])  // Handler-only value
	})
}

func TestAuditLog_GetAuditEntryNil(t *testing.T) {
	t.Parallel()

	t.Run("GetAuditEntry returns nil when middleware not applied", func(t *testing.T) {
		t.Parallel()

		var capturedEntry *forge.AuditEntry
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedEntry = middlewares.GetAuditEntry(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Nil(t, capturedEntry)
	})
}

func TestAuditLog_NilStorePanics(t *testing.T) {
	t.Parallel()

	t.Run("nil store panics", func(t *testing.T) {
		t.Parallel()

		require.Panics(t, func() {
			middlewares.AuditLog(nil)
		})
	})
}
