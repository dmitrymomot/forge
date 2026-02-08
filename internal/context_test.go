package internal_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/internal/mocks"
	"github.com/dmitrymomot/forge/pkg/cookie"
)

// testHandler wraps a test function as a forge.Handler for easy route registration.
type testHandler struct {
	handlerFunc func(forge.Context) error
}

func (h *testHandler) Routes(r forge.Router) {
	r.GET("/test", h.handlerFunc)
	r.POST("/test", h.handlerFunc)
	r.DELETE("/test", h.handlerFunc)
}

func TestContext_UserID(t *testing.T) {
	t.Parallel()

	t.Run("returns empty string when no session configured", func(t *testing.T) {
		t.Parallel()

		var userID string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				userID = c.UserID()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Empty(t, userID)
	})

	t.Run("returns user ID from authenticated session", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		expectedUserID := "user-123"
		sess := &internal.Session{
			ID:           "sess-456",
			TokenHash:    "hash",
			UserID:       &expectedUserID,
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var userID string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				userID = c.UserID()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, expectedUserID, userID)
		store.AssertExpectations(t)
	})

	t.Run("returns empty string for anonymous session", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		sess := &internal.Session{
			ID:           "sess-789",
			TokenHash:    "hash",
			UserID:       nil,
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var userID string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				userID = c.UserID()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Empty(t, userID)
		store.AssertExpectations(t)
	})
}

func TestContext_IsAuthenticated(t *testing.T) {
	t.Parallel()

	t.Run("returns false when no user", func(t *testing.T) {
		t.Parallel()

		var isAuth bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				isAuth = c.IsAuthenticated()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.False(t, isAuth)
	})

	t.Run("returns true when user is authenticated", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		userID := "user-456"
		sess := &internal.Session{
			ID:           "sess-123",
			TokenHash:    "hash",
			UserID:       &userID,
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var isAuth bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				isAuth = c.IsAuthenticated()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.True(t, isAuth)
		store.AssertExpectations(t)
	})
}

func TestContext_IsCurrentUser(t *testing.T) {
	t.Parallel()

	t.Run("returns false when no user", func(t *testing.T) {
		t.Parallel()

		var isCurrent bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				isCurrent = c.IsCurrentUser("user-123")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.False(t, isCurrent)
	})

	t.Run("returns true when ID matches", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		userID := "user-789"
		sess := &internal.Session{
			ID:           "sess-123",
			TokenHash:    "hash",
			UserID:       &userID,
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var isCurrent bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				isCurrent = c.IsCurrentUser("user-789")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.True(t, isCurrent)
		store.AssertExpectations(t)
	})

	t.Run("returns false when ID does not match", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		userID := "user-789"
		sess := &internal.Session{
			ID:           "sess-123",
			TokenHash:    "hash",
			UserID:       &userID,
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var isCurrent bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				isCurrent = c.IsCurrentUser("user-different")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.False(t, isCurrent)
		store.AssertExpectations(t)
	})
}

func TestContext_RBAC(t *testing.T) {
	t.Parallel()

	rolePermissions := forge.RolePermissions{
		"admin": {forge.Permission("users:delete"), forge.Permission("posts:delete")},
		"user":  {forge.Permission("posts:create")},
	}

	t.Run("Can returns false when RBAC not configured", func(t *testing.T) {
		t.Parallel()

		var canDelete bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				canDelete = c.Can(forge.Permission("users:delete"))
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.False(t, canDelete)
	})

	t.Run("Can returns true when role has permission", func(t *testing.T) {
		t.Parallel()

		extractor := func(c forge.Context) string {
			return "admin"
		}

		var canDelete bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				canDelete = c.Can(forge.Permission("users:delete"))
				return nil
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithRoles(rolePermissions, extractor),
			forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.True(t, canDelete)
	})

	t.Run("Can returns false when role lacks permission", func(t *testing.T) {
		t.Parallel()

		extractor := func(c forge.Context) string {
			return "user"
		}

		var canDelete bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				canDelete = c.Can(forge.Permission("users:delete"))
				return nil
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithRoles(rolePermissions, extractor),
			forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.False(t, canDelete)
	})

	t.Run("Role returns correct role from extractor", func(t *testing.T) {
		t.Parallel()

		extractor := func(c forge.Context) string {
			return "admin"
		}

		var role string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				role = c.Role()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithRoles(rolePermissions, extractor),
			forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "admin", role)
	})

	t.Run("Role is cached and extractor called only once per request", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		extractor := func(c forge.Context) string {
			callCount++
			return "admin"
		}

		var role1, role2 string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				role1 = c.Role()
				role2 = c.Role()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithRoles(rolePermissions, extractor),
			forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "admin", role1)
		require.Equal(t, "admin", role2)
		require.Equal(t, 1, callCount, "extractor should be called only once per request")
	})
}

func TestContext_Domain(t *testing.T) {
	t.Parallel()

	t.Run("extracts domain from host stripping port", func(t *testing.T) {
		t.Parallel()

		var domain string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				domain = c.Domain()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "http://example.com:8080/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "example.com", domain)
	})
}

func TestContext_Subdomain(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when no base domain configured", func(t *testing.T) {
		t.Parallel()

		var subdomain string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				subdomain = c.Subdomain()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "http://api.example.com/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Empty(t, subdomain)
	})

	t.Run("extracts subdomain when base domain configured", func(t *testing.T) {
		t.Parallel()

		var subdomain string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				subdomain = c.Subdomain()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{BaseDomain: "example.com"}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "http://api.example.com/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "api", subdomain)
	})
}

func TestContext_Query(t *testing.T) {
	t.Parallel()

	t.Run("returns query parameter value", func(t *testing.T) {
		t.Parallel()

		var page string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				page = c.Query("page")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test?page=2", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "2", page)
	})

	t.Run("returns empty string when parameter missing", func(t *testing.T) {
		t.Parallel()

		var page string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				page = c.Query("page")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Empty(t, page)
	})
}

func TestContext_QueryDefault(t *testing.T) {
	t.Parallel()

	t.Run("returns query value when present", func(t *testing.T) {
		t.Parallel()

		var limit string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				limit = c.QueryDefault("limit", "10")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test?limit=25", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "25", limit)
	})

	t.Run("returns default when parameter missing", func(t *testing.T) {
		t.Parallel()

		var limit string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				limit = c.QueryDefault("limit", "10")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "10", limit)
	})

	t.Run("returns default when parameter empty", func(t *testing.T) {
		t.Parallel()

		var limit string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				limit = c.QueryDefault("limit", "10")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test?limit=", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "10", limit)
	})
}

func TestContext_Form(t *testing.T) {
	t.Parallel()

	t.Run("returns form value from URL-encoded body", func(t *testing.T) {
		t.Parallel()

		var email string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				email = c.Form("email")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		form := url.Values{}
		form.Set("email", "test@example.com")
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "test@example.com", email)
	})
}

func TestContext_JSON(t *testing.T) {
	t.Parallel()

	t.Run("writes JSON response with correct content type", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"message": "success"})
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
		require.JSONEq(t, `{"message":"success"}`, rec.Body.String())
	})
}

func TestContext_String(t *testing.T) {
	t.Parallel()

	t.Run("writes plain text response with correct content type", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "Hello World")
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
		require.Equal(t, "Hello World", rec.Body.String())
	})
}

func TestContext_NoContent(t *testing.T) {
	t.Parallel()

	t.Run("writes response with no body and correct status", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.NoContent(http.StatusNoContent)
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodDelete, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Empty(t, rec.Body.String())
	})
}

func TestContext_Error(t *testing.T) {
	t.Parallel()

	t.Run("creates HTTPError with code and message", func(t *testing.T) {
		t.Parallel()

		var createdErr *forge.HTTPError
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				createdErr = c.Error(http.StatusBadRequest, "invalid input")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NotNil(t, createdErr)
		require.Equal(t, http.StatusBadRequest, createdErr.Code)
		require.Equal(t, "invalid input", createdErr.Message)
	})

	t.Run("applies options to HTTPError", func(t *testing.T) {
		t.Parallel()

		var createdErr *forge.HTTPError
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				createdErr = c.Error(http.StatusNotFound, "not found",
					forge.WithTitle("Missing Resource"),
					forge.WithErrorCode("ERR_404"),
				)
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NotNil(t, createdErr)
		require.Equal(t, "Missing Resource", createdErr.Title)
		require.Equal(t, "ERR_404", createdErr.ErrorCode)
	})
}

func TestContext_IsHTMX(t *testing.T) {
	t.Parallel()

	t.Run("returns false for regular request", func(t *testing.T) {
		t.Parallel()

		var isHTMX bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				isHTMX = c.IsHTMX()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.False(t, isHTMX)
	})

	t.Run("returns true for HTMX request", func(t *testing.T) {
		t.Parallel()

		var isHTMX bool
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				isHTMX = c.IsHTMX()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.True(t, isHTMX)
	})
}

func TestContext_SetGet(t *testing.T) {
	t.Parallel()

	t.Run("stores and retrieves values in request context", func(t *testing.T) {
		t.Parallel()

		type contextKey struct{}

		var retrieved any
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				c.Set(contextKey{}, "test-value")
				retrieved = c.Get(contextKey{})
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "test-value", retrieved)
	})

	t.Run("returns nil for missing key", func(t *testing.T) {
		t.Parallel()

		type contextKey struct{}

		var retrieved any
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				retrieved = c.Get(contextKey{})
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Nil(t, retrieved)
	})
}

func TestContext_Cookie(t *testing.T) {
	t.Parallel()

	t.Run("returns cookie value when present", func(t *testing.T) {
		t.Parallel()

		var value string
		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				value, err = c.Cookie("session")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, err)
		require.Equal(t, "abc123", value)
	})

	t.Run("returns error when cookie missing", func(t *testing.T) {
		t.Parallel()

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				_, err = c.Cookie("session")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, err)
	})
}

func TestContext_SetCookie(t *testing.T) {
	t.Parallel()

	t.Run("sets cookie in response", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				c.SetCookie("pref", "dark", 3600)
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, "pref", cookies[0].Name)
		require.Equal(t, "dark", cookies[0].Value)
	})
}

func TestContext_DeleteCookie(t *testing.T) {
	t.Parallel()

	t.Run("deletes cookie by setting negative maxAge", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				c.DeleteCookie("session")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, "session", cookies[0].Name)
		require.True(t, cookies[0].MaxAge < 0)
	})
}

func TestContext_SignedCookie(t *testing.T) {
	t.Parallel()

	t.Run("returns error when no secret configured", func(t *testing.T) {
		t.Parallel()

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				_, err = c.CookieSigned("secure")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, err)
		require.True(t, errors.Is(err, cookie.ErrNoSecret))
	})

	t.Run("sets signed cookie when secret configured", func(t *testing.T) {
		t.Parallel()

		// 32-byte secret for AES-256
		secret := "12345678901234567890123456789012"

		var setErr error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				setErr = c.SetCookieSigned("secure", "value123", 3600)
				return nil
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: secret}),
			forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, setErr)

		// Verify cookie was set
		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, "secure", cookies[0].Name)
		require.NotEmpty(t, cookies[0].Value)
	})
}

func TestContext_I18n(t *testing.T) {
	t.Parallel()

	t.Run("T returns key when no translator in context", func(t *testing.T) {
		t.Parallel()

		var result string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				result = c.T("greeting.hello")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, "greeting.hello", result)
	})

	t.Run("Language returns empty when no translator", func(t *testing.T) {
		t.Parallel()

		var lang string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				lang = c.Language()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Empty(t, lang)
	})
}

func TestContext_SessionValue(t *testing.T) {
	t.Parallel()

	t.Run("returns error when session not configured", func(t *testing.T) {
		t.Parallel()

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				_, err = c.SessionValue("key")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, err)
		require.True(t, errors.Is(err, internal.ErrSessionNotConfigured))
	})

	t.Run("retrieves value from session", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		sess := &internal.Session{
			ID:           "sess-123",
			TokenHash:    "hash",
			Data:         map[string]any{"cart": "item1,item2"},
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var value any
		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				value, err = c.SessionValue("cart")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, err)
		require.Equal(t, "item1,item2", value)
		store.AssertExpectations(t)
	})

	t.Run("returns nil when key not found in session", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		sess := &internal.Session{
			ID:           "sess-123",
			TokenHash:    "hash",
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var value any
		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				value, err = c.SessionValue("missing")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, err)
		require.Nil(t, value)
		store.AssertExpectations(t)
	})
}

func TestContext_SetSessionValue(t *testing.T) {
	t.Parallel()

	t.Run("returns error when session not configured", func(t *testing.T) {
		t.Parallel()

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				err = c.SetSessionValue("key", "value")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, err)
		require.True(t, errors.Is(err, internal.ErrSessionNotConfigured))
	})

	t.Run("stores value in session and marks dirty", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		sess := &internal.Session{
			ID:           "sess-123",
			TokenHash:    "hash",
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				err = c.SetSessionValue("cart", "item1")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, err)
		require.True(t, sess.IsDirty())
		require.Equal(t, "item1", sess.Data["cart"])
		store.AssertExpectations(t)
	})
}

func TestContext_DestroySession(t *testing.T) {
	t.Parallel()

	t.Run("returns error when session not configured", func(t *testing.T) {
		t.Parallel()

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				err = c.DestroySession()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, err)
		require.True(t, errors.Is(err, internal.ErrSessionNotConfigured))
	})

	t.Run("deletes session from store and clears cookie", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		sess := &internal.Session{
			ID:           "sess-to-delete",
			TokenHash:    "hash",
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)
		store.On("Delete", mock.Anything, "sess-to-delete").Return(nil)

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				// Load session first
				_, _ = c.Session()
				err = c.DestroySession()
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, err)

		// Verify cookie deletion
		cookies := rec.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "__sid" {
				sessionCookie = c
				break
			}
		}
		require.NotNil(t, sessionCookie)
		require.True(t, sessionCookie.MaxAge < 0, "cookie should be expired")

		store.AssertExpectations(t)
	})
}

func TestContext_AuthenticateSession(t *testing.T) {
	t.Parallel()

	t.Run("returns error when session not configured", func(t *testing.T) {
		t.Parallel()

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				err = c.AuthenticateSession("user-123")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, err)
		require.True(t, errors.Is(err, internal.ErrSessionNotConfigured))
	})

	t.Run("associates user with session and rotates token", func(t *testing.T) {
		t.Parallel()

		store := mocks.NewSessionStore(t)
		sess := &internal.Session{
			ID:           "sess-123",
			TokenHash:    "old-hash",
			UserID:       nil, // Anonymous
			Data:         make(map[string]any),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		store.On("GetByTokenHash", mock.Anything, mock.Anything).Return(sess, nil)
		store.On("CountByUserID", mock.Anything, "user-456").Return(0, nil)
		store.On("Update", mock.Anything, mock.MatchedBy(func(s *internal.Session) bool {
			// Verify session was updated with user ID and new token hash
			return s.UserID != nil && *s.UserID == "user-456" && s.TokenHash != "old-hash"
		})).Return(nil)

		var err error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				err = c.AuthenticateSession("user-456")
				return nil
			},
		}

		app := forge.New(forge.AppConfig{}, forge.WithSession(store), forge.WithHandlers(handler))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "test-token"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, err)
		require.NotNil(t, sess.UserID)
		require.Equal(t, "user-456", *sess.UserID)
		require.NotEqual(t, "old-hash", sess.TokenHash, "token should be rotated")

		// Verify new cookie was set
		cookies := rec.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "__sid" {
				sessionCookie = c
				break
			}
		}
		require.NotNil(t, sessionCookie)
		require.NotEqual(t, "test-token", sessionCookie.Value, "cookie should contain new token")

		store.AssertExpectations(t)
	})
}
