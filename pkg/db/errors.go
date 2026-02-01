package db

import "errors"

var (
	ErrFailedToParseDBConfig    = errors.New("pg: failed to parse database configuration")
	ErrFailedToOpenDBConnection = errors.New("pg: failed to open database connection")
	ErrHealthcheckFailed        = errors.New("pg: healthcheck failed")
	ErrSetDialect               = errors.New("pg migrator: failed to set dialect")
	ErrApplyMigrations          = errors.New("pg migrator: failed to apply migrations")
)
