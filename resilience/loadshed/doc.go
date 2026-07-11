// Package loadshed is adaptive admission control: it rejects a fraction of
// incoming work early and cheaply when the service is overloaded, so admitted
// work still succeeds. It protects the callee (this service) based on its own
// current health — unlike ratelimit (per-client fairness) or circuitbreaker
// (protecting the caller from a failing dependency).
//
// # Usage
//
//	sh := loadshed.New(loadshed.WithCriteria(
//	    loadshed.Concurrency(500),
//	    loadshed.Latency(200*time.Millisecond),
//	))
//	mux.Use(sh.Middleware()) // 503s a slice of traffic under overload
//
//	// non-HTTP:
//	if tk, ok := sh.Acquire(ctx); ok {
//	    defer tk.Release()
//	    process(job)
//	}
//
// CPU-based pressure stays consumer-side: implement Criteria over your own CPU
// reader and pass it with WithCriteria.
package loadshed
