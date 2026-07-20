package debug

import (
	"expvar"
	"net/http"
	"net/http/pprof"

	"github.com/dmitrymomot/forge/web/middleware"
)

// Handler returns the diagnostics surface as one http.Handler:
//
//	/debug/pprof/*  pprof index, named profiles, cmdline, profile, symbol, trace
//	/debug/stats    runtime/GC/goroutine snapshot as JSON (see Snapshot)
//	/debug/vars     expvar
//	/               plain-text index of the above
//
// Paths are absolute, so the handler mounts on an existing internal mux without
// StripPrefix: mux.Handle("/debug/", debug.Handler(...)). Handler applies
// WithBasicAuth/WithMiddleware around the whole surface and ignores the
// server-only options (Config, listener, WithoutAuth) — exposure is the
// caller's responsibility here; the fail-closed non-loopback check lives in
// Server.Run.
func Handler(opts ...Option) http.Handler {
	c := newConfig(opts...)
	return buildHandler(&c)
}

func buildHandler(c *config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", index)
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/vars", expvar.Handler())
	mux.HandleFunc("/debug/stats", statsHandler)
	return middleware.Wrap(mux, c.guards...)
}

func index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("/debug/pprof/\tprofiles (heap, goroutine, cpu, trace, ...)\n/debug/stats\truntime/GC/goroutine snapshot\n/debug/vars\texpvar\n"))
}
