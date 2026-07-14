package sqlite

import (
	"context"
	"fmt"
	"log/slog"
)

// Close logs a single line and closes both pools. It is the resource counterpart to
// Open, meant as `defer Close(db, logger)` in main so it runs after the supervisor
// drains every service. A nil db or nil logger is tolerated. It takes no ctx because
// *sql.DB.Close is synchronous.
func Close(db *DB, log *slog.Logger) {
	if db == nil {
		return
	}
	if log != nil {
		log.Info("closing sqlite database")
	}
	if db.reader != nil {
		_ = db.reader.Close()
	}
	if db.writer != nil {
		_ = db.writer.Close()
	}
}

// Healthcheck returns a stateless closure that pings both pools, wrapping any failure
// in ErrHealthcheck. Hand its func(context.Context) error to a readiness probe.
func Healthcheck(db *DB) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := db.writer.PingContext(ctx); err != nil {
			return fmt.Errorf("%w: writer: %v", ErrHealthcheck, err)
		}
		if err := db.reader.PingContext(ctx); err != nil {
			return fmt.Errorf("%w: reader: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
