// Package prometheus adapts ops/metrics to the prometheus client — the only
// place in forge that imports it. Swap it in for the expvar default without
// touching call sites:
//
//	rec := prometheus.New() // private registry + Go runtime & process collectors
//
//	requests := rec.Counter("signups_total", "Completed signups.", "plan")
//	requests.Inc("pro")
//
//	mux.Handle("GET /metrics", rec.Handler())
//	handler := middleware.Wrap(mux, metrics.NewMiddleware(rec))
//
// Bring your own registry (no collectors are auto-added) and a name prefix:
//
//	rec := prometheus.New(
//		prometheus.WithRegistry(reg),
//		prometheus.WithNamespace("checkout"),
//	)
package prometheus
