package request

import (
	"errors"
	"fmt"
	"net/http"
)

// Source identifies which part of the request a value came from.
type Source string

const (
	SourceQuery  Source = "query"
	SourcePath   Source = "path"
	SourceHeader Source = "header"
	SourceCookie Source = "cookie"
	SourceForm   Source = "form"
	SourceBody   Source = "body"
)

// Kind classifies a request-reading failure so handlers can map it to a status.
type Kind int

const (
	KindMalformed            Kind = iota // unparseable value           -> 400
	KindTooLarge                         // body exceeded the size cap   -> 413
	KindUnsupportedMediaType             // wrong/absent Content-Type    -> 415
	KindInvalidBody                      // malformed/unknown-field JSON -> 400
)

// String returns a short human label for k.
func (k Kind) String() string {
	switch k {
	case KindMalformed:
		return "malformed"
	case KindTooLarge:
		return "too large"
	case KindUnsupportedMediaType:
		return "unsupported media type"
	case KindInvalidBody:
		return "invalid body"
	default:
		return "unknown"
	}
}

// Error is the single error type returned by every request-reading helper. Source
// and Key name the offending input; Kind drives StatusCode; Err is the cause.
type Error struct {
	Err    error
	Source Source
	Key    string
	Kind   Kind
}

// Error returns a single-line description; the wrapped cause is reached via Unwrap.
func (e *Error) Error() string {
	loc := string(e.Source)
	if e.Key != "" {
		loc = fmt.Sprintf("%s %q", e.Source, e.Key)
	}
	if e.Err != nil {
		return fmt.Sprintf("request: %s: %s: %v", loc, e.Kind, e.Err)
	}
	return fmt.Sprintf("request: %s: %s", loc, e.Kind)
}

// Unwrap returns the wrapped cause so errors.Is/As reach it.
func (e *Error) Unwrap() error { return e.Err }

// StatusCode reports the HTTP status for err: a *Error maps by Kind
// (400/413/415); any other non-nil error is 400; nil is 200.
func StatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if e, ok := errors.AsType[*Error](err); ok {
		switch e.Kind {
		case KindTooLarge:
			return http.StatusRequestEntityTooLarge
		case KindUnsupportedMediaType:
			return http.StatusUnsupportedMediaType
		default:
			return http.StatusBadRequest
		}
	}
	return http.StatusBadRequest
}
