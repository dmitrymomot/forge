// Command worker demonstrates running a single background worker under the
// forge supervisor. The worker logs a heartbeat every 5 seconds and stops
// gracefully when the process receives SIGINT or SIGTERM (Ctrl+C).
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/dmitrymomot/forge/supervisor"
)

// heartbeat is a long-running service that logs once per interval. It
// satisfies supervisor.Service by providing Name and a blocking Run.
type heartbeat struct {
	log      *slog.Logger
	interval time.Duration
}

// Name identifies the service in supervisor logs and shutdown diagnostics.
func (h heartbeat) Name() string { return "heartbeat" }

// Run logs a heartbeat every interval until ctx is cancelled, then returns
// nil for a clean stop. Observing ctx is what makes shutdown graceful.
func (h heartbeat) Run(ctx context.Context) error {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for count := 1; ; count++ {
		select {
		case <-ctx.Done():
			h.log.Info("worker stopping, shutdown requested")
			return nil
		case <-ticker.C:
			h.log.Info("heartbeat", slog.Int("count", count))
		}
	}
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// NewContext cancels on the first SIGINT/SIGTERM, which the supervisor
	// turns into a coordinated, bounded shutdown of every service.
	ctx, stop := supervisor.NewContext()
	defer stop()

	err := supervisor.Run(ctx,
		supervisor.WithLogger(log),
		supervisor.WithService(heartbeat{log: log, interval: 5 * time.Second}),
	)
	if err != nil {
		log.Error("supervisor stopped with error", slog.Any("err", err))
		os.Exit(1)
	}
}
