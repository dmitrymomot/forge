package internal

import (
	"context"
	"errors"
	"net/http"

	"github.com/dmitrymomot/forge/pkg/hostrouter"
)

// Run starts a multi-domain HTTP server and blocks until shutdown.
// Use this for composing multiple Apps under different domain patterns.
// If any Apps have job workers configured, they start automatically before
// serving requests and stop gracefully during shutdown.
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

	// Collect all apps for worker registration
	var allApps []*App

	if len(cfg.domains) > 0 {
		// Build host router from domain mappings
		routes := make(hostrouter.Routes)
		for pattern, app := range cfg.domains {
			routes[pattern] = app.Router()
			allApps = append(allApps, app)
		}

		// Determine fallback handler
		var fallback http.Handler = http.NotFoundHandler()
		if cfg.fallback != nil {
			fallback = cfg.fallback.Router()
			allApps = append(allApps, cfg.fallback)
		}

		handler = hostrouter.New(routes, fallback)
	} else if cfg.fallback != nil {
		// No domains, but fallback provided - use as main handler
		handler = cfg.fallback.Router()
		allApps = append(allApps, cfg.fallback)
	} else {
		return errors.New("forge.Run: no domains or fallback configured")
	}

	// Collect workers from all apps and deduplicate
	startupHooks := cfg.startupHooks
	shutdownHooks := cfg.shutdownHooks
	seenWorkers := make(map[*JobManager]bool)

	for _, app := range allApps {
		worker := app.JobWorker()
		if worker != nil && !seenWorkers[worker] {
			seenWorkers[worker] = true
			startupHooks = append([]func(context.Context) error{worker.Manager().StartFunc()}, startupHooks...)
			shutdownHooks = append(shutdownHooks, worker.Shutdown())
		}
	}

	return runServer(runtimeConfig{
		handler:         handler,
		address:         cfg.address,
		logger:          cfg.logger,
		shutdownTimeout: cfg.shutdownTimeout,
		startupHooks:    startupHooks,
		shutdownHooks:   shutdownHooks,
		baseCtx:         cfg.baseCtx,
	})
}
