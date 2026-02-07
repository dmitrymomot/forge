package internal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/session"
)

// Compile-time check: mockSessionStore implements session.Store.
var _ session.Store = (*mockSessionStore)(nil)

// requestVia creates an App with the given options, registers a handler at GET /,
// executes fn inside that handler, and sends a request. This lets tests exercise
// the real requestContext without accessing unexported symbols.
func requestVia(t *testing.T, req *http.Request, opts []internal.Option, fn func(c internal.Context)) *httptest.ResponseRecorder {
	t.Helper()

	h := &captureHandler{fn: fn}
	opts = append(opts, internal.WithHandlers(h))
	app := internal.New(opts...)

	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)
	return w
}

type captureHandler struct {
	fn func(c internal.Context)
}

func (h *captureHandler) Routes(r internal.Router) {
	r.GET("/", func(c internal.Context) error {
		h.fn(c)
		return nil
	})
}

// --- context.Context interface tests ---

func TestContextImplementsContextInterface(t *testing.T) {
	t.Parallel()

	t.Run("Deadline delegates to request context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		requestVia(t, req, nil, func(c internal.Context) {
			deadline, ok := c.Deadline()
			require.True(t, ok)
			require.False(t, deadline.IsZero())

			expected, _ := ctx.Deadline()
			require.Equal(t, expected, deadline)
		})
	})

	t.Run("Deadline returns false when no deadline set", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			deadline, ok := c.Deadline()
			require.False(t, ok)
			require.True(t, deadline.IsZero())
		})
	})

	t.Run("Done delegates to request context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		requestVia(t, req, nil, func(c internal.Context) {
			// Done channel should not be closed yet.
			select {
			case <-c.Done():
				t.Fatal("Done channel should not be closed before cancel")
			default:
			}

			cancel()

			// Done channel should be closed after cancel.
			select {
			case <-c.Done():
				// expected
			case <-time.After(time.Second):
				t.Fatal("Done channel should be closed after cancel")
			}
		})
	})

	t.Run("Done returns nil when no cancellation", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			// Just verify it doesn't panic.
			_ = c.Done()
		})
	})

	t.Run("Err returns nil before cancellation", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(t.Context())
		requestVia(t, req, nil, func(c internal.Context) {
			require.NoError(t, c.Err())
		})
	})

	t.Run("Err returns Canceled after cancel", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		requestVia(t, req, nil, func(c internal.Context) {
			cancel()
			require.ErrorIs(t, c.Err(), context.Canceled)
		})
	})

	t.Run("Err returns DeadlineExceeded after timeout", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()

		// Wait for the timeout to expire.
		time.Sleep(time.Millisecond)

		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		requestVia(t, req, nil, func(c internal.Context) {
			require.ErrorIs(t, c.Err(), context.DeadlineExceeded)
		})
	})

	t.Run("Value delegates to request context", func(t *testing.T) {
		t.Parallel()

		type testKey struct{}
		ctx := context.WithValue(context.Background(), testKey{}, "hello")

		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		requestVia(t, req, nil, func(c internal.Context) {
			val := c.Value(testKey{})
			require.Equal(t, "hello", val)
		})
	})

	t.Run("Value returns nil for missing key", func(t *testing.T) {
		t.Parallel()

		type testKey struct{}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Nil(t, c.Value(testKey{}))
		})
	})

	t.Run("Value reflects Set changes", func(t *testing.T) {
		t.Parallel()

		type testKey struct{}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(testKey{}, 42)
			require.Equal(t, 42, c.Value(testKey{}))
		})
	})

	t.Run("context can be passed to functions accepting context.Context", func(t *testing.T) {
		t.Parallel()

		type testKey struct{}
		ctx := context.WithValue(context.Background(), testKey{}, "world")
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		requestVia(t, req, nil, func(c internal.Context) {
			// Wrap in context.WithValue to prove it works as a parent context.
			type childKey struct{}
			derived := context.WithValue(c, childKey{}, "child-val")

			require.Equal(t, "world", derived.Value(testKey{}))
			require.Equal(t, "child-val", derived.Value(childKey{}))
		})
	})
}

// --- Identity methods tests ---

func TestIdentityMethods(t *testing.T) {
	t.Parallel()

	t.Run("UserID returns empty string when no session manager", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "", c.UserID())
		})
	})

	t.Run("IsAuthenticated returns false when no session manager", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.False(t, c.IsAuthenticated())
		})
	})

	t.Run("IsCurrentUser returns false when no session manager", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.False(t, c.IsCurrentUser("user-123"))
		})
	})

	t.Run("UserID returns empty string for anonymous session", func(t *testing.T) {
		t.Parallel()

		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-1", "tok-1", now.Add(24*time.Hour))
				// Anonymous: UserID stays nil
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.Equal(t, "", c.UserID())
		})
	})

	t.Run("IsAuthenticated returns false for anonymous session", func(t *testing.T) {
		t.Parallel()

		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				return session.New("sess-1", "tok-1", now.Add(24*time.Hour)), nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.IsAuthenticated())
		})
	})

	t.Run("UserID returns user ID from authenticated session", func(t *testing.T) {
		t.Parallel()

		userID := "user-456"
		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-1", "tok-1", now.Add(24*time.Hour))
				s.UserID = &userID
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.Equal(t, "user-456", c.UserID())
		})
	})

	t.Run("IsAuthenticated returns true for authenticated session", func(t *testing.T) {
		t.Parallel()

		userID := "user-789"
		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-1", "tok-1", now.Add(24*time.Hour))
				s.UserID = &userID
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.True(t, c.IsAuthenticated())
		})
	})

	t.Run("IsCurrentUser returns true for matching user", func(t *testing.T) {
		t.Parallel()

		userID := "user-abc"
		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-1", "tok-1", now.Add(24*time.Hour))
				s.UserID = &userID
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.True(t, c.IsCurrentUser("user-abc"))
		})
	})

	t.Run("IsCurrentUser returns false for non-matching user", func(t *testing.T) {
		t.Parallel()

		userID := "user-abc"
		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-1", "tok-1", now.Add(24*time.Hour))
				s.UserID = &userID
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.IsCurrentUser("user-different"))
		})
	})

	t.Run("IsCurrentUser returns false when unauthenticated", func(t *testing.T) {
		t.Parallel()

		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				return session.New("sess-1", "tok-1", now.Add(24*time.Hour)), nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.IsCurrentUser("any-id"))
		})
	})

	t.Run("UserID returns empty for session not found", func(t *testing.T) {
		t.Parallel()

		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				return nil, session.ErrNotFound
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-invalid"})

		opts := []internal.Option{
			internal.WithSession(store),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.Equal(t, "", c.UserID())
		})
	})
}

// --- AuthenticateSession tests ---

func TestAuthenticateSessionRotatesToken(t *testing.T) {
	t.Parallel()

	const oldToken = "old-token-abc"
	var updatedSession *session.Session

	store := &mockSessionStore{
		getFn: func(_ context.Context, token string) (*session.Session, error) {
			now := time.Now()
			s := session.New("sess-1", oldToken, now.Add(24*time.Hour))
			return s, nil
		},
		updateFn: func(_ context.Context, s *session.Session) error {
			updatedSession = s
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "__sid", Value: oldToken})

	opts := []internal.Option{
		internal.WithSession(store),
	}

	w := requestVia(t, req, opts, func(c internal.Context) {
		err := c.AuthenticateSession("user-1")
		require.NoError(t, err)
	})

	// The session should have been updated with a rotated (different) token.
	require.NotNil(t, updatedSession)
	require.NotEqual(t, oldToken, updatedSession.Token, "token should have been rotated")
	require.NotNil(t, updatedSession.UserID)
	require.Equal(t, "user-1", *updatedSession.UserID)

	// The response cookie should carry the new token.
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "__sid" {
			found = true
			require.NotEqual(t, oldToken, c.Value, "cookie should have the new rotated token")
			require.Equal(t, updatedSession.Token, c.Value)
		}
	}
	require.True(t, found, "expected __sid cookie in response")
}

// --- RBAC tests ---

func TestRBAC(t *testing.T) {
	t.Parallel()

	t.Run("Can returns false when RBAC not configured", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.False(t, c.Can("any_permission"))
		})
	})

	t.Run("Can returns false when only permissions configured but no extractor", func(t *testing.T) {
		t.Parallel()

		// WithRoles requires both permissions and extractor, so we can't
		// pass one without the other via the public API. Instead, we pass
		// an extractor that returns empty string.
		perms := internal.RolePermissions{
			"admin": {"read", "write"},
		}
		extractor := func(ctx internal.Context) string { return "" }

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.Can("read"))
		})
	})

	t.Run("Can returns true for valid role with permission", func(t *testing.T) {
		t.Parallel()

		perms := internal.RolePermissions{
			"admin":  {"read", "write", "delete"},
			"viewer": {"read"},
		}
		extractor := func(ctx internal.Context) string { return "admin" }

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.True(t, c.Can("read"))
			require.True(t, c.Can("write"))
			require.True(t, c.Can("delete"))
		})
	})

	t.Run("Can returns false for valid role without permission", func(t *testing.T) {
		t.Parallel()

		perms := internal.RolePermissions{
			"admin":  {"read", "write", "delete"},
			"viewer": {"read"},
		}
		extractor := func(ctx internal.Context) string { return "viewer" }

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.True(t, c.Can("read"))
			require.False(t, c.Can("write"))
			require.False(t, c.Can("delete"))
		})
	})

	t.Run("Can returns false for unknown role", func(t *testing.T) {
		t.Parallel()

		perms := internal.RolePermissions{
			"admin": {"read", "write"},
		}
		extractor := func(ctx internal.Context) string { return "unknown-role" }

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.Can("read"))
		})
	})

	t.Run("Can returns false for empty role", func(t *testing.T) {
		t.Parallel()

		perms := internal.RolePermissions{
			"admin": {"read", "write"},
		}
		extractor := func(ctx internal.Context) string { return "" }

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.Can("read"))
		})
	})

	t.Run("extractor is called only once per request", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		perms := internal.RolePermissions{
			"admin": {"read", "write", "delete"},
		}
		extractor := func(ctx internal.Context) string {
			callCount++
			return "admin"
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.True(t, c.Can("read"))
			require.True(t, c.Can("write"))
			require.True(t, c.Can("delete"))
			require.False(t, c.Can("nonexistent"))

			// Extractor should have been called exactly once due to caching.
			require.Equal(t, 1, callCount)
		})
	})

	t.Run("cached role persists across Can calls", func(t *testing.T) {
		t.Parallel()

		perms := internal.RolePermissions{
			"admin":  {"read", "write"},
			"viewer": {"read"},
		}
		firstCall := true
		extractor := func(ctx internal.Context) string {
			if firstCall {
				firstCall = false
				return "admin"
			}
			return "viewer"
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			// First call triggers extraction → "admin".
			require.True(t, c.Can("write"))

			// Second call should use cached "admin", not re-extract to "viewer".
			require.True(t, c.Can("write"))
		})
	})

	t.Run("Can with empty permissions map", func(t *testing.T) {
		t.Parallel()

		perms := internal.RolePermissions{}
		extractor := func(ctx internal.Context) string { return "admin" }

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.Can("read"))
		})
	})

	t.Run("Can with role that has empty permissions slice", func(t *testing.T) {
		t.Parallel()

		perms := internal.RolePermissions{
			"admin": {},
		}
		extractor := func(ctx internal.Context) string { return "admin" }

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		opts := []internal.Option{
			internal.WithRoles(perms, extractor),
		}
		requestVia(t, req, opts, func(c internal.Context) {
			require.False(t, c.Can("read"))
		})
	})
}

func TestCanExtractorCalledOnce(t *testing.T) {
	t.Parallel()

	var extractorCalls atomic.Int32

	perms := internal.RolePermissions{
		"admin": {"read", "write", "delete"},
	}
	extractor := func(ctx internal.Context) string {
		extractorCalls.Add(1)
		return "admin"
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	opts := []internal.Option{
		internal.WithRoles(perms, extractor),
	}

	requestVia(t, req, opts, func(c internal.Context) {
		// Call Can() many times with different permissions.
		results := make([]bool, 10)
		for i := range results {
			results[i] = c.Can("read")
		}

		// All calls should return the same result.
		for i, r := range results {
			require.True(t, r, "call %d got false", i)
		}

		// Also verify other permissions.
		require.True(t, c.Can("write"))
		require.True(t, c.Can("delete"))
		require.False(t, c.Can("nonexistent"))

		// The extractor should have been called exactly once (cached after first call).
		require.Equal(t, int32(1), extractorCalls.Load(), "extractor should be called exactly once")
	})
}

// --- Mock session store ---

type mockSessionStore struct {
	createFn         func(ctx context.Context, s *session.Session) error
	getFn            func(ctx context.Context, token string) (*session.Session, error)
	updateFn         func(ctx context.Context, s *session.Session) error
	deleteFn         func(ctx context.Context, id string) error
	deleteByUserIDFn func(ctx context.Context, userID string) error
}

func (m *mockSessionStore) Create(ctx context.Context, s *session.Session) error {
	if m.createFn != nil {
		return m.createFn(ctx, s)
	}
	return nil
}

func (m *mockSessionStore) Get(ctx context.Context, token string) (*session.Session, error) {
	if m.getFn != nil {
		return m.getFn(ctx, token)
	}
	return nil, session.ErrNotFound
}

func (m *mockSessionStore) Update(ctx context.Context, s *session.Session) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, s)
	}
	return nil
}

func (m *mockSessionStore) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockSessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	if m.deleteByUserIDFn != nil {
		return m.deleteByUserIDFn(ctx, userID)
	}
	return nil
}

func (m *mockSessionStore) Touch(ctx context.Context, id string, lastActiveAt time.Time) error {
	return nil
}
