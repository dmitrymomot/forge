package mmdb

import "errors"

// ErrNoDatabase is returned by New/Reload when neither a city nor an ASN
// database was provided.
var ErrNoDatabase = errors.New("mmdb: no database provided")

// ErrInvalidDatabase is returned when a database's bytes are not a valid
// MaxMind DB (missing marker, truncated, or inconsistent metadata).
var ErrInvalidDatabase = errors.New("mmdb: invalid database")

// ErrUnsupportedFormat is returned when the database binary format major
// version is not 2.
var ErrUnsupportedFormat = errors.New("mmdb: unsupported database format")

// ErrClosed is returned by Lookup after the Reader has been closed.
var ErrClosed = errors.New("mmdb: reader closed")
