package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Close logs a single line and closes the pool. It is the resource counterpart to
// Open, meant as `defer Close(pool, logger)` in main so it runs after the
// supervisor has drained every service. A nil pool or nil logger is tolerated: the
// close still happens (when the pool is non-nil); the log line is skipped when the
// logger is nil. It takes no ctx because pgxpool.Close is synchronous.
func Close(pool *pgxpool.Pool, log *slog.Logger) {
	if pool == nil {
		return
	}
	if log != nil {
		log.Info("closing postgres pool")
	}
	pool.Close()
}

// Healthcheck returns a stateless closure that pings the pool, wrapping any failure
// in ErrHealthcheck. Its func(context.Context) error shape is exactly what a
// readiness/liveness probe wants; hand it to the app's /readyz handler.
func Healthcheck(pool *pgxpool.Pool) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
