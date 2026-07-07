package bootstrap_test

import (
	"context"
	"log/slog"
	"os"

	"github.com/dmitrymomot/forge/ops/bootstrap"
	"github.com/dmitrymomot/forge/ops/logger"
)

// Example_main shows the canonical shape of a bootstrap-driven main(): the
// callback owns the whole app body (wiring, supervisor.Run, defer-based
// cleanup) while bootstrap owns only the runtime edges. This example has no
// "Output:" comment, so `go test` compiles it but does not execute it.
func Example_main() {
	os.Exit(bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, log *slog.Logger) error {
			// Wire the app, then hand off to supervisor.Run, e.g.:
			//
			//	return supervisor.Run(ctx, log, srv1, srv2)
			//
			// defer any resource cleanup here — it runs after the callback
			// returns, whether that's a clean drain or an early error.
			return nil
		},
		bootstrap.WithLogger(logger.NewNope()),
	))
}
