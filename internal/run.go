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
func Run(cfg RunConfig, opts ...RunOption) error {
	rc := buildRunConfig(cfg, opts...)

	var handler http.Handler

	// Collect all apps for worker registration
	var allApps []*App

	if len(rc.domains) > 0 {
		// Build host router from domain mappings
		routes := make(hostrouter.Routes)
		for pattern, app := range rc.domains {
			routes[pattern] = app.Router()
			allApps = append(allApps, app)
		}

		// Determine fallback handler
		var fallback http.Handler = http.NotFoundHandler()
		if rc.fallback != nil {
			fallback = rc.fallback.Router()
			allApps = append(allApps, rc.fallback)
		}

		handler = hostrouter.New(routes, fallback)
	} else if rc.fallback != nil {
		// No domains, but fallback provided - use as main handler
		handler = rc.fallback.Router()
		allApps = append(allApps, rc.fallback)
	} else {
		return errors.New("forge.Run: no domains or fallback configured")
	}

	// Collect workers from all apps and deduplicate
	startupHooks := rc.startupHooks
	shutdownHooks := rc.shutdownHooks
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
		address:         rc.address,
		logger:          rc.logger,
		shutdownTimeout: rc.shutdownTimeout,
		startupHooks:    startupHooks,
		shutdownHooks:   shutdownHooks,
		baseCtx:         rc.baseCtx,
	})
}
