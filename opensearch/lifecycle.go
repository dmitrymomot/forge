package opensearch

import (
	"context"
	"fmt"
	"log/slog"

	osgo "github.com/opensearch-project/opensearch-go/v4"
)

// Close logs a single "closing" line. The opensearch-go client is HTTP-based and
// owns no long-lived sockets the package controls, so there is nothing to release —
// this helper exists for lifecycle symmetry with the other connectivity packages
// so every backend reads Open / Close / Healthcheck. Used as
// `defer Close(client, logger)` in main. A nil logger is tolerated (the log line is
// skipped); a nil client is tolerated (no-op).
func Close(c *osgo.Client, log *slog.Logger) {
	if c == nil {
		return
	}
	if log != nil {
		log.Info("opensearch: closing client")
	}
}

// Healthcheck returns a stateless closure that probes the cluster's health,
// wrapping any failure in ErrHealthcheck. Its func(context.Context) error shape is
// exactly what a readiness/liveness probe wants; it is safe to call on every probe.
func Healthcheck(c *osgo.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		api := apiFor(c)
		if _, err := api.Cluster.Health(ctx, nil); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
