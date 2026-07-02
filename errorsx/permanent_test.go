package errorsx_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/errorsx"
)

func TestMarkPermanent(t *testing.T) {
	base := errors.New("bad request")
	err := errorsx.MarkPermanent(base)
	require.Error(t, err)

	assert.True(t, errorsx.IsPermanent(err))
	assert.False(t, errorsx.IsRetryable(err))
	assert.Equal(t, "bad request", err.Error(), "marking does not alter the message")
	assert.True(t, errors.Is(err, base), "wrapped base stays matchable")
}

func TestUnmarked_IsRetryableByDefault(t *testing.T) {
	err := errors.New("transient")
	assert.False(t, errorsx.IsPermanent(err))
	assert.True(t, errorsx.IsRetryable(err), "unmarked errors are retryable")
}

func TestIsRetryable_IsNegationOfIsPermanent(t *testing.T) {
	cases := []error{
		errors.New("plain"),
		errorsx.MarkPermanent(errors.New("marked")),
		errorsx.New("code", "coded"),
		errorsx.MarkPermanent(errorsx.New("code", "coded+marked")),
	}
	for _, err := range cases {
		assert.Equal(t, !errorsx.IsPermanent(err), errorsx.IsRetryable(err), err.Error())
	}
}

func TestIsPermanent_AnywhereInChain(t *testing.T) {
	base := errors.New("root")
	marked := errorsx.MarkPermanent(base)
	wrapped := errorsx.WithCode(marked, "conflict") // code on top of a permanent mark

	assert.True(t, errorsx.IsPermanent(wrapped), "mark found beneath a coded wrapper")
	assert.False(t, errorsx.IsRetryable(wrapped))

	code, ok := errorsx.Code(wrapped)
	assert.True(t, ok)
	assert.Equal(t, "conflict", code, "code and permanent mark coexist")

	assert.True(t, errors.Is(wrapped, base), "sentinel still matchable through both wrappers")
}

func TestMarkPermanent_Nil(t *testing.T) {
	assert.Nil(t, errorsx.MarkPermanent(nil))
}

func TestMarkPermanent_Idempotent(t *testing.T) {
	err := errorsx.MarkPermanent(errorsx.MarkPermanent(errors.New("x")))
	assert.True(t, errorsx.IsPermanent(err))
	assert.False(t, errorsx.IsRetryable(err))
}

func TestIsPermanent_Nil(t *testing.T) {
	assert.False(t, errorsx.IsPermanent(nil))
	assert.True(t, errorsx.IsRetryable(nil), "nil has no permanent mark, so IsRetryable is its negation")
}
