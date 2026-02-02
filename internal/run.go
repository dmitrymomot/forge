package internal

import (
	"errors"
	"net/http"

	"github.com/dmitrymomot/forge/pkg/hostrouter"
)

// Run starts a multi-domain HTTP server and blocks until shutdown.
// Use this for composing multiple Apps under different domain patterns.
//
// Example:
//
//	api := forge.New(
//	    forge.WithHandlers(handlers.NewAPIHandler()),
//	)
//
//	website := forge.New(
//	    forge.WithHandlers(handlers.NewLandingHandler()),
//	)
//
//	err := forge.Run(
//	    forge.Domain("api.acme.com", api),
//	    forge.Domain("*.acme.com", website),
//	    forge.Address(":8080"),
//	    forge.Logger(slog),
//	)
func Run(opts ...RunOption) error {
	cfg := buildRunConfig(opts...)

	var handler http.Handler

	if len(cfg.domains) > 0 {
		// Build host router from domain mappings
		routes := make(hostrouter.Routes)
		for pattern, app := range cfg.domains {
			routes[pattern] = app.Router()
		}

		// Determine fallback handler
		var fallback http.Handler = http.NotFoundHandler()
		if cfg.fallback != nil {
			fallback = cfg.fallback.Router()
		}

		handler = hostrouter.New(routes, fallback)
	} else if cfg.fallback != nil {
		// No domains, but fallback provided - use as main handler
		handler = cfg.fallback.Router()
	} else {
		return errors.New("forge.Run: no domains or fallback configured")
	}

	return runServer(runtimeConfig{
		handler:         handler,
		address:         cfg.address,
		logger:          cfg.logger,
		shutdownTimeout: cfg.shutdownTimeout,
		shutdownHooks:   cfg.shutdownHooks,
		baseCtx:         cfg.baseCtx,
	})
}
