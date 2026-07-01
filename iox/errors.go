package iox

import "errors"

// ErrLimitExceeded is returned by a LimitReader once the input exceeds the
// configured limit.
var ErrLimitExceeded = errors.New("iox: read limit exceeded")
