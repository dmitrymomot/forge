package migration

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
var (
	// ErrMigrate wraps a failure to build the goose provider or apply migrations.
	ErrMigrate = errors.New("migration: migrate failed")
)
