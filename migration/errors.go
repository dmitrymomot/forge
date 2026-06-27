package migration

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
var (
	// ErrInvalidConfig is returned by an option when it receives an invalid value.
	ErrInvalidConfig = errors.New("migration: invalid config")
	// ErrMigrate wraps a failure to build the goose provider or apply migrations.
	ErrMigrate = errors.New("migration: migrate failed")
)
