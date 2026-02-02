package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Shutdown returns a function that gracefully closes the database connection pool.
// Use with forge.WithShutdownHook().
//
// Example:
//
//	app := forge.New(
//	    forge.WithShutdownHook(db.Shutdown(pool)),
//	)
func Shutdown(pool *pgxpool.Pool) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		pool.Close()
		return nil
	}
}
