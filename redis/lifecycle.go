package redis

import (
	"context"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
)

// Close announces intent by logging "closing redis client" (when log is non-nil),
// then closes the client. On close error it additionally logs an error line.
// It is the defer helper used in main — `defer Close(client, logger)` —
// so it runs after supervisor.Run returns, once in-flight work has drained. It takes
// no context because the driver's Close is synchronous. A nil client and/or a nil
// logger are tolerated: the log line is skipped and no close is attempted on nil.
func Close(c goredis.UniversalClient, log *slog.Logger) {
	if c == nil {
		return
	}
	if log != nil {
		log.Info("closing redis client")
	}
	if err := c.Close(); err != nil && log != nil {
		log.Error("redis client close failed", "err", err)
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
