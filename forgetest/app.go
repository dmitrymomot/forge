package forgetest

import (
	"testing"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/pkg/cookie"
)

// testCookieSecret is a 32-byte secret for test cookie encryption/signing.
const testCookieSecret = "forgetest-secret-key-32-bytes!!!" // exactly 32 bytes

// roleDataKey is the session data key used to store the role for RBAC testing.
const roleDataKey = "__forgetest_role"

// App wraps a forge.App with test helpers.
type App struct {
	app   *forge.App
	store *MemoryStore
}

// Option configures the test App.
type Option func(*appConfig)

type appConfig struct {
	roles        forge.RolePermissions
	middlewares  []forge.Middleware
	errorHandler forge.ErrorHandler
	extraOpts    []forge.Option
}

// WithRoles enables RBAC with the given permissions.
// Roles are read from session data key "__forgetest_role",
// set via Request.WithRole().
func WithRoles(perms forge.RolePermissions) Option {
	return func(c *appConfig) {
		c.roles = perms
	}
}

// WithMiddleware adds middleware to the test app.
func WithMiddleware(mw ...forge.Middleware) Option {
	return func(c *appConfig) {
		c.middlewares = append(c.middlewares, mw...)
	}
}

// WithErrorHandler sets a custom error handler.
func WithErrorHandler(h forge.ErrorHandler) Option {
	return func(c *appConfig) {
		c.errorHandler = h
	}
}

// WithOption passes through an arbitrary forge.Option for extensibility.
func WithOption(opt forge.Option) Option {
	return func(c *appConfig) {
		c.extraOpts = append(c.extraOpts, opt)
	}
}

// NewApp creates a forge.App wired for testing with an in-memory session store.
func NewApp(t testing.TB, handler forge.Handler, opts ...Option) *App {
	t.Helper()

	cfg := &appConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	store := newMemoryStore()

	forgeOpts := []forge.Option{
		forge.WithCookieConfig(cookie.Config{
			Secret: testCookieSecret,
		}),
		forge.WithSession(store),
		forge.WithHandlers(handler),
	}

	if cfg.roles != nil {
		extractor := func(c forge.Context) string {
			val, err := c.SessionValue(roleDataKey)
			if err != nil {
				return ""
			}
			role, ok := val.(string)
			if !ok {
				return ""
			}
			return role
		}
		forgeOpts = append(forgeOpts, forge.WithRoles(cfg.roles, extractor))
	}

	if len(cfg.middlewares) > 0 {
		forgeOpts = append(forgeOpts, forge.WithMiddleware(cfg.middlewares...))
	}

	if cfg.errorHandler != nil {
		forgeOpts = append(forgeOpts, forge.WithErrorHandler(cfg.errorHandler))
	}

	forgeOpts = append(forgeOpts, cfg.extraOpts...)

	app := forge.New(forge.AppConfig{}, forgeOpts...)

	return &App{
		app:   app,
		store: store,
	}
}

// Store returns the in-memory session store for direct inspection in tests.
func (a *App) Store() *MemoryStore {
	return a.store
}
