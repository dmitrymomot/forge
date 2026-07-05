// Package health is a single pull-evaluated handler factory for liveness and
// readiness. Handler() with no checks always returns 200 (liveness); the same
// function with checks is readiness. Checks run on each scrape (no cache, no
// background workers); they are critical by default (failure → 503) unless
// marked NonCritical (failure → "degraded" 200).
//
// # Liveness + readiness over datastores
//
// Each forge data client already exposes a func(ctx) error healthcheck, which
// IS a health.Check — so they plug straight in:
//
//	mux.Handle("GET /livez", health.Handler()) // process is up
//
//	mux.Handle("GET /readyz", health.Handler(
//		health.WithCheck("postgres", postgres.Healthcheck(pool)),                // critical
//		health.WithCheck("mongo", mongo.Healthcheck(mdb)),                       // critical
//		health.WithCheck("redis", redis.Healthcheck(rdb), health.NonCritical()), // degrade
//		health.WithCheck("opensearch", opensearch.Healthcheck(osc), health.NonCritical()),
//		health.WithTimeout(2*time.Second),
//	))
//
// # Graceful drain (readiness 503 before the server stops)
//
// A Gate check plus supervisor's ordered pre-shutdown phase flips /readyz to 503
// and waits so the load balancer deregisters while the server still serves:
//
//	gate := health.NewGate()
//	readyz := health.Handler(health.WithCheck("accepting", gate.Check) /* + store checks */)
//	supervisor.Run(ctx,
//		supervisor.WithPreShutdown("drain", func(ctx context.Context) {
//			gate.Down() // next /readyz scrape is 503; server keeps serving
//			select {
//			case <-time.After(5 * time.Second): // ≥ probeInterval × failureThreshold
//			case <-ctx.Done():
//			}
//		}),
//		supervisor.WithService(srv), // listener closes only after the hook returns
//	)
//
// An HTTP-dependency check is a one-liner (GET → 2xx, drain+close body, honor
// ctx); it is not a package export.
package health
