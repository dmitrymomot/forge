package job

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Migrate runs all pending River schema migrations.
// It is safe to call repeatedly — already-applied migrations are skipped.
// This is called automatically by WithJobs, WithJobEnqueuer, and WithJobWorker,
// but is exported for standalone or testing use.
func Migrate(pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(context.Background(), rivermigrate.DirectionUp, nil)
	return err
}
