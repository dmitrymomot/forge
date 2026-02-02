package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/example/handlers"
	"github.com/dmitrymomot/forge/example/repository"
	"github.com/dmitrymomot/forge/example/views"
	"github.com/dmitrymomot/forge/pkg/db"
	"github.com/dmitrymomot/forge/pkg/logger"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	ctx := context.Background()
	log := logger.New()

	// Database connection with migrations (single call)
	pool := db.MustOpen(ctx, getEnv("DATABASE_URL", "postgres://forge:forge@localhost:5432/forge_example?sslmode=disable"),
		db.WithMigrations(migrations),
		db.WithLogger(log),
		db.WithMinConns(2),
	)

	// Create repository
	repo := repository.New(pool)

	// Create application with explicit dependency wiring
	app := forge.New(
		forge.WithAddress(getEnv("ADDRESS", ":8080")),
		forge.WithLogger(log),

		// Register handlers with injected dependencies
		forge.WithHandlers(
			handlers.NewContactHandler(repo),
			handlers.NewTestHandler(), // Test handler for error testing
		),

		// Custom error handlers
		forge.WithErrorHandler(handleError),
		forge.WithNotFoundHandler(handleNotFound),
		forge.WithMethodNotAllowedHandler(handleMethodNotAllowed),

		// Health checks (integrated)
		forge.WithHealthChecks(
			forge.WithReadinessCheck("postgres", db.Healthcheck(pool)),
		),

		// Graceful shutdown hooks
		forge.WithShutdownTimeout(30*time.Second),
		forge.WithShutdownHook(db.Shutdown(pool)),
	)

	// Run the application (blocks until shutdown)
	if err := app.Run(); err != nil {
		log.Error("application error", "error", err)
		os.Exit(1)
	}
}

// handleError handles errors returned from handlers.
// For HTMX requests, renders error messages inline.
// For regular requests, redirects or shows error page.
func handleError(c forge.Context, err error) error {
	log.Printf("handler error: %v", err)
	return c.RenderPartial(http.StatusInternalServerError,
		views.ErrorPage(500, err.Error()),
		views.ErrorContent(500, err.Error()),
	)
}

// handleNotFound renders a 404 error page.
func handleNotFound(c forge.Context) error {
	return c.RenderPartial(http.StatusNotFound,
		views.ErrorPage(404, "The page you're looking for doesn't exist."),
		views.ErrorContent(404, "The page you're looking for doesn't exist."),
	)
}

// handleMethodNotAllowed renders a 405 error page.
func handleMethodNotAllowed(c forge.Context) error {
	return c.RenderPartial(http.StatusMethodNotAllowed,
		views.ErrorPage(405, "This HTTP method is not allowed for this resource."),
		views.ErrorContent(405, "This HTTP method is not allowed for this resource."),
	)
}

// getEnv returns environment variable value or default if not set.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
