package internal

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Router is the interface handlers use to declare routes.
// It provides HTTP method routing and grouping capabilities.
type Router interface {
	// GET registers a handler for GET requests.
	GET(path string, h HandlerFunc, mw ...Middleware)

	// POST registers a handler for POST requests.
	POST(path string, h HandlerFunc, mw ...Middleware)

	// PUT registers a handler for PUT requests.
	PUT(path string, h HandlerFunc, mw ...Middleware)

	// PATCH registers a handler for PATCH requests.
	PATCH(path string, h HandlerFunc, mw ...Middleware)

	// DELETE registers a handler for DELETE requests.
	DELETE(path string, h HandlerFunc, mw ...Middleware)

	// HEAD registers a handler for HEAD requests.
	HEAD(path string, h HandlerFunc, mw ...Middleware)

	// OPTIONS registers a handler for OPTIONS requests.
	OPTIONS(path string, h HandlerFunc, mw ...Middleware)

	// Group creates an inline route group.
	// All routes defined inside fn share no common pattern prefix.
	Group(fn func(r Router))

	// Route creates a route group with a pattern prefix.
	// All routes defined inside fn share the pattern prefix.
	Route(pattern string, fn func(r Router))

	// Use appends middleware to the router's middleware stack.
	Use(mw ...Middleware)

	// Mount attaches an http.Handler at the given pattern.
	// Use this for legacy handlers or third-party routers.
	Mount(pattern string, h http.Handler)
}

// routerAdapter wraps chi.Router to implement the Router interface.
type routerAdapter struct {
	router chi.Router
	app    *App
}

// GET registers a handler for GET requests.
func (r *routerAdapter) GET(path string, h HandlerFunc, mw ...Middleware) {
	r.router.Get(path, r.wrap(h, mw...))
}

// POST registers a handler for POST requests.
func (r *routerAdapter) POST(path string, h HandlerFunc, mw ...Middleware) {
	r.router.Post(path, r.wrap(h, mw...))
}

// PUT registers a handler for PUT requests.
func (r *routerAdapter) PUT(path string, h HandlerFunc, mw ...Middleware) {
	r.router.Put(path, r.wrap(h, mw...))
}

// PATCH registers a handler for PATCH requests.
func (r *routerAdapter) PATCH(path string, h HandlerFunc, mw ...Middleware) {
	r.router.Patch(path, r.wrap(h, mw...))
}

// DELETE registers a handler for DELETE requests.
func (r *routerAdapter) DELETE(path string, h HandlerFunc, mw ...Middleware) {
	r.router.Delete(path, r.wrap(h, mw...))
}

// HEAD registers a handler for HEAD requests.
func (r *routerAdapter) HEAD(path string, h HandlerFunc, mw ...Middleware) {
	r.router.Head(path, r.wrap(h, mw...))
}

// OPTIONS registers a handler for OPTIONS requests.
func (r *routerAdapter) OPTIONS(path string, h HandlerFunc, mw ...Middleware) {
	r.router.Options(path, r.wrap(h, mw...))
}

// Group creates an inline route group.
func (r *routerAdapter) Group(fn func(Router)) {
	r.router.Group(func(cr chi.Router) {
		fn(&routerAdapter{router: cr, app: r.app})
	})
}

// Route creates a route group with a pattern prefix.
func (r *routerAdapter) Route(pattern string, fn func(Router)) {
	r.router.Route(pattern, func(cr chi.Router) {
		fn(&routerAdapter{router: cr, app: r.app})
	})
}

// Use appends middleware to the router's middleware stack.
func (r *routerAdapter) Use(mw ...Middleware) {
	for _, m := range mw {
		r.router.Use(r.app.adaptMiddleware(m))
	}
}

// Mount attaches an http.Handler at the given pattern.
func (r *routerAdapter) Mount(pattern string, h http.Handler) {
	r.router.Mount(pattern, h)
}

// wrap converts a HandlerFunc with optional middleware to http.HandlerFunc.
func (r *routerAdapter) wrap(h HandlerFunc, mw ...Middleware) http.HandlerFunc {
	// Apply route-specific middleware in reverse order
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return r.adaptHandler(h)
}

// adaptHandler converts a forge HandlerFunc to http.HandlerFunc.
func (r *routerAdapter) adaptHandler(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		c := newContext(w, req, r.app.logger, r.app.cookieManager)
		if err := h(c); err != nil {
			r.app.handleError(c, err)
		}
	}
}

// adaptMiddleware converts a forge Middleware to chi middleware.
func (a *App) adaptMiddleware(mw Middleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create a HandlerFunc that calls the next http.Handler
			nextFunc := func(c Context) error {
				next.ServeHTTP(c.Response(), c.Request())
				return nil
			}
			// Apply the forge middleware
			wrapped := mw(nextFunc)
			// Execute with a new context
			c := newContext(w, r, a.logger, a.cookieManager)
			if err := wrapped(c); err != nil {
				a.handleError(c, err)
			}
		})
	}
}
