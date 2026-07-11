package migration

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
var (
	// ErrMigrate wraps a failure to build the goose provider or apply migrations.
	ErrMigrate = errors.New("migration: migrate failed")

	// ErrDuplicateSource reports that a Group's Sets resolve to the same
	// version table (empty tables default to DefaultTable), which would make
	// goose silently skip one source's migrations as "already applied".
	ErrDuplicateSource = errors.New("migration: Group sources must resolve to distinct version tables")
)
