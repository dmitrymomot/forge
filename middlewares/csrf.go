package middlewares

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/dmitrymomot/forge/internal"
)

type csrfTokenKey struct{}

// CSRFConfig configures the CSRF middleware.
type CSRFConfig struct {
	CookieName string `env:"CSRF_COOKIE_NAME" envDefault:"_csrf"`
	HeaderName string `env:"CSRF_HEADER_NAME" envDefault:"X-CSRF-Token"`
	FieldName  string `env:"CSRF_FIELD_NAME"  envDefault:"_csrf"`
	MaxAge     int    `env:"CSRF_MAX_AGE"     envDefault:"86400"`
}

type csrfOptions struct {
	tokenGenerator func() string
	errorHandler   func(internal.Context, error) error
	skipFunc       func(internal.Context) bool
}

// CSRFOption configures runtime dependencies for the CSRF middleware.
type CSRFOption func(*csrfOptions)

var (
	errCSRFCookieMissing = errors.New("csrf cookie missing or invalid")
	errCSRFTokenMissing  = errors.New("csrf token missing")
	errCSRFTokenMismatch = errors.New("csrf token mismatch")
)

// WithCSRFTokenGenerator sets a custom token generator function.
func WithCSRFTokenGenerator(fn func() string) CSRFOption {
	return func(o *csrfOptions) {
		o.tokenGenerator = fn
	}
}

// WithCSRFErrorHandler sets a custom error handler for CSRF validation failures.
func WithCSRFErrorHandler(fn func(internal.Context, error) error) CSRFOption {
	return func(o *csrfOptions) {
		o.errorHandler = fn
	}
}

// WithCSRFSkipFunc sets a function to skip CSRF validation for specific requests.
func WithCSRFSkipFunc(fn func(internal.Context) bool) CSRFOption {
	return func(o *csrfOptions) {
		o.skipFunc = fn
	}
}

// CSRF returns middleware that protects against Cross-Site Request Forgery attacks.
// It uses the double-submit cookie pattern with signed cookies.
func CSRF(cfg CSRFConfig, opts ...CSRFOption) internal.Middleware {
	o := &csrfOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if cfg.CookieName == "" {
		cfg.CookieName = "_csrf"
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-CSRF-Token"
	}
	if cfg.FieldName == "" {
		cfg.FieldName = "_csrf"
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 86400
	}

	if o.tokenGenerator == nil {
		o.tokenGenerator = defaultCSRFToken
	}
	if o.errorHandler == nil {
		o.errorHandler = func(c internal.Context, err error) error {
			return c.Error(http.StatusForbidden, err.Error())
		}
	}

	safeMethods := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
		http.MethodTrace:   true,
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			if o.skipFunc != nil && o.skipFunc(c) {
				return next(c)
			}

			token, err := c.CookieSigned(cfg.CookieName)
			if err != nil {
				token = ""
			}

			if safeMethods[c.Request().Method] {
				if token == "" {
					token = o.tokenGenerator()
					if err := c.SetCookieSigned(cfg.CookieName, token, cfg.MaxAge); err != nil {
						return err
					}
				}
				c.Set(csrfTokenKey{}, token)
				c.Response().Header().Add("Vary", "Cookie")
				return next(c)
			}

			if token == "" {
				return o.errorHandler(c, errCSRFCookieMissing)
			}

			submitted := c.Form(cfg.FieldName)
			if submitted == "" {
				submitted = c.Header(cfg.HeaderName)
			}
			if submitted == "" {
				return o.errorHandler(c, errCSRFTokenMissing)
			}

			if subtle.ConstantTimeCompare([]byte(token), []byte(submitted)) != 1 {
				return o.errorHandler(c, errCSRFTokenMismatch)
			}

			c.Set(csrfTokenKey{}, token)
			c.Response().Header().Add("Vary", "Cookie")
			return next(c)
		}
	}
}

// GetCSRFToken extracts the CSRF token from the context.
// Returns an empty string if no token is set.
func GetCSRFToken(c internal.Context) string {
	if v, ok := c.Get(csrfTokenKey{}).(string); ok {
		return v
	}
	return ""
}

func defaultCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("csrf: failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
