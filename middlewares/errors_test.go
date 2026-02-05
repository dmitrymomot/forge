package middlewares_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/middlewares"
)

func TestPanicError_Error(t *testing.T) {
	t.Parallel()

	t.Run("formats string panic value", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.PanicError{
			Value: "something went wrong",
			Stack: []byte("stack trace here"),
		}
		require.Equal(t, "panic: something went wrong", err.Error())
	})

	t.Run("formats non-string panic value", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.PanicError{
			Value: 42,
			Stack: nil,
		}
		require.Equal(t, "panic: 42", err.Error())
	})

	t.Run("formats nil panic value", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.PanicError{
			Value: nil,
			Stack: []byte("stack"),
		}
		require.Equal(t, "panic: <nil>", err.Error())
	})
}

func TestTimeoutError_Error(t *testing.T) {
	t.Parallel()

	t.Run("formats timeout duration", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.TimeoutError{
			Duration: 5 * time.Second,
		}
		require.Equal(t, "request timeout after 5s", err.Error())
	})

	t.Run("formats millisecond timeout", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.TimeoutError{
			Duration: 100 * time.Millisecond,
		}
		require.Equal(t, "request timeout after 100ms", err.Error())
	})
}

func TestIsPanicError(t *testing.T) {
	t.Parallel()

	t.Run("returns true for PanicError", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.PanicError{Value: "test"}
		require.True(t, middlewares.IsPanicError(err))
	})

	t.Run("returns true for wrapped PanicError", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.PanicError{Value: "test"}
		wrapped := errors.Join(err, errors.New("other error"))
		require.True(t, middlewares.IsPanicError(wrapped))
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		t.Parallel()

		require.False(t, middlewares.IsPanicError(nil))
	})
}

func TestIsTimeoutError(t *testing.T) {
	t.Parallel()

	t.Run("returns true for TimeoutError", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.TimeoutError{Duration: time.Second}
		require.True(t, middlewares.IsTimeoutError(err))
	})

	t.Run("returns true for wrapped TimeoutError", func(t *testing.T) {
		t.Parallel()

		err := &middlewares.TimeoutError{Duration: time.Second}
		wrapped := errors.Join(err, errors.New("other error"))
		require.True(t, middlewares.IsTimeoutError(wrapped))
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		t.Parallel()

		require.False(t, middlewares.IsTimeoutError(nil))
	})
}

func TestAsPanicError(t *testing.T) {
	t.Parallel()

	t.Run("extracts PanicError", func(t *testing.T) {
		t.Parallel()

		original := &middlewares.PanicError{
			Value: "test panic",
			Stack: []byte("stack"),
		}
		pe, ok := middlewares.AsPanicError(original)
		require.True(t, ok)
		require.Equal(t, original.Value, pe.Value)
		require.Equal(t, original.Stack, pe.Stack)
	})

	t.Run("extracts wrapped PanicError", func(t *testing.T) {
		t.Parallel()

		original := &middlewares.PanicError{Value: "test"}
		wrapped := errors.Join(original, errors.New("other"))
		pe, ok := middlewares.AsPanicError(wrapped)
		require.True(t, ok)
		require.Equal(t, original.Value, pe.Value)
	})

	t.Run("returns false for non-panic error", func(t *testing.T) {
		t.Parallel()

		err := errors.New("regular error")
		_, ok := middlewares.AsPanicError(err)
		require.False(t, ok)
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		t.Parallel()

		pe, ok := middlewares.AsPanicError(nil)
		require.False(t, ok)
		require.Nil(t, pe)
	})
}

func TestAsTimeoutError(t *testing.T) {
	t.Parallel()

	t.Run("extracts TimeoutError", func(t *testing.T) {
		t.Parallel()

		original := &middlewares.TimeoutError{Duration: 5 * time.Second}
		te, ok := middlewares.AsTimeoutError(original)
		require.True(t, ok)
		require.Equal(t, original.Duration, te.Duration)
	})

	t.Run("extracts wrapped TimeoutError", func(t *testing.T) {
		t.Parallel()

		original := &middlewares.TimeoutError{Duration: time.Second}
		wrapped := errors.Join(original, errors.New("other"))
		te, ok := middlewares.AsTimeoutError(wrapped)
		require.True(t, ok)
		require.Equal(t, original.Duration, te.Duration)
	})

	t.Run("returns false for non-timeout error", func(t *testing.T) {
		t.Parallel()

		err := errors.New("regular error")
		_, ok := middlewares.AsTimeoutError(err)
		require.False(t, ok)
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		t.Parallel()

		te, ok := middlewares.AsTimeoutError(nil)
		require.False(t, ok)
		require.Nil(t, te)
	})
}
