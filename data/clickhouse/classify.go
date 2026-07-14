package clickhouse

import (
	"errors"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// ClickHouse server error codes recognized by the classification predicates. These are
// stable codes from ClickHouse's server-side ErrorCodes table, not driver constants.
const (
	codeTableAlreadyExists = 57  // TABLE_ALREADY_EXISTS
	codeUnknownTable       = 60  // UNKNOWN_TABLE
	codeUnknownDatabase    = 81  // UNKNOWN_DATABASE
	codeAuthFailed         = 516 // AUTHENTICATION_FAILED
)

// Code returns the ClickHouse server error code carried by err if it (or anything it
// wraps) is a *clickhouse.Exception, and reports whether such an exception was found.
// Use it instead of importing the driver and matching *clickhouse.Exception at the
// call site.
func Code(err error) (int32, bool) {
	if e, ok := errors.AsType[*ch.Exception](err); ok {
		return e.Code, true
	}
	return 0, false
}

// IsCode reports whether err carries a *clickhouse.Exception with the given server
// error code.
func IsCode(err error, code int32) bool {
	c, ok := Code(err)
	return ok && c == code
}

// IsTableNotFound reports whether err is a ClickHouse UNKNOWN_TABLE (60) error.
func IsTableNotFound(err error) bool { return IsCode(err, codeUnknownTable) }

// IsDatabaseNotFound reports whether err is a ClickHouse UNKNOWN_DATABASE (81) error.
func IsDatabaseNotFound(err error) bool { return IsCode(err, codeUnknownDatabase) }

// IsAlreadyExists reports whether err is a ClickHouse TABLE_ALREADY_EXISTS (57) error.
func IsAlreadyExists(err error) bool { return IsCode(err, codeTableAlreadyExists) }

// IsAuthFailed reports whether err is a ClickHouse AUTHENTICATION_FAILED (516) error.
// Modern ClickHouse collapses wrong-password and unknown-user into this single code so
// authentication failures do not reveal which half was wrong.
func IsAuthFailed(err error) bool { return IsCode(err, codeAuthFailed) }
