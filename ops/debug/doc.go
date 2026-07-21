// Package debug serves one internal diagnostics surface: /debug/pprof/*,
// /debug/stats (runtime/GC/goroutine JSON), and /debug/vars (expvar) — guarded,
// and off the public port.
//
// NewServer runs the surface on a dedicated port as a supervisor.Service. The
// default address is loopback-only (localhost:6060), so the zero-ceremony form
// is safe; Run refuses a non-loopback bind unless auth middleware is configured
// (WithBasicAuth / WithMiddleware) or WithoutAuth explicitly opts out — an open
// pprof endpoint leaks heap contents, command lines, and can stall the process
// with profile requests:
//
//	users, err := guard.ParseUsers(os.Getenv("DEBUG_BASIC_USERS")) // "ops:s3cret"
//	if err != nil {
//		log.Fatal(err)
//	}
//	dbg := debug.NewServer(
//		debug.WithAddr(":6060"),
//		debug.WithBasicAuth(users),
//	)
//	err = supervisor.Run(ctx,
//		supervisor.WithService(app),
//		supervisor.WithService(dbg),
//	)
//
// Handler returns the same surface as a plain http.Handler for mounting into an
// existing internal mux — paths are absolute, so no StripPrefix:
//
//	mux.Handle("/debug/", debug.Handler(debug.WithBasicAuth(users)))
//
// The dedicated server disables WriteTimeout because CPU profiles and traces
// stream for the requested ?seconds=N. Importing this package (via
// net/http/pprof) also registers the pprof handlers on http.DefaultServeMux;
// forge never serves DefaultServeMux, but consumers that do should be aware.
package debug
