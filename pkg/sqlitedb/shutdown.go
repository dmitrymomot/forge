package sqlitedb

import (
	"context"
	"database/sql"
)

// Shutdown returns a function for graceful database closure.
// Compatible with forge.WithShutdownHook().
//
// Example:
//
//	forge.Run(cfg,
//	    forge.WithShutdownHook(sqlitedb.Shutdown(db)),
//	)
func Shutdown(db *sql.DB) func(context.Context) error {
	return func(_ context.Context) error {
		return db.Close()
	}
}
