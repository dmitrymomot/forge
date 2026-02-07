package middlewares_test

import (
	"errors"
	"testing"

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
