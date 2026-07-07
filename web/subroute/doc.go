// Package subroute mounts http.Handlers under path prefixes on a standard
// *http.ServeMux, in the spirit of chi's Mount.
//
// Mount registers a prefix — exact and subtree — and dispatches to the
// mounted handler with the prefix stripped, so sub-apps are written as if
// they live at the root:
//
//	adminMux := http.NewServeMux()
//	adminMux.HandleFunc("GET /users/{id}", showUser) // serves /admin/users/{id}
//
//	mux := http.NewServeMux()
//	subroute.Mount(mux, "/admin", adminMux)
//
// A request for the bare prefix ("/admin") reaches the mounted handler with
// path "/". The caller's request is never mutated; stripping happens on a
// clone. Mounts nest: a mounted ServeMux can itself have Mount applied.
//
// # Wildcard prefixes
//
// Prefixes may contain single-segment {name} wildcards. Their matched values
// are captured at the mount boundary (via request.WithPathValues), because
// the standard library discards a parent mux's path values when a nested mux
// matches. Read them with the request package's path accessors, which check
// the current mux match first and then mount captures, innermost first:
//
//	subroute.Mount(mux, "/app/{tenant}/dashboard", dashboardMux)
//
//	// in a dashboardMux handler:
//	tenant, _ := request.Path[string](r, "tenant") // from the mount prefix
//	id, _ := request.Path[int](r, "id")            // own wildcards, typed as usual
//
// Plain r.PathValue only sees the current mux's own wildcards. Method, host,
// {$}, and {name...} elements are not allowed in prefixes.
//
// # Middleware
//
// Mount takes no middleware parameters; compose with web/middleware:
//
//	subroute.Mount(mux, "/admin", middleware.Wrap(adminMux, requireAuth))
//	srv.Handler = middleware.Wrap(mux, requestlog, recoverer) // global
//
// Middleware wrapped inside the mount observes the stripped path ("/users"),
// while middleware wrapping the outer mux observes the full original path
// ("/admin/users"). This differs from chi, which routes through a shared
// context and never rewrites the URL; with a plain ServeMux as the child
// router the rewrite is unavoidable.
package subroute
