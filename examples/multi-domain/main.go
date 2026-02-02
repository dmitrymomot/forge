package main

import (
	"log"
	"os"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/examples/multi-domain/handlers"
	"github.com/dmitrymomot/forge/examples/multi-domain/middleware"
	"github.com/dmitrymomot/forge/pkg/logger"
)

func main() {
	slog := logger.New()

	// Create API app (for api.lvh.me)
	api := forge.New(
		forge.WithMiddleware(jsonContentType),
		forge.WithHandlers(handlers.NewAPIHandler()),
	)

	// Create tenant app (for *.lvh.me wildcard)
	tenant := forge.New(
		forge.WithMiddleware(middleware.TenantExtractor),
		forge.WithHandlers(handlers.NewTenantHandler()),
	)

	// Create landing app (fallback for unmatched hosts)
	landing := forge.New(
		forge.WithHandlers(handlers.NewLandingHandler()),
	)

	// Run the multi-domain server
	if err := forge.Run(
		forge.Domain("api.lvh.me", api),
		forge.Domain("*.lvh.me", tenant),
		forge.Fallback(landing),
		forge.Address(getEnv("ADDRESS", ":8081")),
		forge.Logger(slog),
	); err != nil {
		log.Printf("application error: %v", err)
		os.Exit(1)
	}
}

// jsonContentType is middleware that sets Content-Type to application/json.
func jsonContentType(next forge.HandlerFunc) forge.HandlerFunc {
	return func(c forge.Context) error {
		c.SetHeader("Content-Type", "application/json")
		return next(c)
	}
}

// getEnv returns environment variable value or default if not set.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
