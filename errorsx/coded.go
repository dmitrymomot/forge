package errorsx

import (
	"errors"
	"fmt"
)

// codedError is an unexported error carrying a string code. When it wraps
// another error, Unwrap returns it so errors.Is/As traverse the chain.
type codedError struct {
	err  error // wrapped error (nil for a leaf)
	code string
	msg  string // used when err == nil (leaf, from New/Errorf)
}

func (e *codedError) Error() string {
	msg := e.msg
	if e.err != nil {
		msg = e.err.Error()
	}
	if e.code == "" {
		return msg
	}
	return e.code + ": " + msg
}

// Code reports this error's code. Satisfied by errors.As in the package Code func.
func (e *codedError) Code() string { return e.code }

// Unwrap exposes the wrapped error. It returns nil for a leaf (New/Errorf without
// a %w arg), which stops the errors.Is/As walk at this error.
func (e *codedError) Unwrap() error { return e.err }

// coder is the interface a coded error satisfies. Kept unexported: Code(err)
// finds it via errors.As, callers never name the type.
type coder interface{ Code() string }

// New creates a coded error with a static message.
func New(code, message string) error {
	return &codedError{code: code, msg: message}
}

// Errorf creates a coded error with a formatted message. A %w verb wraps the
// argument, so the returned error's chain includes it (errors.Is/As traverse it).
func Errorf(code, format string, args ...any) error {
	wrapped := fmt.Errorf(format, args...) //nolint:err113 // dynamic message by design
	return &codedError{code: code, err: wrapped}
}

// WithCode attaches code to err, wrapping it so the chain is preserved.
// WithCode(nil, _) returns nil.
func WithCode(err error, code string) error {
	if err == nil {
		return nil
	}
	return &codedError{code: code, err: err}
}

// Code walks err's chain and returns the nearest non-empty code and true, else
// ("", false). Coded wrappers with an empty code are skipped so a deeper code is
// still found.
func Code(err error) (string, bool) {
	for err != nil {
		var c coder
		if !errors.As(err, &c) {
			return "", false
		}
		if code := c.Code(); code != "" {
			return code, true
		}
		// Empty-code coded error: continue past it to look for a deeper code.
		err = errors.Unwrap(err)
	}
	return "", false
}
