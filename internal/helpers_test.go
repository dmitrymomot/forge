package internal_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/htmx"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/storage"
	"github.com/dmitrymomot/forge/pkg/validator"
)

type paramContext struct {
	params  map[string]string
	request *http.Request
	values  map[any]any
}

func newParamContext(params map[string]string, queryString string) *paramContext {
	url := "/"
	if queryString != "" {
		url = "/?" + queryString
	}
	return &paramContext{
		params:  params,
		request: httptest.NewRequest(http.MethodGet, url, nil),
		values:  make(map[any]any),
	}
}

func (c *paramContext) Param(name string) string                 { return c.params[name] }
func (c *paramContext) Query(name string) string                 { return c.request.URL.Query().Get(name) }
func (c *paramContext) QueryDefault(name, def string) string     { return "" }
func (c *paramContext) Request() *http.Request                   { return c.request }
func (c *paramContext) Response() http.ResponseWriter            { return httptest.NewRecorder() }
func (c *paramContext) Context() context.Context                 { return c.request.Context() }
func (c *paramContext) Deadline() (time.Time, bool)              { return c.request.Context().Deadline() }
func (c *paramContext) Done() <-chan struct{}                    { return c.request.Context().Done() }
func (c *paramContext) Err() error                               { return c.request.Context().Err() }
func (c *paramContext) Value(key any) any                        { return c.request.Context().Value(key) }
func (c *paramContext) Domain() string                           { return "" }
func (c *paramContext) Subdomain() string                        { return "" }
func (c *paramContext) Header(name string) string                { return "" }
func (c *paramContext) SetHeader(name, value string)             {}
func (c *paramContext) JSON(code int, v any) error               { return nil }
func (c *paramContext) String(code int, s string) error          { return nil }
func (c *paramContext) NoContent(code int) error                 { return nil }
func (c *paramContext) Redirect(code int, url string) error      { return nil }
func (c *paramContext) IsHTMX() bool                             { return false }
func (c *paramContext) Written() bool                            { return false }
func (c *paramContext) Logger() *slog.Logger                     { return slog.Default() }
func (c *paramContext) LogDebug(msg string, attrs ...any)        {}
func (c *paramContext) LogInfo(msg string, attrs ...any)         {}
func (c *paramContext) LogWarn(msg string, attrs ...any)         {}
func (c *paramContext) LogError(msg string, attrs ...any)        {}
func (c *paramContext) Set(key, value any)                       { c.values[key] = value }
func (c *paramContext) Get(key any) any                          { return c.values[key] }
func (c *paramContext) Cookie(name string) (string, error)       { return "", nil }
func (c *paramContext) SetCookie(name, value string, maxAge int) {}
func (c *paramContext) DeleteCookie(name string)                 {}
func (c *paramContext) UserID() string                           { return "" }
func (c *paramContext) IsAuthenticated() bool                    { return false }
func (c *paramContext) IsCurrentUser(id string) bool             { return false }
func (c *paramContext) Can(permission internal.Permission) bool  { return false }

func (c *paramContext) Error(code int, message string, opts ...internal.HTTPErrorOption) *internal.HTTPError {
	return internal.NewHTTPError(code, message)
}

func (c *paramContext) Render(code int, component internal.Component, opts ...htmx.RenderOption) error {
	return nil
}

func (c *paramContext) RenderPartial(code int, fullPage, partial internal.Component, opts ...htmx.RenderOption) error {
	return nil
}

func (c *paramContext) Bind(v any) (validator.ValidationErrors, error)      { return nil, nil }
func (c *paramContext) BindQuery(v any) (validator.ValidationErrors, error) { return nil, nil }
func (c *paramContext) BindJSON(v any) (validator.ValidationErrors, error)  { return nil, nil }

func (c *paramContext) CookieSigned(name string) (string, error)                          { return "", nil }
func (c *paramContext) SetCookieSigned(name, value string, maxAge int) error              { return nil }
func (c *paramContext) CookieEncrypted(name string) (string, error)                       { return "", nil }
func (c *paramContext) SetCookieEncrypted(name, value string, maxAge int) error           { return nil }
func (c *paramContext) Flash(key string, dest any) error                                  { return nil }
func (c *paramContext) SetFlash(key string, value any) error                              { return nil }
func (c *paramContext) Session() (*session.Session, error)                                { return nil, nil }
func (c *paramContext) InitSession() error                                                { return nil }
func (c *paramContext) AuthenticateSession(userID string) error                           { return nil }
func (c *paramContext) SessionValue(key string) (any, error)                              { return nil, nil }
func (c *paramContext) SetSessionValue(key string, val any) error                         { return nil }
func (c *paramContext) DeleteSessionValue(key string) error                               { return nil }
func (c *paramContext) DestroySession() error                                             { return nil }
func (c *paramContext) ResponseWriter() *internal.ResponseWriter                          { return nil }
func (c *paramContext) Enqueue(name string, payload any, opts ...job.EnqueueOption) error { return nil }
func (c *paramContext) EnqueueTx(tx pgx.Tx, name string, payload any, opts ...job.EnqueueOption) error {
	return nil
}
func (c *paramContext) Storage() (storage.Storage, error) { return nil, nil }
func (c *paramContext) Upload(r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error) {
	return nil, nil
}
func (c *paramContext) Download(key string) (io.ReadCloser, error)                    { return nil, nil }
func (c *paramContext) DeleteFile(key string) error                                   { return nil }
func (c *paramContext) FileURL(key string, opts ...storage.URLOption) (string, error) { return "", nil }

func TestParam(t *testing.T) {
	t.Parallel()

	t.Run("string", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			raw  string
			want string
		}{
			{"non-empty", "hello", "hello"},
			{"empty", "", ""},
			{"with spaces", "hello world", "hello world"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(map[string]string{"val": tt.raw}, "")
				require.Equal(t, tt.want, internal.Param[string](c, "val"))
			})
		}
	})

	t.Run("int", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			raw  string
			want int
		}{
			{"positive", "42", 42},
			{"negative", "-7", -7},
			{"zero", "0", 0},
			{"empty returns zero", "", 0},
			{"invalid returns zero", "abc", 0},
			{"float string returns zero", "3.14", 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(map[string]string{"val": tt.raw}, "")
				require.Equal(t, tt.want, internal.Param[int](c, "val"))
			})
		}
	})

	t.Run("int64", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			raw  string
			want int64
		}{
			{"positive", "9999999999", 9999999999},
			{"negative", "-100", -100},
			{"zero", "0", 0},
			{"empty returns zero", "", 0},
			{"invalid returns zero", "not-a-number", 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(map[string]string{"val": tt.raw}, "")
				require.Equal(t, tt.want, internal.Param[int64](c, "val"))
			})
		}
	})

	t.Run("float64", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			raw  string
			want float64
		}{
			{"positive", "3.14", 3.14},
			{"negative", "-2.5", -2.5},
			{"integer string", "42", 42.0},
			{"zero", "0", 0.0},
			{"empty returns zero", "", 0.0},
			{"invalid returns zero", "abc", 0.0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(map[string]string{"val": tt.raw}, "")
				require.InDelta(t, tt.want, internal.Param[float64](c, "val"), 0.001)
			})
		}
	})

	t.Run("bool", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			raw  string
			want bool
		}{
			{"true", "true", true},
			{"1", "1", true},
			{"false", "false", false},
			{"0", "0", false},
			{"TRUE", "TRUE", true},
			{"empty returns false", "", false},
			{"invalid returns false", "maybe", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(map[string]string{"val": tt.raw}, "")
				require.Equal(t, tt.want, internal.Param[bool](c, "val"))
			})
		}
	})

	t.Run("missing param returns zero value", func(t *testing.T) {
		t.Parallel()

		c := newParamContext(map[string]string{}, "")
		require.Equal(t, "", internal.Param[string](c, "missing"))
		require.Equal(t, 0, internal.Param[int](c, "missing"))
		require.Equal(t, int64(0), internal.Param[int64](c, "missing"))
		require.InDelta(t, 0.0, internal.Param[float64](c, "missing"), 0.001)
		require.Equal(t, false, internal.Param[bool](c, "missing"))
	})
}

func TestQuery(t *testing.T) {
	t.Parallel()

	t.Run("string", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			query string
			want  string
		}{
			{"non-empty", "val=hello", "hello"},
			{"missing key", "", ""},
			{"empty value", "val=", ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(nil, tt.query)
				require.Equal(t, tt.want, internal.Query[string](c, "val"))
			})
		}
	})

	t.Run("int", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			query string
			want  int
		}{
			{"positive", "page=5", 5},
			{"zero", "page=0", 0},
			{"negative", "page=-1", -1},
			{"missing returns zero", "", 0},
			{"invalid returns zero", "page=abc", 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(nil, tt.query)
				require.Equal(t, tt.want, internal.Query[int](c, "page"))
			})
		}
	})

	t.Run("int64", func(t *testing.T) {
		t.Parallel()

		c := newParamContext(nil, "id=9876543210")
		require.Equal(t, int64(9876543210), internal.Query[int64](c, "id"))
	})

	t.Run("float64", func(t *testing.T) {
		t.Parallel()

		c := newParamContext(nil, "price=19.99")
		require.InDelta(t, 19.99, internal.Query[float64](c, "price"), 0.001)
	})

	t.Run("bool", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			query string
			want  bool
		}{
			{"true", "verbose=true", true},
			{"1", "verbose=1", true},
			{"false", "verbose=false", false},
			{"missing returns false", "", false},
			{"invalid returns false", "verbose=yes", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				c := newParamContext(nil, tt.query)
				require.Equal(t, tt.want, internal.Query[bool](c, "verbose"))
			})
		}
	})
}

func TestQueryDefault(t *testing.T) {
	t.Parallel()

	t.Run("returns default when missing", func(t *testing.T) {
		t.Parallel()

		c := newParamContext(nil, "")
		require.Equal(t, 1, internal.QueryDefault[int](c, "page", 1))
		require.Equal(t, "default", internal.QueryDefault[string](c, "name", "default"))
		require.Equal(t, int64(100), internal.QueryDefault[int64](c, "id", 100))
		require.InDelta(t, 9.99, internal.QueryDefault[float64](c, "price", 9.99), 0.001)
		require.Equal(t, true, internal.QueryDefault[bool](c, "flag", true))
	})

	t.Run("returns parsed value when present", func(t *testing.T) {
		t.Parallel()

		c := newParamContext(nil, "page=5&name=hello&id=200&price=19.99&flag=false")
		require.Equal(t, 5, internal.QueryDefault[int](c, "page", 1))
		require.Equal(t, "hello", internal.QueryDefault[string](c, "name", "default"))
		require.Equal(t, int64(200), internal.QueryDefault[int64](c, "id", 100))
		require.InDelta(t, 19.99, internal.QueryDefault[float64](c, "price", 9.99), 0.001)
		require.Equal(t, false, internal.QueryDefault[bool](c, "flag", true))
	})

	t.Run("returns default when empty value", func(t *testing.T) {
		t.Parallel()

		c := newParamContext(nil, "page=")
		require.Equal(t, 1, internal.QueryDefault[int](c, "page", 1))
	})

	t.Run("returns default on invalid when present", func(t *testing.T) {
		t.Parallel()

		// When the query param is present but unparseable, QueryDefault
		// returns the default value.
		c := newParamContext(nil, "page=abc")
		require.Equal(t, 1, internal.QueryDefault[int](c, "page", 1))
	})
}

func TestContextValue(t *testing.T) {
	t.Parallel()

	t.Run("returns correct typed value when key exists", func(t *testing.T) {
		t.Parallel()

		type key struct{}
		c := newParamContext(nil, "")
		c.Set(key{}, "hello")

		require.Equal(t, "hello", internal.ContextValue[string](c, key{}))
	})

	t.Run("returns zero value for wrong type", func(t *testing.T) {
		t.Parallel()

		type key struct{}
		c := newParamContext(nil, "")
		c.Set(key{}, 42) // stored as int

		require.Equal(t, "", internal.ContextValue[string](c, key{}))
	})

	t.Run("returns zero value for missing key", func(t *testing.T) {
		t.Parallel()

		type key struct{}
		c := newParamContext(nil, "")

		require.Equal(t, "", internal.ContextValue[string](c, key{}))
		require.Equal(t, 0, internal.ContextValue[int](c, key{}))
		require.Equal(t, false, internal.ContextValue[bool](c, key{}))
	})

	t.Run("works with custom struct types", func(t *testing.T) {
		t.Parallel()

		type key struct{}
		type user struct {
			Name string
			Age  int
		}

		c := newParamContext(nil, "")
		c.Set(key{}, user{Name: "Alice", Age: 30})

		got := internal.ContextValue[user](c, key{})
		require.Equal(t, "Alice", got.Name)
		require.Equal(t, 30, got.Age)
	})

	t.Run("returns zero struct for missing custom type", func(t *testing.T) {
		t.Parallel()

		type key struct{}
		type user struct {
			Name string
			Age  int
		}

		c := newParamContext(nil, "")

		got := internal.ContextValue[user](c, key{})
		require.Equal(t, user{}, got)
	})
}
