package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/middlewares"
	"github.com/dmitrymomot/forge/pkg/cookie"
)

type testHandler struct {
	handlerFunc func(forge.Context) error
}

func (h *testHandler) Routes(r forge.Router) {
	r.GET("/test", h.handlerFunc)
	r.POST("/test", h.handlerFunc)
	r.PUT("/test", h.handlerFunc)
	r.PATCH("/test", h.handlerFunc)
	r.DELETE("/test", h.handlerFunc)
	r.HEAD("/test", h.handlerFunc)
	r.OPTIONS("/test", h.handlerFunc)
}

const testCookieSecret = "12345678901234567890123456789012"

func testErrorHandler(c forge.Context, err error) error {
	httpErr := forge.AsHTTPError(err)
	if httpErr != nil {
		http.Error(c.Response(), httpErr.Error(), httpErr.StatusCode())
		return nil
	}
	http.Error(c.Response(), "Internal Server Error", http.StatusInternalServerError)
	return nil
}

func TestDefaultCSRFToken(t *testing.T) {
	t.Parallel()

	t.Run("returns 64-char hex string", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				token := middlewares.GetCSRFToken(c)
				require.Len(t, token, 64, "CSRF token should be 64 characters (32 bytes as hex)")
				// Verify it's valid hex
				for _, ch := range token {
					require.True(t, (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'),
						"token should contain only hex characters")
				}
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("no duplicates in batch of 100", func(t *testing.T) {
		t.Parallel()

		tokens := make(map[string]bool)

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				token := middlewares.GetCSRFToken(c)
				tokens[token] = true
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		for range 100 {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
		}

		require.Len(t, tokens, 100, "all generated tokens should be unique")
	})
}

func TestCSRF_DefaultsApplied(t *testing.T) {
	t.Parallel()

	t.Run("does not panic with zero-value config", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		// Should not panic
		require.NotPanics(t, func() {
			app := forge.New(forge.AppConfig{},
				forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
				forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
				forge.WithHandlers(handler),
			)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
		})
	})
}

func TestCSRF_SafeMethods(t *testing.T) {
	t.Parallel()

	t.Run("GET with no cookie sets signed cookie and token in context", func(t *testing.T) {
		t.Parallel()

		var capturedToken string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedToken = middlewares.GetCSRFToken(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotEmpty(t, capturedToken, "token should be set in context")

		// Check cookie was set
		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, "_csrf", cookies[0].Name)
		require.NotEmpty(t, cookies[0].Value)
		require.Contains(t, cookies[0].Value, ".", "signed cookie should contain a dot separator")
	})

	t.Run("GET with valid signed cookie does not set new cookie", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		var capturedToken string

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedToken = middlewares.GetCSRFToken(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// First request to get the cookie
		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec1 := httptest.NewRecorder()
		app.Router().ServeHTTP(rec1, req1)

		require.Equal(t, http.StatusOK, rec1.Code)
		cookies := rec1.Result().Cookies()
		require.Len(t, cookies, 1)

		// Second request with the cookie
		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.AddCookie(cookies[0])
		rec2 := httptest.NewRecorder()
		app.Router().ServeHTTP(rec2, req2)

		require.Equal(t, http.StatusOK, rec2.Code)
		require.Equal(t, knownToken, capturedToken, "same token should be in context")
		// No Set-Cookie header should be present
		require.Empty(t, rec2.Result().Cookies(), "no new cookie should be set")
	})

	t.Run("GET with invalid cookie generates new token and sets new cookie", func(t *testing.T) {
		t.Parallel()

		var capturedToken string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedToken = middlewares.GetCSRFToken(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		// Add a tampered/invalid cookie
		req.AddCookie(&http.Cookie{Name: "_csrf", Value: "invalid-cookie-value"})
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotEmpty(t, capturedToken, "new token should be generated")

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1, "new cookie should be set")
		require.Equal(t, "_csrf", cookies[0].Name)
	})

	t.Run("HEAD treated same as GET", func(t *testing.T) {
		t.Parallel()

		var capturedToken string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedToken = middlewares.GetCSRFToken(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodHead, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotEmpty(t, capturedToken, "token should be set in context")

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, "_csrf", cookies[0].Name)
	})

	t.Run("OPTIONS treated same as GET", func(t *testing.T) {
		t.Parallel()

		var capturedToken string
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedToken = middlewares.GetCSRFToken(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotEmpty(t, capturedToken, "token should be set in context")

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, "_csrf", cookies[0].Name)
	})

	t.Run("Vary Cookie header present on safe requests", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "Cookie", rec.Header().Get("Vary"))
	})
}

func TestCSRF_UnsafeMethods(t *testing.T) {
	t.Parallel()

	t.Run("POST with matching form token returns 200", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: POST with the cookie and token in form
		form := url.Values{"_csrf": {knownToken}}
		postReq := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(form.Encode()))
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			postReq.AddCookie(c)
		}
		postRec := httptest.NewRecorder()
		app.Router().ServeHTTP(postRec, postReq)

		require.Equal(t, http.StatusOK, postRec.Code)
		require.Equal(t, "ok", postRec.Body.String())
	})

	t.Run("POST with matching header token returns 200", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: POST with the cookie and token in header
		postReq := httptest.NewRequest(http.MethodPost, "/test", nil)
		postReq.Header.Set("X-CSRF-Token", knownToken)
		for _, c := range cookies {
			postReq.AddCookie(c)
		}
		postRec := httptest.NewRecorder()
		app.Router().ServeHTTP(postRec, postReq)

		require.Equal(t, http.StatusOK, postRec.Code)
		require.Equal(t, "ok", postRec.Body.String())
	})

	t.Run("POST with no cookie returns 403", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("POST with no submitted token returns 403", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: POST with the cookie but NO token
		postReq := httptest.NewRequest(http.MethodPost, "/test", nil)
		for _, c := range cookies {
			postReq.AddCookie(c)
		}
		postRec := httptest.NewRecorder()
		app.Router().ServeHTTP(postRec, postReq)

		require.Equal(t, http.StatusForbidden, postRec.Code)
	})

	t.Run("POST with mismatched token returns 403", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: POST with the cookie but wrong token
		form := url.Values{"_csrf": {"wrong-token"}}
		postReq := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(form.Encode()))
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			postReq.AddCookie(c)
		}
		postRec := httptest.NewRecorder()
		app.Router().ServeHTTP(postRec, postReq)

		require.Equal(t, http.StatusForbidden, postRec.Code)
	})

	t.Run("POST with tampered cookie returns 403", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{})),
			forge.WithHandlers(handler),
		)

		// POST with a tampered cookie
		form := url.Values{"_csrf": {"some-token"}}
		postReq := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(form.Encode()))
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postReq.AddCookie(&http.Cookie{Name: "_csrf", Value: "tampered.signature"})
		postRec := httptest.NewRecorder()
		app.Router().ServeHTTP(postRec, postReq)

		require.Equal(t, http.StatusForbidden, postRec.Code)
	})

	t.Run("PUT same as POST with matching token returns 200", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: PUT with the cookie and token
		form := url.Values{"_csrf": {knownToken}}
		putReq := httptest.NewRequest(http.MethodPut, "/test", strings.NewReader(form.Encode()))
		putReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			putReq.AddCookie(c)
		}
		putRec := httptest.NewRecorder()
		app.Router().ServeHTTP(putRec, putReq)

		require.Equal(t, http.StatusOK, putRec.Code)
		require.Equal(t, "ok", putRec.Body.String())
	})

	t.Run("PATCH same as POST with matching token returns 200", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: PATCH with the cookie and token
		form := url.Values{"_csrf": {knownToken}}
		patchReq := httptest.NewRequest(http.MethodPatch, "/test", strings.NewReader(form.Encode()))
		patchReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			patchReq.AddCookie(c)
		}
		patchRec := httptest.NewRecorder()
		app.Router().ServeHTTP(patchRec, patchReq)

		require.Equal(t, http.StatusOK, patchRec.Code)
		require.Equal(t, "ok", patchRec.Body.String())
	})

	t.Run("DELETE same as POST with matching token returns 200", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: DELETE with the cookie and token in header
		deleteReq := httptest.NewRequest(http.MethodDelete, "/test", nil)
		deleteReq.Header.Set("X-CSRF-Token", knownToken)
		for _, c := range cookies {
			deleteReq.AddCookie(c)
		}
		deleteRec := httptest.NewRecorder()
		app.Router().ServeHTTP(deleteRec, deleteReq)

		require.Equal(t, http.StatusOK, deleteRec.Code)
		require.Equal(t, "ok", deleteRec.Body.String())
	})

	t.Run("form field takes priority over header when both present", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		// Step 1: GET to establish the cookie
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		cookies := rec.Result().Cookies()

		// Step 2: POST with correct form token but wrong header token
		form := url.Values{"_csrf": {knownToken}}
		postReq := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(form.Encode()))
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postReq.Header.Set("X-CSRF-Token", "wrong-header-token")
		for _, c := range cookies {
			postReq.AddCookie(c)
		}
		postRec := httptest.NewRecorder()
		app.Router().ServeHTTP(postRec, postReq)

		// Should succeed because form field takes priority
		require.Equal(t, http.StatusOK, postRec.Code)
		require.Equal(t, "ok", postRec.Body.String())
	})
}

func TestCSRF_Options(t *testing.T) {
	t.Parallel()

	t.Run("custom token generator is called", func(t *testing.T) {
		t.Parallel()

		customToken := "my-custom-csrf-token"
		var generatorCalled bool

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				token := middlewares.GetCSRFToken(c)
				require.Equal(t, customToken, token)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string {
					generatorCalled = true
					return customToken
				}),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, generatorCalled, "custom token generator should be called")
	})

	t.Run("custom error handler receives the error on validation failure", func(t *testing.T) {
		t.Parallel()

		var receivedError error
		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithErrorHandler(testErrorHandler),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFErrorHandler(func(c forge.Context, err error) error {
					receivedError = err
					return c.Error(http.StatusTeapot, "custom error response")
				}),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusTeapot, rec.Code)
		require.NotNil(t, receivedError, "custom error handler should receive the error")
		require.Contains(t, receivedError.Error(), "csrf cookie missing")
	})

	t.Run("skip function returning true bypasses all CSRF validation for POST", func(t *testing.T) {
		t.Parallel()

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFSkipFunc(func(c forge.Context) bool {
					// Skip CSRF for all requests in this test
					return true
				}),
			)),
			forge.WithHandlers(handler),
		)

		// POST with no cookie and no token should succeed because skip function returns true
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "ok", rec.Body.String())
	})
}

func TestCSRF_GetCSRFToken(t *testing.T) {
	t.Parallel()

	t.Run("returns token from context after GET", func(t *testing.T) {
		t.Parallel()

		knownToken := "test-csrf-token-value"
		var capturedToken string

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedToken = middlewares.GetCSRFToken(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithMiddleware(middlewares.CSRF(middlewares.CSRFConfig{},
				middlewares.WithCSRFTokenGenerator(func() string { return knownToken }),
			)),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, knownToken, capturedToken)
	})

	t.Run("returns empty string when no CSRF middleware", func(t *testing.T) {
		t.Parallel()

		var capturedToken string

		handler := &testHandler{
			handlerFunc: func(c forge.Context) error {
				capturedToken = middlewares.GetCSRFToken(c)
				return c.String(http.StatusOK, "ok")
			},
		}

		// App without CSRF middleware
		app := forge.New(forge.AppConfig{},
			forge.WithCookieConfig(cookie.Config{Secret: testCookieSecret}),
			forge.WithHandlers(handler),
		)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, capturedToken, "should return empty string when no CSRF middleware")
	})
}
