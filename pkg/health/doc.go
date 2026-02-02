// Package health provides HTTP handlers for health probes.
//
// This package implements liveness and readiness endpoints compatible with
// Docker, Kubernetes, and 3rd-party monitoring services. It integrates with
// existing healthcheck closures from pg, redis, cqrs, and jobs packages.
//
// # Main Functions
//
// [LivenessHandler] provides a simple always-OK endpoint for process liveness.
// [ReadinessHandler] executes a set of [Checks] and returns service readiness.
//
// # Features
//
//   - Liveness and readiness HTTP handlers
//   - Named health checks with detailed status reporting
//   - JSON and plain text response formats (content negotiation)
//   - Parallel check execution with configurable timeout
//   - Compatible with existing func(context.Context) error signatures
//   - Works with any HTTP router (standard http.HandlerFunc)
//
// # Quick Start
//
// Register health endpoints on your router:
//
//	r.Get("/health/live", health.LivenessHandler())
//	r.Get("/health/ready", health.ReadinessHandler(health.Checks{
//	    "postgres": pg.Healthcheck(pool),
//	    "redis":    redis.Healthcheck(client),
//	}))
//
// # Response Formats
//
// By default, handlers respond with plain text for compatibility with probes.
// Request JSON by setting Accept: application/json header or ?format=json:
//
//	curl http://localhost:8080/health/ready?format=json
//
// Plain text responses:
//   - 200 OK: "OK"
//   - 503 Service Unavailable: "Service Unavailable"
//
// JSON response structure:
//
//	{
//	  "status": "healthy",
//	  "checks": {
//	    "postgres": {"status": "healthy"},
//	    "redis": {"status": "unhealthy", "error": "connection refused"}
//	  }
//	}
//
// # Configuration Options
//
// Configure timeout and logging:
//
//	r.Get("/health/ready", health.ReadinessHandler(checks,
//	    health.WithTimeout(3*time.Second),
//	    health.WithLogger(logger),
//	))
//
// # Integration Example
//
// Complete example with existing boilerplate packages:
//
//	func main() {
//	    // ... setup db, redis, workers ...
//
//	    checks := health.Checks{
//	        "postgres":       pg.Healthcheck(db),
//	        "redis":          redis.Healthcheck(redisConn),
//	        "command_worker": cmdWorker.Healthcheck(),
//	        "event_worker":   eventWorker.Healthcheck(),
//	        "jobs":           runner.Healthcheck(),
//	    }
//
//	    r := chi.NewRouter()
//	    r.Get("/health/live", health.LivenessHandler())
//	    r.Get("/health/ready", health.ReadinessHandler(checks, health.WithLogger(log)))
//
//	    // ... start server ...
//	}
//
// # Kubernetes Configuration
//
// Example Kubernetes probe configuration:
//
//	livenessProbe:
//	  httpGet:
//	    path: /health/live
//	    port: 8080
//	  initialDelaySeconds: 5
//	  periodSeconds: 10
//
//	readinessProbe:
//	  httpGet:
//	    path: /health/ready
//	    port: 8080
//	  initialDelaySeconds: 5
//	  periodSeconds: 10
//
// # Docker Healthcheck
//
// Example Docker healthcheck:
//
//	HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
//	  CMD curl -f http://localhost:8080/health/ready || exit 1
//
// # Error Handling
//
// The package defines sentinel errors for consistent error handling:
//
//   - [ErrCheckFailed] - One or more checks failed
//   - [ErrCheckTimeout] - Check exceeded timeout
package health
