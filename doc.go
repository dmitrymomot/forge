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
//	    forge.WithHandlers(
//	        handlers.NewAuth(repo),
//	        handlers.NewPages(repo),
//	    ),
//	)
//
//	if err := app.Run(":8080", forge.Logger(logger)); err != nil {
//	    log.Fatal(err)
//	}
//
// # Multi-Domain Routing
//
// For applications that need host-based routing, compose multiple Apps
// with forge.Run():
//
//	api := forge.New(
//	    forge.WithHandlers(handlers.NewAPIHandler()),
//	)
//
//	website := forge.New(
//	    forge.WithHandlers(handlers.NewLandingHandler()),
//	)
//
//	if err := forge.Run(
//	    forge.Domain("api.acme.com", api),
//	    forge.Domain("*.acme.com", website),
//	    forge.Address(":8080"),
//	    forge.Logger(logger),
//	); err != nil {
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
// Register cleanup functions with ShutdownHook:
//
//	app.Run(":8080",
//	    forge.ShutdownHook(func(ctx context.Context) error {
//	        return pool.Close()
//	    }),
//	)
//
// # Testing
//
// For testing, use httptest.NewServer with the app's Router():
//
//	app := forge.New(forge.WithHandlers(myHandler))
//	ts := httptest.NewServer(app.Router())
//	defer ts.Close()
//
//	resp, err := http.Get(ts.URL + "/path")
package forge
