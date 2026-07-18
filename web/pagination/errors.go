package pagination

import "errors"

// ErrOffsetOverflow is wrapped in the request error returned by Parse when a valid page and limit would produce an offset outside the int32 range sqlc commonly generates for PostgreSQL LIMIT and OFFSET parameters.
var ErrOffsetOverflow = errors.New("pagination: offset overflows int32")
