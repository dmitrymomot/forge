// Package httpserver runs an HTTP server as a supervised, gracefully-stopping
// service.
//
// Server wraps net/http and satisfies the supervisor.Service interface (Name and
// Run). New takes the handler and functional options; serializable settings live
// in an env-loadable Config with secure defaults:
//
//	srv := httpserver.New(router,
//		httpserver.WithAddr(":8080"),
//		httpserver.WithName("api"),
//	)
//	if err := supervisor.Run(ctx, supervisor.WithService(srv)); err != nil {
//		// ...
//	}
//
// On context cancellation the server stops accepting, drains in-flight requests
// within ShutdownTimeout, then cancels the request base context and force-closes
// any stragglers (returning ErrShutdownTimeout). All option and Config values are
// validated; invalid input is reported by Run as a joined ErrInvalidConfig and no
// I/O is performed. Diagnostics are emitted as structured slog attributes; errors
// are single-line and matchable with errors.Is against ErrNoHandler,
// ErrInvalidConfig, and ErrShutdownTimeout.
package httpserver
