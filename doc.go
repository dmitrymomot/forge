// Package forge provides a simple, opinionated framework for building
// B2B micro-SaaS applications in Go.
//
// Forge is designed around the principle of "no magic" - it uses explicit,
// readable code with no reflection or service containers. The framework
// provides a thin orchestration layer while keeping business logic in
// plain Go handlers.
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
// # Context as context.Context
//
// The [Context] interface embeds [context.Context], so it can be passed
// directly to any function that expects a standard library context:
//
//	func (h *Handler) getUser(c forge.Context) error {
//	    // c satisfies context.Context — pass it to DB calls, HTTP clients, etc.
//	    user, err := h.repo.GetUser(c, userID)
//	    if err != nil {
//	        return err
//	    }
//	    return c.JSON(200, user)
//	}
//
// # Identity and Authentication
//
// Context provides convenience methods for checking the current user.
// These are shortcuts over the session system and return safe defaults
// when no session is configured:
//
//	func (h *Handler) showProfile(c forge.Context) error {
//	    if !c.IsAuthenticated() {
//	        return c.Redirect(302, "/login")
//	    }
//
//	    user, err := h.repo.GetUser(c, c.UserID())
//	    if err != nil {
//	        return err
//	    }
//
//	    // Only allow users to edit their own profile
//	    canEdit := c.IsCurrentUser(user.ID)
//	    return c.Render(200, views.Profile(user, canEdit))
//	}
//
// # Role-Based Access Control (RBAC)
//
// Configure permissions with [WithRoles]. The role extractor is called
// lazily on the first [Context.Can] call and cached for the request:
//
//	app := forge.New(
//	    forge.WithRoles(
//	        forge.RolePermissions{
//	            "admin":  {"users.read", "users.write", "billing.manage"},
//	            "member": {"users.read"},
//	        },
//	        func(c forge.Context) string {
//	            return forge.ContextValue[string](c, roleKey{})
//	        },
//	    ),
//	)
//
// Check permissions in handlers:
//
//	func (h *Handler) deleteUser(c forge.Context) error {
//	    if !c.Can("users.write") {
//	        return c.Error(403, "forbidden")
//	    }
//	    return h.repo.DeleteUser(c, forge.Param[string](c, "id"))
//	}
//
// # Type-Safe Parameter Helpers
//
// Generic helper functions provide type-safe access to URL and query
// parameters. They use strconv for conversion and return zero values
// on parse failure:
//
//	func (h *Handler) listItems(c forge.Context) error {
//	    page := forge.QueryDefault[int](c, "page", 1)
//	    limit := forge.QueryDefault[int](c, "limit", 20)
//	    items, err := h.repo.ListItems(c, page, limit)
//	    if err != nil {
//	        return err
//	    }
//	    return c.JSON(200, items)
//	}
//
//	func (h *Handler) getItem(c forge.Context) error {
//	    id := forge.Param[int64](c, "id")
//	    item, err := h.repo.GetItem(c, id)
//	    if err != nil {
//	        return err
//	    }
//	    return c.JSON(200, item)
//	}
//
// Supported types: ~string, ~int, ~int64, ~float64, ~bool.
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
