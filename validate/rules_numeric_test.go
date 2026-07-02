package validate_test

import (
	"math"
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

func TestNumericNaN(t *testing.T) {
	nan := math.NaN()

	// NaN must be rejected by range rules (comparisons against NaN are all false).
	assert.Equal(t, "validation.between", validate.Between(0.0, 120.0)(nan).Key)
	assert.Equal(t, "validation.min", validate.Min(0.0)(nan).Key)
	assert.Equal(t, "validation.max", validate.Max(10.0)(nan).Key)

	// float32 NaN too.
	assert.Equal(t, "validation.between", validate.Between[float32](0, 120)(float32(math.NaN())).Key)
	assert.Equal(t, "validation.min", validate.Min[float32](0)(float32(math.NaN())).Key)
	assert.Equal(t, "validation.max", validate.Max[float32](10)(float32(math.NaN())).Key)

	// Sign rules must reject NaN too (v <= 0 / v >= 0 are both false for NaN).
	assert.False(t, validate.Positive(nan).IsZero())
	assert.Equal(t, "validation.positive", validate.Positive(nan).Key)
	assert.False(t, validate.Negative(nan).IsZero())
	assert.Equal(t, "validation.negative", validate.Negative(nan).Key)

	// float32 NaN for sign rules too.
	assert.Equal(t, "validation.positive", validate.Positive(float32(math.NaN())).Key)
	assert.Equal(t, "validation.negative", validate.Negative(float32(math.NaN())).Key)

	// Normal in-range floats still pass.
	assert.True(t, validate.Between(0.0, 120.0)(42.0).IsZero())
	assert.True(t, validate.Min(0.0)(42.0).IsZero())
	assert.True(t, validate.Max(10.0)(5.0).IsZero())

	// Normal signed floats are unaffected.
	assert.True(t, validate.Positive(1.5).IsZero())
	assert.True(t, validate.Negative(-1.5).IsZero())

	// Integers are unaffected.
	assert.True(t, validate.Between(0, 120)(42).IsZero())
	assert.True(t, validate.Min(0)(42).IsZero())
	assert.True(t, validate.Max(10)(5).IsZero())
	assert.True(t, validate.Positive(1).IsZero())
	assert.True(t, validate.Negative(-1).IsZero())
}
