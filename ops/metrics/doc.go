// Package metrics is a minimal Counter/Gauge/Histogram facade with an
// expvar-backed default recorder and HTTP request middleware. Application and
// forge packages record against the Recorder seam; swapping the backend
// (metrics/prometheus is the only adapter) changes wiring, never call sites.
// Instruments are created once at startup and are safe for concurrent use;
// schema mistakes (invalid names, label count mismatch, negative counter
// deltas) panic, mirroring prometheus semantics on every backend.
//
// # Zero-dependency default (expvar)
//
//	rec := metrics.New() // published as expvar var "metrics" → /debug/vars
//
//	signups := rec.Counter("signups_total", "Completed signups.", "plan")
//	queueDepth := rec.Gauge("queue_depth", "Jobs waiting per queue.", "queue")
//	jobSeconds := rec.Histogram("job_duration_seconds", "Job run time.", nil, "kind")
//
//	signups.Inc("pro")
//	queueDepth.Set(17, "email")
//	jobSeconds.Observe(0.42, "welcome_email")
//
// # Pull gauges
//
// GaugeFunc is the pull counterpart of Gauge: the value is read at collection time
// (a /debug/vars render or a prometheus scrape) instead of being pushed, so a number
// a live source already owns needs no goroutine to mirror it:
//
//	rec.GaugeFunc("db_pool_conns_idle", "Idle pool connections.",
//		func(context.Context) (float64, error) {
//			return float64(pool.Stat().IdleConns()), nil
//		})
//
//	rec.GaugeFunc("jobs_pending", "Jobs ready to run.",
//		func(ctx context.Context) (float64, error) {
//			n, err := queries.CountPendingJobs(ctx)
//			return float64(n), err
//		})
//
// The call is bounded by WithCollectTimeout (default DefaultCollectTimeout), so a
// stalled query cannot hold the endpoint open. A failed read drops the gauge from
// that collection — stale beats wrong — and counts one CollectFailuresMetric under
// the gauge's name. GaugeFunc takes no labels: register one name per series.
//
//	mux := http.NewServeMux()
//	mux.Handle("GET /users/{id}", getUser)
//	handler := middleware.Wrap(mux,
//		metrics.NewMiddleware(rec, metrics.WithSkip(func(r *http.Request) bool {
//			return r.URL.Path == "/livez"
//		})),
//		recoverer.New(), // inside metrics: recovered panics are recorded as 500s
//	)
//
// The middleware records http_requests_total, http_request_duration_seconds
// (labels: method, path, status), and http_requests_in_flight. The path label
// is the matched r.Pattern — bounded cardinality — with WithPathFunc as the
// hook for non-ServeMux routers.
//
// # Multi-tenant scoping
//
// WithContextLabel is the construction-time tenancy seam; single-tenant apps
// skip it and pay nothing:
//
//	metrics.NewMiddleware(rec,
//		metrics.WithContextLabel("tenant", func(ctx context.Context) string {
//			id, _ := tenant.FromContext(ctx)
//			return id // "" records as "unknown" (fail-closed)
//		}),
//	)
//
// # Prometheus
//
//	rec := prometheus.New() // ops/metrics/prometheus
//	mux.Handle("GET /metrics", rec.Handler())
//
// Packages that emit metrics take a Recorder option defaulting to NewNoop().
package metrics
