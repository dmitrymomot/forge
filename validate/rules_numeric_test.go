package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestNumeric(t *testing.T) {
	assert.True(t, validate.Min(18)(18).IsZero())
	assert.Equal(t, "validation.min", validate.Min(18)(17).Key)
	assert.Equal(t, []validate.Param{{Key: "min", Value: 18}}, validate.Min(18)(17).Params)

	assert.True(t, validate.Max(120)(120).IsZero())
	assert.Equal(t, "validation.max", validate.Max(120)(121).Key)

	assert.True(t, validate.Between(1, 5)(3).IsZero())
	assert.Equal(t, "validation.between", validate.Between(1, 5)(6).Key)

	// works for floats (cmp.Ordered):
	assert.True(t, validate.Min(1.5)(2.0).IsZero())

	assert.True(t, validate.Positive(1).IsZero())
	assert.Equal(t, "validation.positive", validate.Positive(0).Key)
	assert.Equal(t, "validation.positive", validate.Positive(-1).Key)

	assert.True(t, validate.Negative(-1).IsZero())
	assert.Equal(t, "validation.negative", validate.Negative(0).Key)

	assert.True(t, validate.MultipleOf(5)(15).IsZero())
	assert.Equal(t, "validation.multiple_of", validate.MultipleOf(5)(16).Key)
}
