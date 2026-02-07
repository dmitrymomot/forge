package internal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/cookie"
	"github.com/dmitrymomot/forge/pkg/session"
)

// paramCaptureHandler registers a GET /{id} route.
type paramCaptureHandler struct {
	fn func(c internal.Context)
}

func (h *paramCaptureHandler) Routes(r internal.Router) {
	r.GET("/{id}", func(c internal.Context) error {
		h.fn(c)
		return nil
	})
}

// requestViaParam creates an App and sends a request to GET /{id}.
func requestViaParam(t *testing.T, req *http.Request, opts []internal.Option, fn func(c internal.Context)) *httptest.ResponseRecorder {
	t.Helper()

	h := &paramCaptureHandler{fn: fn}
	opts = append(opts, internal.WithHandlers(h))
	app := internal.New(opts...)

	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)
	return w
}

// --- Extractor tests ---

func TestExtractor(t *testing.T) {
	t.Parallel()

	t.Run("empty sources returns false", func(t *testing.T) {
		t.Parallel()

		ext := internal.NewExtractor()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := ext.Extract(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})

	t.Run("first source wins", func(t *testing.T) {
		t.Parallel()

		ext := internal.NewExtractor(
			internal.FromHeader("X-First"),
			internal.FromHeader("X-Second"),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-First", "first-val")
		req.Header.Set("X-Second", "second-val")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := ext.Extract(c)
			require.True(t, ok)
			require.Equal(t, "first-val", v)
		})
	})

	t.Run("falls through to second source when first misses", func(t *testing.T) {
		t.Parallel()

		ext := internal.NewExtractor(
			internal.FromHeader("X-Missing"),
			internal.FromHeader("X-Present"),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Present", "found")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := ext.Extract(c)
			require.True(t, ok)
			require.Equal(t, "found", v)
		})
	})

	t.Run("all sources miss returns false", func(t *testing.T) {
		t.Parallel()

		ext := internal.NewExtractor(
			internal.FromHeader("X-A"),
			internal.FromQuery("b"),
		)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := ext.Extract(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromHeader tests ---

func TestFromHeader(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		src := internal.FromHeader("X-Api-Key")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Api-Key", "secret123")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "secret123", v)
		})
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		src := internal.FromHeader("X-Api-Key")
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})

	t.Run("empty value", func(t *testing.T) {
		t.Parallel()

		src := internal.FromHeader("X-Api-Key")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Api-Key", "")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromQuery tests ---

func TestFromQuery(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		src := internal.FromQuery("token")
		req := httptest.NewRequest(http.MethodGet, "/?token=abc", nil)

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "abc", v)
		})
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		src := internal.FromQuery("token")
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})

	t.Run("empty value", func(t *testing.T) {
		t.Parallel()

		src := internal.FromQuery("token")
		req := httptest.NewRequest(http.MethodGet, "/?token=", nil)

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromCookie tests ---

func TestFromCookie(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		src := internal.FromCookie("auth")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "auth", Value: "token123"})

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "token123", v)
		})
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		src := internal.FromCookie("auth")
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromCookieSigned tests ---

func TestFromCookieSigned(t *testing.T) {
	t.Parallel()

	// secret must be at least 32 bytes
	secret := "this-is-a-32-byte-secret-key!!!!" // 32 bytes

	t.Run("present and valid", func(t *testing.T) {
		t.Parallel()

		src := internal.FromCookieSigned("sid")

		opts := []internal.Option{
			internal.WithCookieOptions(cookie.WithSecret(secret)),
		}

		// First request: set a signed cookie
		reqSet := httptest.NewRequest(http.MethodGet, "/", nil)
		wSet := requestVia(t, reqSet, opts, func(c internal.Context) {
			err := c.SetCookieSigned("sid", "signed-value", 3600)
			require.NoError(t, err)
		})

		// Extract cookie from response
		cookies := wSet.Result().Cookies()
		require.NotEmpty(t, cookies)

		// Second request: read the signed cookie
		reqGet := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, ck := range cookies {
			reqGet.AddCookie(ck)
		}

		requestVia(t, reqGet, opts, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "signed-value", v)
		})
	})

	t.Run("missing returns false", func(t *testing.T) {
		t.Parallel()

		src := internal.FromCookieSigned("sid")

		opts := []internal.Option{
			internal.WithCookieOptions(cookie.WithSecret(secret)),
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromCookieEncrypted tests ---

func TestFromCookieEncrypted(t *testing.T) {
	t.Parallel()

	secret := "this-is-a-32-byte-secret-key!!!!" // 32 bytes

	t.Run("present and valid", func(t *testing.T) {
		t.Parallel()

		src := internal.FromCookieEncrypted("enc")

		opts := []internal.Option{
			internal.WithCookieOptions(cookie.WithSecret(secret)),
		}

		// First request: set an encrypted cookie
		reqSet := httptest.NewRequest(http.MethodGet, "/", nil)
		wSet := requestVia(t, reqSet, opts, func(c internal.Context) {
			err := c.SetCookieEncrypted("enc", "encrypted-value", 3600)
			require.NoError(t, err)
		})

		cookies := wSet.Result().Cookies()
		require.NotEmpty(t, cookies)

		// Second request: read the encrypted cookie
		reqGet := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, ck := range cookies {
			reqGet.AddCookie(ck)
		}

		requestVia(t, reqGet, opts, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "encrypted-value", v)
		})
	})

	t.Run("missing returns false", func(t *testing.T) {
		t.Parallel()

		src := internal.FromCookieEncrypted("enc")

		opts := []internal.Option{
			internal.WithCookieOptions(cookie.WithSecret(secret)),
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, opts, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromParam tests ---

func TestFromParam(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		src := internal.FromParam("id")
		req := httptest.NewRequest(http.MethodGet, "/abc123", nil)

		requestViaParam(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "abc123", v)
		})
	})

	t.Run("empty param route segment not matched", func(t *testing.T) {
		t.Parallel()

		src := internal.FromParam("id")
		// Use a different param name that the route doesn't match
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		requestViaParam(t, req, nil, func(c internal.Context) {
			// "id" is matched by the route, so it will be "test"
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "test", v)
		})
	})

	t.Run("missing param name", func(t *testing.T) {
		t.Parallel()

		// Ask for a param that doesn't exist in the route
		src := internal.FromParam("slug")
		req := httptest.NewRequest(http.MethodGet, "/something", nil)

		requestViaParam(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromForm tests ---

func TestFromForm(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		src := internal.FromForm("email")

		body := url.Values{"email": {"user@example.com"}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		// Use POST handler for form tests
		h := &postCaptureHandler{}
		opts := []internal.Option{internal.WithHandlers(h)}
		app := internal.New(opts...)

		var gotVal string
		var gotOk bool
		h.fn = func(c internal.Context) {
			gotVal, gotOk = src(c)
		}

		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		require.True(t, gotOk)
		require.Equal(t, "user@example.com", gotVal)
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		src := internal.FromForm("email")

		body := url.Values{"name": {"John"}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		h := &postCaptureHandler{}
		opts := []internal.Option{internal.WithHandlers(h)}
		app := internal.New(opts...)

		var gotVal string
		var gotOk bool
		h.fn = func(c internal.Context) {
			gotVal, gotOk = src(c)
		}

		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		require.False(t, gotOk)
		require.Empty(t, gotVal)
	})
}

// postCaptureHandler registers a POST / route.
type postCaptureHandler struct {
	fn func(c internal.Context)
}

func (h *postCaptureHandler) Routes(r internal.Router) {
	r.POST("/", func(c internal.Context) error {
		h.fn(c)
		return nil
	})
}

// --- FromSession tests ---

func TestFromSession(t *testing.T) {
	t.Parallel()

	t.Run("string value", func(t *testing.T) {
		t.Parallel()

		src := internal.FromSession("tenant_id")

		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-1", "tok-1", now.Add(24*time.Hour))
				s.SetValue("tenant_id", "tenant-abc")
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-1"})

		opts := []internal.Option{
			internal.WithSession(store),
		}

		requestVia(t, req, opts, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "tenant-abc", v)
		})
	})

	t.Run("non-string value uses fmt.Sprint", func(t *testing.T) {
		t.Parallel()

		src := internal.FromSession("count")

		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-2", "tok-2", now.Add(24*time.Hour))
				s.SetValue("count", 42)
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-2"})

		opts := []internal.Option{
			internal.WithSession(store),
		}

		requestVia(t, req, opts, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "42", v)
		})
	})

	t.Run("missing key", func(t *testing.T) {
		t.Parallel()

		src := internal.FromSession("nonexistent")

		store := &mockSessionStore{
			getFn: func(_ context.Context, _ string) (*session.Session, error) {
				now := time.Now()
				s := session.New("sess-3", "tok-3", now.Add(24*time.Hour))
				return s, nil
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__sid", Value: "tok-3"})

		opts := []internal.Option{
			internal.WithSession(store),
		}

		requestVia(t, req, opts, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})

	t.Run("no session configured", func(t *testing.T) {
		t.Parallel()

		src := internal.FromSession("key")
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}

// --- FromBearerToken tests ---

func TestFromBearerToken(t *testing.T) {
	t.Parallel()

	t.Run("valid Bearer token", func(t *testing.T) {
		t.Parallel()

		src := internal.FromBearerToken()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer my-token-123")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "my-token-123", v)
		})
	})

	t.Run("case insensitive prefix", func(t *testing.T) {
		t.Parallel()

		src := internal.FromBearerToken()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "BEARER token-upper")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "token-upper", v)
		})
	})

	t.Run("mixed case prefix", func(t *testing.T) {
		t.Parallel()

		src := internal.FromBearerToken()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "bEaReR mixed-token")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.True(t, ok)
			require.Equal(t, "mixed-token", v)
		})
	})

	t.Run("missing Authorization header", func(t *testing.T) {
		t.Parallel()

		src := internal.FromBearerToken()
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})

	t.Run("non-Bearer scheme", func(t *testing.T) {
		t.Parallel()

		src := internal.FromBearerToken()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})

	t.Run("empty token after prefix", func(t *testing.T) {
		t.Parallel()

		src := internal.FromBearerToken()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer ")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})

	t.Run("just Bearer without space", func(t *testing.T) {
		t.Parallel()

		src := internal.FromBearerToken()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer")

		requestVia(t, req, nil, func(c internal.Context) {
			v, ok := src(c)
			require.False(t, ok)
			require.Empty(t, v)
		})
	})
}
