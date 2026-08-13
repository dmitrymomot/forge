package hostrouter

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nopHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func TestNew_RejectsInvalidRegistrations(t *testing.T) {
	tests := []struct {
		name     string
		opts     []Option
		sentinel error
	}{
		{"nil handler", []Option{WithHost("x.com", nil)}, ErrNilHandler},
		{"empty pattern", []Option{WithHost("", nopHandler())}, ErrInvalidPattern},
		{"bare wildcard", []Option{WithHost("*.", nopHandler())}, ErrInvalidPattern},
		{"lone star", []Option{WithHost("*", nopHandler())}, ErrInvalidPattern},
		{"double wildcard", []Option{WithHost("*.*.com", nopHandler())}, ErrInvalidPattern},
		{"embedded star", []Option{WithHost("fo*.com", nopHandler())}, ErrInvalidPattern},
		{
			"duplicate exact",
			[]Option{WithHost("x.com", nopHandler()), WithHost("x.com", nopHandler())},
			ErrDuplicateHost,
		},
		{
			"duplicate wildcard",
			[]Option{WithHost("*.x.com", nopHandler()), WithHost("*.x.com", nopHandler())},
			ErrDuplicateHost,
		},
		{"nil fallback", []Option{WithFallback(nil)}, ErrNilHandler},
		{"nil lookup", []Option{WithLookup(nil)}, ErrNilLookup},
		{"nil lookup error handler", []Option{WithLookupErrorHandler(nil)}, ErrNilHandler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(tt.opts...)
			require.Error(t, err)
			assert.Nil(t, r)
			assert.ErrorIs(t, err, tt.sentinel)
		})
	}
}

func TestNew_ReportsEveryInvalidRegistrationAtOnce(t *testing.T) {
	r, err := New(
		WithHost("a.com", nil),
		WithHost("*.*.com", nopHandler()),
		WithLookup(nil),
	)
	require.Error(t, err)
	assert.Nil(t, r)
	assert.ErrorIs(t, err, ErrNilHandler)
	assert.ErrorIs(t, err, ErrInvalidPattern)
	assert.ErrorIs(t, err, ErrNilLookup)
}

func TestNew_AcceptsValidRegistrations(t *testing.T) {
	lookup := func(context.Context, string) (http.Handler, error) { return nil, ErrHostNotFound }

	t.Run("exact and wildcard share a parent", func(t *testing.T) {
		_, err := New(WithHost("x.com", nopHandler()), WithHost("*.x.com", nopHandler()))
		require.NoError(t, err)
	})
	t.Run("repeated fallback: last wins", func(t *testing.T) {
		_, err := New(WithFallback(nopHandler()), WithFallback(nopHandler()))
		require.NoError(t, err)
	})
	t.Run("repeated lookup: last wins", func(t *testing.T) {
		_, err := New(WithLookup(lookup), WithLookup(lookup))
		require.NoError(t, err)
	})
}
