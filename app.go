package forge

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/pkg/hostrouter"
	"github.com/go-chi/chi/v5"
)

// Default server timeouts (hardcoded, opinionated).
const (
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultReadHeaderTimeout = 5 * time.Second
	defaultMaxHeaderBytes    = 1 << 20 // 1MB
)

// HostRoutes is an alias for hostrouter.Routes for convenience.
// Maps host patterns to HTTP handlers.
// Patterns: "api.example.com" (exact) or "*.example.com" (wildcard)
type HostRoutes = hostrouter.Routes

// ErrorHandler handles errors returned from handlers.
type ErrorHandler func(Context, error) error

// App orchestrates the application lifecycle.
// It manages HTTP routing, middleware, and graceful shutdown.
// App is immutable after creation - all configuration is done via New().
type App struct {
	// Base context for signal handling (defaults to context.Background())
	baseCtx context.Context

	// Logging
	logger *slog.Logger

	// HTTP server
	server       *http.Server
	router       chi.Router
	listener     net.Listener // set during Run()
	middlewares  []Middleware
	handlers     []Handler
	staticRoutes []staticRoute

	// Host-based routing (wraps router as middleware)
	hostRoutes HostRoutes

	// Error handling
	errorHandler            ErrorHandler
	notFoundHandler         HandlerFunc
	methodNotAllowedHandler HandlerFunc

	// Health checks
	healthConfig *healthConfig

	// Background jobs (River) - future
	// riverClient  *river.Client[pgx.Tx]
	// workers      []river.Worker

	// Scheduled tasks (River periodic) - future
	// periodicJobs []*river.PeriodicJob

	// Lifecycle
	shutdownTimeout time.Duration
	shutdownHooks   []func(ctx context.Context) error
	done            chan struct{} // for programmatic shutdown via Stop()
}

// staticRoute represents a static file handler mount point.
type staticRoute struct {
	pattern string
	handler http.Handler
}

// New creates a new application with the given options.
// The App is immutable after creation.
//
// Example:
//
//	app := forge.New(
//	    forge.WithLogger(logger),
//	    forge.WithAddress(":8080"),
//	    forge.WithMiddleware(middlewares.Logger(log)),
//	    forge.WithHandlers(
//	        handlers.NewAuth(repo),
//	        handlers.NewPages(repo),
//	    ),
//	)
func New(opts ...Option) *App {
	router := chi.NewRouter()

	a := &App{
		router:          router,
		shutdownTimeout: 30 * time.Second,
		done:            make(chan struct{}),
		server: &http.Server{
			Addr:              ":8080",
			Handler:           router,
			ReadTimeout:       defaultReadTimeout,
			WriteTimeout:      defaultWriteTimeout,
			IdleTimeout:       defaultIdleTimeout,
			ReadHeaderTimeout: defaultReadHeaderTimeout,
			MaxHeaderBytes:    defaultMaxHeaderBytes,
		},
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

// Addr returns the server's listening address.
// Returns empty string if the server hasn't started yet.
func (a *App) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}
