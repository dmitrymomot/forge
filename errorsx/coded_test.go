package errorsx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/errorsx"
)

func TestNew(t *testing.T) {
	err := errorsx.New("not_found", "user missing")
	require.Error(t, err)
	assert.Equal(t, "not_found: user missing", err.Error())

	code, ok := errorsx.Code(err)
	assert.True(t, ok)
	assert.Equal(t, "not_found", code)
}

func TestNew_EmptyCode(t *testing.T) {
	err := errorsx.New("", "boom")
	assert.Equal(t, "boom", err.Error(), "no code prefix when code is empty")

	code, ok := errorsx.Code(err)
	assert.False(t, ok, "empty code is not reported as a code")
	assert.Equal(t, "", code)
}

func TestErrorf(t *testing.T) {
	err := errorsx.Errorf("invalid", "bad id %d", 42)
	assert.Equal(t, "invalid: bad id 42", err.Error())

	code, ok := errorsx.Code(err)
	assert.True(t, ok)
	assert.Equal(t, "invalid", code)
}

func TestErrorf_WrapsWithVerbW(t *testing.T) {
	sentinel := errors.New("root cause")
	err := errorsx.Errorf("io_error", "reading file: %w", sentinel)
	assert.Equal(t, "io_error: reading file: root cause", err.Error())
	assert.True(t, errors.Is(err, sentinel), "%w chain is preserved through the coded error")

	code, ok := errorsx.Code(err)
	assert.True(t, ok)
	assert.Equal(t, "io_error", code)
}

func TestWithCode(t *testing.T) {
	base := errors.New("connection refused")
	err := errorsx.WithCode(base, "unavailable")
	require.Error(t, err)
	assert.Equal(t, "unavailable: connection refused", err.Error())

	assert.True(t, errors.Is(err, base), "wrapped base is still matchable")

	code, ok := errorsx.Code(err)
	assert.True(t, ok)
	assert.Equal(t, "unavailable", code)
}

func TestWithCode_Nil(t *testing.T) {
	assert.Nil(t, errorsx.WithCode(nil, "whatever"))
}

func TestWithCode_EmptyCode(t *testing.T) {
	base := errors.New("boom")
	err := errorsx.WithCode(base, "")
	assert.Equal(t, "boom", err.Error(), "empty code adds no prefix")
	assert.True(t, errors.Is(err, base))

	code, ok := errorsx.Code(err)
	assert.False(t, ok)
	assert.Equal(t, "", code)
}

func TestCode_NearestWins(t *testing.T) {
	inner := errorsx.New("inner_code", "inner")
	outer := errorsx.WithCode(inner, "outer_code")

	code, ok := errorsx.Code(outer)
	assert.True(t, ok)
	assert.Equal(t, "outer_code", code, "nearest (outermost) code wins")
}

func TestCode_SkipsEmptyCodedFindsDeeper(t *testing.T) {
	inner := errorsx.New("deep_code", "inner")
	middle := errorsx.WithCode(inner, "") // empty-code coded wrapper
	outer := fmt.Errorf("context: %w", middle)

	code, ok := errorsx.Code(outer)
	assert.True(t, ok)
	assert.Equal(t, "deep_code", code, "empty-code wrapper is skipped, deeper code found")
}

func TestCode_NoCode(t *testing.T) {
	code, ok := errorsx.Code(errors.New("plain"))
	assert.False(t, ok)
	assert.Equal(t, "", code)

	code, ok = errorsx.Code(nil)
	assert.False(t, ok)
	assert.Equal(t, "", code)
}

func TestEndToEnd_CodePlusPermanent(t *testing.T) {
	base := errors.New("duplicate key")
	err := errorsx.MarkPermanent(errorsx.WithCode(base, "conflict"))

	code, ok := errorsx.Code(err)
	assert.True(t, ok)
	assert.Equal(t, "conflict", code)
	assert.True(t, errorsx.IsPermanent(err))
	assert.False(t, errorsx.IsRetryable(err))
	assert.True(t, errors.Is(err, base))
	assert.Equal(t, "conflict: duplicate key", err.Error())
}
