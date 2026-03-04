package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
)

// Healthcheck returns a health check function for the SQLite database.
// The check verifies that the database connection is alive.
// Compatible with forge.CheckFunc.
//
// Example:
//
//	forge.WithHealthChecks(
//	    forge.HealthCheck("db", sqlitedb.Healthcheck(db)),
//	)
func Healthcheck(db *sql.DB) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := db.PingContext(ctx); err != nil {
			return errors.Join(ErrHealthcheckFailed, err)
		}
		return nil
	}
}
