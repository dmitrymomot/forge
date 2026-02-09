// Package forge provides a simple, opinionated framework for building
// B2B micro-SaaS applications in Go.
//
// Forge is designed around the principle of "no magic" — it uses explicit,
// readable code with no reflection or service containers. The framework
// provides a thin orchestration layer while keeping business logic in
// plain Go handlers.
//
// # Quick Start
//
// Create a new application with New(), configure it with options, and
// pass it to Run():
//
//	app := forge.New(
//	    forge.AppConfig{},
//	    forge.WithHandlers(
//	        handlers.NewAuth(repo),
//	        handlers.NewPages(repo),
//	    ),
//	)
//
//	if err := forge.Run(
//	    forge.RunConfig{},
//	    forge.WithFallback(app),
//	    forge.WithRunLogger(logger),
//	); err != nil {
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
//	        return c.Redirect(http.StatusSeeOther, "/login")
//	    }
//
//	    user, err := h.repo.GetUser(c, c.UserID())
//	    if err != nil {
//	        return err
//	    }
//
//	    // Only allow users to edit their own profile
//	    canEdit := c.IsCurrentUser(user.ID)
//	    return c.Render(http.StatusOK, views.Profile(user, canEdit))
//	}
//
// # Sessions
//
// Enable server-side session management with WithSession. Sessions auto-create
// on first access via SessionGet or SessionSet:
//
//	app := forge.New(
//	    forge.AppConfig{},
//	    forge.WithSession(postgresStore,
//	        forge.WithSessionTTL(7 * 24 * time.Hour),
//	        forge.WithMaxSessionsPerUser(3),
//	        forge.WithSessionFingerprint(
//	            forge.FingerprintCookie,
//	            forge.FingerprintWarn,
//	        ),
//	    ),
//	)
//
// Use SessionGet and SessionSet for type-safe access:
//
//	func (h *Handler) login(c forge.Context) error {
//	    if err := forge.SessionSet(c, "user_id", user.ID); err != nil {
//	        return err
//	    }
//	    if err := forge.SessionSet(c, "login_time", time.Now()); err != nil {
//	        return err
//	    }
//	    return c.Redirect(http.StatusSeeOther, "/dashboard")
//	}
//
//	func (h *Handler) dashboard(c forge.Context) error {
//	    userID, ok := forge.SessionGet[string](c, "user_id")
//	    if !ok {
//	        return c.Redirect(http.StatusSeeOther, "/login")
//	    }
//	    // Use userID...
//	    return c.Render(http.StatusOK, views.Dashboard())
//	}
//
// # Role-Based Access Control (RBAC)
//
// Configure permissions with WithRoles. The role extractor is called
// lazily on the first [Context.Can] call and cached for the request:
//
//	app := forge.New(
//	    forge.AppConfig{},
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
//	        return forge.ErrForbidden("You do not have permission")
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
//	    return c.JSON(http.StatusOK, items)
//	}
//
//	func (h *Handler) getItem(c forge.Context) error {
//	    id := forge.Param[int64](c, "id")
//	    item, err := h.repo.GetItem(c, id)
//	    if err != nil {
//	        return err
//	    }
//	    return c.JSON(http.StatusOK, item)
//	}
//
// Supported types: ~string, ~int, ~int64, ~float64, ~bool.
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
//	    return c.Render(http.StatusOK, views.LoginPage())
//	}
//
// # Middleware
//
// Middleware wraps handlers to add cross-cutting concerns:
//
//	func RequestLogger(log *slog.Logger) forge.Middleware {
//	    return func(next forge.HandlerFunc) forge.HandlerFunc {
//	        return func(c forge.Context) error {
//	            start := time.Now()
//	            err := next(c)
//	            log.Info("request",
//	                "method", c.Request().Method,
//	                "path", c.Request().URL.Path,
//	                "duration", time.Since(start),
//	                "error", err,
//	            )
//	            return err
//	        }
//	    }
//	}
//
// Add middleware globally with WithMiddleware:
//
//	app := forge.New(
//	    forge.AppConfig{},
//	    forge.WithMiddleware(
//	        RequestLogger(logger),
//	        middlewares.RequestID(middlewares.RequestIDConfig{}),
//	        middlewares.Recover(middlewares.RecoverConfig{}),
//	    ),
//	)
//
// # Built-In Middlewares
//
// The middlewares package provides common functionality:
//
//   - RequestID: Adds request tracking IDs
//   - Recover: Handles panics gracefully
//   - I18n: Internationalization and localization
//   - JWT: Token-based authentication
//   - CSRF: Cross-site request forgery protection
//   - RateLimit: Request rate limiting
//   - AuditLog: Request and action logging
//
// # Jobs and Background Processing
//
// Enable background job processing with River integration:
//
//	app := forge.New(
//	    forge.AppConfig{},
//	    forge.WithJobs(pgxPool,
//	        forge.JobConfig{
//	            Workers: 2,
//	        },
//	        forge.WithTask(EmailTask{}),
//	        forge.WithScheduledTask(CleanupTask{}),
//	    ),
//	)
//
// Define tasks using structural typing:
//
//	type EmailTask struct{}
//
//	func (EmailTask) Name() string { return "send_email" }
//	func (EmailTask) Handle(ctx context.Context, p struct{ Email string }) error {
//	    // Send email...
//	    return nil
//	}
//
// Enqueue jobs from handlers:
//
//	func (h *Handler) signup(c forge.Context) error {
//	    // ...validation...
//	    err := c.Enqueue("send_email",
//	        struct{ Email string }{Email: user.Email},
//	        forge.WithQueue("emails"),
//	        forge.WithScheduledIn(1*time.Minute),
//	    )
//	    if err != nil {
//	        return err
//	    }
//	    return c.Redirect(http.StatusSeeOther, "/signup/confirm")
//	}
//
// # File Storage
//
// Enable S3-compatible file storage:
//
//	storage, err := forge.NewS3Storage(forge.StorageConfig{
//	    Endpoint:  "s3.amazonaws.com",
//	    AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
//	    SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
//	    Bucket:    "myapp-uploads",
//	    Region:    "us-east-1",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	app := forge.New(
//	    forge.AppConfig{},
//	    forge.WithStorage(storage),
//	)
//
// Upload and download files from handlers:
//
//	func (h *Handler) uploadAvatar(c forge.Context) error {
//	    info, err := c.Upload("avatar",
//	        forge.WithStoragePrefix("avatars"),
//	        forge.WithStorageValidation(
//	            forge.MaxFileSize(5*1024*1024),
//	            forge.ImageFilesOnly(),
//	        ),
//	    )
//	    if err != nil {
//	        return err
//	    }
//
//	    // Save info.Key to database
//	    return c.JSON(http.StatusOK, map[string]string{
//	        "url": info.URL,
//	    })
//	}
//
// # Server-Sent Events (SSE)
//
// Stream events to clients using the channel-based SSE API.
// The framework handles headers, keepalive, and flushing:
//
//	func (h *Handler) streamEvents(c forge.Context) error {
//	    ch := make(chan forge.SSEEvent)
//	    go func() {
//	        defer close(ch)
//	        for {
//	            select {
//	            case <-c.Done():
//	                return
//	            case event := <-eventChan:
//	                ch <- forge.SSEString("message", event.Data)
//	            }
//	        }
//	    }()
//	    return c.SSE(ch)
//	}
//
// # Multi-Domain Routing
//
// For applications that need host-based routing, compose multiple Apps
// with Run():
//
//	api := forge.New(
//	    forge.AppConfig{BaseDomain: "acme.com"},
//	    forge.WithHandlers(handlers.NewAPIHandler()),
//	)
//
//	website := forge.New(
//	    forge.AppConfig{BaseDomain: "acme.com"},
//	    forge.WithHandlers(handlers.NewLandingHandler()),
//	)
//
//	if err := forge.Run(
//	    forge.RunConfig{Address: ":8080"},
//	    forge.WithDomain("api.acme.com", api),
//	    forge.WithDomain("*.acme.com", website),
//	    forge.WithRunLogger(logger),
//	); err != nil {
//	    log.Fatal(err)
//	}
//
// # Error Handling
//
// Return HTTPError from handlers to set the status code and error details:
//
//	func (h *Handler) getUser(c forge.Context) error {
//	    user, err := h.repo.GetUser(c, id)
//	    if err == sql.ErrNoRows {
//	        return forge.ErrNotFound("User not found")
//	    }
//	    if err != nil {
//	        return forge.ErrInternal("Failed to fetch user")
//	    }
//	    return c.JSON(http.StatusOK, user)
//	}
//
// Customize error response handling with WithErrorHandler:
//
//	app := forge.New(
//	    forge.AppConfig{},
//	    forge.WithErrorHandler(func(c forge.Context, err error) error {
//	        if httpErr := forge.AsHTTPError(err); httpErr != nil {
//	            return c.JSON(httpErr.StatusCode(), httpErr)
//	        }
//	        return c.JSON(http.StatusInternalServerError, map[string]string{
//	            "message": "Something went wrong",
//	        })
//	    }),
//	)
//
// # Shutdown
//
// The application handles SIGINT/SIGTERM for graceful shutdown.
// Register cleanup functions with WithShutdownHook:
//
//	if err := forge.Run(
//	    forge.RunConfig{Address: ":8080"},
//	    forge.WithFallback(app),
//	    forge.WithShutdownHook(func(ctx context.Context) error {
//	        return pool.Close()
//	    }),
//	); err != nil {
//	    log.Fatal(err)
//	}
//
// # Configuration
//
// Load configuration from environment variables with LoadConfig:
//
//	type Config struct {
//	    DatabaseURL string `env:"DATABASE_URL,required"`
//	    Port        string `env:"PORT" envDefault:":8080"`
//	    Debug       bool   `env:"DEBUG"`
//	}
//
//	var cfg Config
//	if err := forge.LoadConfig(&cfg); err != nil {
//	    log.Fatal(err)
//	}
//
// # Testing
//
// For testing, use httptest.NewServer with the app:
//
//	app := forge.New(
//	    forge.AppConfig{},
//	    forge.WithHandlers(myHandler),
//	)
//	ts := httptest.NewServer(app.Router())
//	defer ts.Close()
//
//	resp, err := http.Get(ts.URL + "/path")
//	if err != nil {
//	    t.Fatal(err)
//	}
//	// Assert response...
package forge
