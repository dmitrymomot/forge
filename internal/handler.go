package internal

// Handler declares routes on a router.
//
// Example:
//
//	type AuthHandler struct {
//	    repo *repository.Queries
//	}
//
//	func (h *AuthHandler) Routes(r forge.Router) {
//	    r.GET("/login", h.showLogin)
//	    r.POST("/login", h.handleLogin)
//	}
type Handler interface {
	Routes(r Router)
}

// HandlerFunc is the signature for route handlers.
// It receives a Context and returns an error.
// Returning a non-nil error triggers the error handling middleware.
type HandlerFunc func(c Context) error

// Middleware wraps a HandlerFunc to add cross-cutting concerns.
// Middleware can inspect/modify the request, short-circuit processing,
// or wrap the response.
//
// Example:
//
//	func Auth(next forge.HandlerFunc) forge.HandlerFunc {
//	    return func(c forge.Context) error {
//	        if !isAuthenticated(c) {
//	            return c.Redirect(302, "/login")
//	        }
//	        return next(c)
//	    }
//	}
type Middleware func(next HandlerFunc) HandlerFunc

// ErrorHandler handles errors returned from handlers.
type ErrorHandler func(Context, error) error
