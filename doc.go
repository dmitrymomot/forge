// Package forge provides a simple, opinionated framework for building
// B2B micro-SaaS applications in Go.
//
// Forge is designed around the principle of "no magic" - it generates explicit,
// readable code that you own and can modify. The framework provides a thin
// orchestration layer while keeping business logic in plain Go handlers.
//
// # Quick Start
//
// Create a new application with forge.New(), configure it with options,
// and call Run() to start the HTTP server:
//
//	app := forge.New(
//	    forge.WithLogger(logger),
//	    forge.WithAddress(":8080"),
//	    forge.WithHandlers(
//	        handlers.NewAuth(repo),
//	        handlers.NewPages(repo),
//	    ),
//	)
//
//	if err := app.Run(); err != nil {
//	    log.Fatal(err)
//	}
//
// # Handlers
//
// Handlers implement the [Handler] interface to declare routes:
//
//	type AuthHandler struct {
//	    repo *repository.Queries
//	}
//
//	func NewAuth(repo *repository.Queries) *AuthHandler {
//	    return &AuthHandler{repo: repo}
//	}
//
//	func (h *AuthHandler) Routes(r forge.Router) {
//	    r.GET("/login", h.showLogin)
//	    r.POST("/login", h.handleLogin)
//	    r.POST("/logout", h.handleLogout)
//	}
//
//	func (h *AuthHandler) showLogin(c forge.Context) error {
//	    return c.Render(200, views.LoginPage())
//	}
//
// # Middleware
//
// Middleware wraps handlers to add cross-cutting concerns:
//
//	func Logger(log *slog.Logger) forge.Middleware {
//	    return func(next forge.HandlerFunc) forge.HandlerFunc {
//	        return func(c forge.Context) error {
//	            start := time.Now()
//	            err := next(c)
//	            log.Info("request",
//	                "method", c.Request().Method,
//	                "path", c.Request().URL.Path,
//	                "duration", time.Since(start),
//	            )
//	            return err
//	        }
//	    }
//	}
//
// # Shutdown
//
// The application handles SIGINT/SIGTERM for graceful shutdown.
// Register cleanup functions with WithShutdownHook:
//
//	app := forge.New(
//	    forge.WithShutdownHook(func(ctx context.Context) error {
//	        return pool.Close()
//	    }),
//	)
//
// # Escape Hatch
//
// For advanced use cases requiring raw chi router access, use the
// [github.com/dmitrymomot/forge/pkg/httpserver] package directly:
//
//	router := chi.NewRouter()
//	router.Mount("/legacy", legacyHandler)
//	httpserver.Run(ctx, cfg, router, logger)
package forge
