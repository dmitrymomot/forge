package hostrouter

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nopHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

// recoverErr runs fn and returns the error it panicked with (nil if no panic).
// A non-error panic is wrapped so it still fails an errors.Is assertion loudly
// rather than being silently reported as "no panic".
func recoverErr(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("non-error panic: %v", r)
			}
		}
	}()
	fn()
	return nil
}

func TestWithHost_Panics(t *testing.T) {
	tests := []struct {
		name     string
		build    func()
		sentinel error
	}{
		{"nil handler", func() { New(WithHost("x.com", nil)) }, ErrNilHandler},
		{"empty pattern", func() { New(WithHost("", nopHandler())) }, ErrInvalidPattern},
		{"bare wildcard", func() { New(WithHost("*.", nopHandler())) }, ErrInvalidPattern},
		{"lone star", func() { New(WithHost("*", nopHandler())) }, ErrInvalidPattern},
		{"double wildcard", func() { New(WithHost("*.*.com", nopHandler())) }, ErrInvalidPattern},
		{"embedded star", func() { New(WithHost("fo*.com", nopHandler())) }, ErrInvalidPattern},
		{"duplicate exact", func() {
			New(WithHost("x.com", nopHandler()), WithHost("x.com", nopHandler()))
		}, ErrDuplicateHost},
		{"duplicate wildcard", func() {
			New(WithHost("*.x.com", nopHandler()), WithHost("*.x.com", nopHandler()))
		}, ErrDuplicateHost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := recoverErr(tt.build)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.sentinel)
		})
	}
}

func TestWithFallback_NilPanics(t *testing.T) {
	err := recoverErr(func() { New(WithFallback(nil)) })
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNilHandler)
}

func TestWithHost_NoPanicCases(t *testing.T) {
	// exact + wildcard with the same parent coexist (different maps).
	assert.NotPanics(t, func() {
		New(WithHost("x.com", nopHandler()), WithHost("*.x.com", nopHandler()))
	})
	// repeated WithFallback: last wins, no panic.
	assert.NotPanics(t, func() {
		New(WithFallback(nopHandler()), WithFallback(nopHandler()))
	})
}
