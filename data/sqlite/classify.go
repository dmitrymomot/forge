package sqlite

import (
	"database/sql"
	"errors"

	"modernc.org/sqlite"
)

// SQLite extended result codes recognized by the classification predicates.
const (
	codeBusy                 = 5    // SQLITE_BUSY
	codeLocked               = 6    // SQLITE_LOCKED
	codeConstraintForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	codeConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	codeConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// resultCode extracts the extended result code from err if it (or anything it wraps)
// is a *sqlite.Error; ok is false otherwise.
func resultCode(err error) (code int, ok bool) {
	if e, found := errors.AsType[*sqlite.Error](err); found {
		return e.Code(), true
	}
	return 0, false
}

// IsUniqueViolation reports whether err is a UNIQUE or PRIMARY KEY constraint
// violation. Use it instead of importing the driver at the call site.
func IsUniqueViolation(err error) bool {
	code, ok := resultCode(err)
	return ok && (code == codeConstraintUnique || code == codeConstraintPrimaryKey)
}

// IsForeignKeyViolation reports whether err is a FOREIGN KEY constraint violation.
func IsForeignKeyViolation(err error) bool {
	code, ok := resultCode(err)
	return ok && code == codeConstraintForeignKey
}

// IsBusy reports whether err is a busy/locked condition (SQLITE_BUSY/SQLITE_LOCKED and
// their extended variants, which share the primary result code). This is what
// WithTxRetry retries.
func IsBusy(err error) bool {
	code, ok := resultCode(err)
	if !ok {
		return false
	}
	primary := code & 0xFF
	return primary == codeBusy || primary == codeLocked
}

// IsNotFound reports whether err is sql.ErrNoRows — a missing row, not a failure.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
