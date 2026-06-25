package sqlitedb

import "errors"

// Sentinel errors for SQLite database operations.
var (
	// ErrInvalidConfig is returned when the configuration is missing required fields.
	ErrInvalidConfig = errors.New("sqlitedb: invalid configuration")

	// ErrOpenFailed is returned when opening the SQLite database fails.
	ErrOpenFailed = errors.New("sqlitedb: failed to open database")

	// ErrHealthcheckFailed is returned when the health check fails.
	ErrHealthcheckFailed = errors.New("sqlitedb: healthcheck failed")

	// ErrApplyMigrations is returned when applying migrations fails.
	ErrApplyMigrations = errors.New("sqlitedb: failed to apply migrations")
)
