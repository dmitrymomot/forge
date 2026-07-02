package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestRequired(t *testing.T) {
	assert.Equal(t, "validation.required", validate.Required("").Key)
	assert.True(t, validate.Required("x").IsZero())
	assert.Equal(t, "validation.required", validate.Required[int](0).Key)
	assert.True(t, validate.Required(1).IsZero())
}

func TestNotBlank(t *testing.T) {
	assert.Equal(t, "validation.not_blank", validate.NotBlank("   ").Key)
	assert.True(t, validate.NotBlank(" x ").IsZero())
	// whitespace-only fails NotBlank but passes Required:
	assert.True(t, validate.Required("   ").IsZero())
}

func TestMinMaxLenBetween(t *testing.T) {
	assert.Equal(t, "validation.min_len", validate.MinLen(3)("ab").Key)
	assert.Equal(t, []validate.Param{{Key: "min", Value: 3}}, validate.MinLen(3)("ab").Params)
	assert.True(t, validate.MinLen(3)("abc").IsZero())

	assert.Equal(t, "validation.max_len", validate.MaxLen(3)("abcd").Key)
	assert.True(t, validate.MaxLen(3)("abc").IsZero())

	assert.True(t, validate.LenBetween(2, 4)("abc").IsZero())
	assert.Equal(t, "validation.len_between", validate.LenBetween(2, 4)("a").Key)

	// rune-counted, not byte-counted:
	assert.True(t, validate.MinLen(2)("é😀").IsZero())
}
