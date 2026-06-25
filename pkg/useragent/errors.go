package useragent

import "errors"

var (
	// ErrParsingFailed is a parent sentinel joined into every Parse failure, so
	// callers can match any parse error with errors.Is(err, ErrParsingFailed)
	// while still discriminating the specific cause below.
	ErrParsingFailed = errors.New("failed to parse user agent")

	ErrEmptyUserAgent     = errors.New("empty user agent string")
	ErrMalformedUserAgent = errors.New("malformed user agent string")
	ErrUnknownDevice      = errors.New("unknown device type")
)
