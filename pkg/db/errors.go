package db

import "errors"

var (
	ErrFailedToParseDBConfig    = errors.New("db: failed to parse database configuration")
	ErrFailedToOpenDBConnection = errors.New("db: failed to open database connection")
	ErrHealthcheckFailed        = errors.New("db: healthcheck failed")
	ErrSetDialect               = errors.New("db migrator: failed to set dialect")
	ErrApplyMigrations          = errors.New("db migrator: failed to apply migrations")
)
