package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL SQLSTATE codes recognized by the classification predicates.
const (
	sqlstateUniqueViolation     = "23505"
	sqlstateForeignKeyViolation = "23503"
	sqlstateCheckViolation      = "23514"
	sqlstateSerializationFail   = "40001"
	sqlstateDeadlockDetected    = "40P01"
)

// sqlState extracts the SQLSTATE code from err if it (or anything it wraps) is a
// *pgconn.PgError; otherwise it returns "".
func sqlState(err error) string {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code
	}
	return ""
}

// IsUniqueViolation reports whether err is a unique-constraint violation (SQLSTATE
// 23505). Use it instead of importing pgconn and matching codes at the call site.
func IsUniqueViolation(err error) bool {
	return sqlState(err) == sqlstateUniqueViolation
}

// IsForeignKeyViolation reports whether err is a foreign-key violation (SQLSTATE
// 23503).
func IsForeignKeyViolation(err error) bool {
	return sqlState(err) == sqlstateForeignKeyViolation
}

// IsCheckViolation reports whether err broke a CHECK constraint or a domain
// constraint (SQLSTATE 23514). A check violation is a rejected value, so it maps to a
// 422 where a unique violation maps to a 409.
func IsCheckViolation(err error) bool {
	return sqlState(err) == sqlstateCheckViolation
}

// IsNotFound reports whether err is pgx.ErrNoRows — a missing row, not a failure.
func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsSerializationFailure reports whether err is a serialization failure (40001) or
// a detected deadlock (40P01) — the two codes WithTxRetry retries.
func IsSerializationFailure(err error) bool {
	code := sqlState(err)
	return code == sqlstateSerializationFail || code == sqlstateDeadlockDetected
}
