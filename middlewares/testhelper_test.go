package middlewares_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/htmx"
	"github.com/dmitrymomot/forge/pkg/job"
	"github.com/dmitrymomot/forge/pkg/session"
	"github.com/dmitrymomot/forge/pkg/storage"
	"github.com/dmitrymomot/forge/pkg/validator"
)

type testContext struct {
	response http.ResponseWriter
	request  *http.Request
	values   map[any]any
}

func newTestContext(w http.ResponseWriter, r *http.Request) *testContext {
	return &testContext{
		response: w,
		request:  r,
		values:   make(map[any]any),
	}
}

func (c *testContext) Request() *http.Request        { return c.request }
func (c *testContext) Response() http.ResponseWriter { return c.response }
func (c *testContext) Context() context.Context      { return c.request.Context() }
func (c *testContext) Param(name string) string      { return "" }

func (c *testContext) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

func (c *testContext) QueryDefault(name, defaultValue string) string {
	v := c.request.URL.Query().Get(name)
	if v == "" {
		return defaultValue
	}
	return v
}

func (c *testContext) Domain() string               { return c.request.Host }
func (c *testContext) Subdomain() string            { return "" }
func (c *testContext) Header(name string) string    { return c.request.Header.Get(name) }
func (c *testContext) SetHeader(name, value string) { c.response.Header().Set(name, value) }
func (c *testContext) JSON(code int, v any) error   { c.response.WriteHeader(code); return nil }
func (c *testContext) String(code int, s string) error {
	c.response.WriteHeader(code)
	_, err := c.response.Write([]byte(s))
	return err
}
func (c *testContext) NoContent(code int) error { c.response.WriteHeader(code); return nil }
func (c *testContext) Redirect(code int, url string) error {
	http.Redirect(c.response, c.request, url, code)
	return nil
}
func (c *testContext) IsHTMX() bool                      { return htmx.IsHTMX(c.request) }
func (c *testContext) Written() bool                     { return false }
func (c *testContext) Logger() *slog.Logger              { return slog.Default() }
func (c *testContext) LogDebug(msg string, attrs ...any) {}
func (c *testContext) LogInfo(msg string, attrs ...any)  {}
func (c *testContext) LogWarn(msg string, attrs ...any)  {}
func (c *testContext) LogError(msg string, attrs ...any) {}

func (c *testContext) Error(code int, message string, opts ...internal.HTTPErrorOption) *internal.HTTPError {
	err := internal.NewHTTPError(code, message)
	for _, opt := range opts {
		opt(err)
	}
	return err
}

func (c *testContext) Render(code int, component internal.Component, opts ...htmx.RenderOption) error {
	c.response.WriteHeader(code)
	return component.Render(c.request.Context(), c.response)
}

func (c *testContext) RenderPartial(code int, fullPage, partial internal.Component, opts ...htmx.RenderOption) error {
	if htmx.IsHTMX(c.request) {
		return c.Render(code, partial, opts...)
	}
	return c.Render(code, fullPage)
}

func (c *testContext) Bind(v any) (validator.ValidationErrors, error)      { return nil, nil }
func (c *testContext) BindQuery(v any) (validator.ValidationErrors, error) { return nil, nil }
func (c *testContext) BindJSON(v any) (validator.ValidationErrors, error)  { return nil, nil }

func (c *testContext) Set(key, value any) {
	c.values[key] = value
	// Also store in request context for context extractors
	ctx := context.WithValue(c.request.Context(), key, value)
	c.request = c.request.WithContext(ctx)
}

func (c *testContext) Get(key any) any {
	return c.values[key]
}

func (c *testContext) Cookie(name string) (string, error) {
	cookie, err := c.request.Cookie(name)
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

func (c *testContext) SetCookie(name, value string, maxAge int) {
	http.SetCookie(c.response, &http.Cookie{
		Name:   name,
		Value:  value,
		MaxAge: maxAge,
	})
}

func (c *testContext) DeleteCookie(name string) {
	http.SetCookie(c.response, &http.Cookie{
		Name:   name,
		MaxAge: -1,
	})
}

func (c *testContext) CookieSigned(name string) (string, error)                          { return "", nil }
func (c *testContext) SetCookieSigned(name, value string, maxAge int) error              { return nil }
func (c *testContext) CookieEncrypted(name string) (string, error)                       { return "", nil }
func (c *testContext) SetCookieEncrypted(name, value string, maxAge int) error           { return nil }
func (c *testContext) Flash(key string, dest any) error                                  { return nil }
func (c *testContext) SetFlash(key string, value any) error                              { return nil }
func (c *testContext) Session() (*session.Session, error)                                { return nil, nil }
func (c *testContext) InitSession() error                                                { return nil }
func (c *testContext) AuthenticateSession(userID string) error                           { return nil }
func (c *testContext) SessionValue(key string) (any, error)                              { return nil, nil }
func (c *testContext) SetSessionValue(key string, val any) error                         { return nil }
func (c *testContext) DeleteSessionValue(key string) error                               { return nil }
func (c *testContext) DestroySession() error                                             { return nil }
func (c *testContext) ResponseWriter() *internal.ResponseWriter                          { return nil }
func (c *testContext) Enqueue(name string, payload any, opts ...job.EnqueueOption) error { return nil }
func (c *testContext) EnqueueTx(tx pgx.Tx, name string, payload any, opts ...job.EnqueueOption) error {
	return nil
}
func (c *testContext) Storage() (storage.Storage, error) { return nil, nil }
func (c *testContext) Upload(r io.Reader, size int64, opts ...storage.Option) (*storage.FileInfo, error) {
	return nil, nil
}
func (c *testContext) Download(key string) (io.ReadCloser, error)                    { return nil, nil }
func (c *testContext) DeleteFile(key string) error                                   { return nil }
func (c *testContext) FileURL(key string, opts ...storage.URLOption) (string, error) { return "", nil }
func (c *testContext) Deadline() (time.Time, bool)                                   { return c.request.Context().Deadline() }
func (c *testContext) Done() <-chan struct{}                                         { return c.request.Context().Done() }
func (c *testContext) Err() error                                                    { return c.request.Context().Err() }
func (c *testContext) Value(key any) any                                             { return c.request.Context().Value(key) }
func (c *testContext) UserID() string                                                { return "" }
func (c *testContext) IsAuthenticated() bool                                         { return false }
func (c *testContext) IsCurrentUser(id string) bool                                  { return false }
func (c *testContext) Can(permission internal.Permission) bool                       { return false }
