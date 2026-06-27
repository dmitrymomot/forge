package redis

import (
	"context"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
)

// Close logs a single "closing redis client" line (when log is non-nil) and closes
// the client. It is the defer helper used in main — `defer Close(client, logger)` —
// so it runs after supervisor.Run returns, once in-flight work has drained. It takes
// no context because the driver's Close is synchronous. A nil client and/or a nil
// logger are tolerated: the log line is skipped and no close is attempted on nil.
func Close(c goredis.UniversalClient, log *slog.Logger) {
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		if log != nil {
			log.Error("redis client close failed", "err", err)
		}
		return
	}
	if log != nil {
		log.Info("closing redis client")
	}
}

// Healthcheck returns a stateless closure that PINGs the server, wrapping any failure
// in ErrHealthcheck. The closure has the exact func(context.Context) error shape a
// readiness/liveness probe wants; hand it to the app's /readyz handler. It is safe to
// call on every probe.
func Healthcheck(c goredis.UniversalClient) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := c.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
