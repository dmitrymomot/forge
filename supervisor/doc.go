// Package supervisor runs a set of long-running services under a single
// coordinated lifecycle with graceful, bounded shutdown.
//
// A Service is any type with a Name and a blocking Run(ctx) method. Register
// services and tune behavior through options passed to Run, and use NewContext
// to wire main's context to SIGINT/SIGTERM:
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		err := supervisor.Run(ctx,
//			supervisor.WithService(httpServer),
//			supervisor.WithServiceFunc("cleanup", cleanup.Loop),
//			supervisor.WithShutdownTimeout(20*time.Second),
//		)
//		if err != nil {
//			slog.Error("runtime stopped", "err", err)
//			os.Exit(1)
//		}
//	}
//
// The first service to return (nil or error), or cancellation of ctx, begins a
// coordinated shutdown: the shared context is cancelled so every service drains
// itself, and Run waits up to the shutdown timeout (default 30s; 0 means wait
// indefinitely) before abandoning any service that has not stopped. All
// diagnostics are emitted as structured slog attributes; the values returned by
// Run are single-line errors that can be matched with errors.Is against
// ErrNoServices, ErrUnnamedService, ErrShutdownTimeout, and ErrPanic.
package supervisor
