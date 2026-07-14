package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// Close logs a single line (when log is non-nil), then closes c. Both the native
// clickhouse.Conn returned by Open and the *sql.DB returned by OpenDB satisfy
// io.Closer, so one helper covers both. It is the resource counterpart to
// Open/OpenDB, meant as `defer Close(conn, logger)` in main so it runs after the
// supervisor has drained every service. A nil c and/or nil log is tolerated: the log
// line is skipped and no close is attempted on a nil closer. It takes no context
// because both driver Close methods are synchronous.
func Close(c io.Closer, log *slog.Logger) {
	if c == nil {
		return
	}
	if log != nil {
		log.Info("closing clickhouse connection")
	}
	if err := c.Close(); err != nil && log != nil {
		log.Error("clickhouse close failed", "err", err)
	}
}

// Healthcheck returns a stateless closure that pings the native connection, wrapping
// any failure in ErrHealthcheck. Its func(context.Context) error shape is exactly what
// a readiness/liveness probe wants; hand it to the app's /readyz handler. It is safe
// to call on every probe.
func Healthcheck(conn ch.Conn) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := conn.Ping(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}

// HealthcheckDB is the *sql.DB counterpart to Healthcheck for connections opened with
// OpenDB. It pings via PingContext and wraps failures in ErrHealthcheck.
func HealthcheckDB(db *sql.DB) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
