package validate_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/validate"
)

func TestOneOf(t *testing.T) {
	r := validate.OneOf("red", "green", "blue")
	assert.True(t, r("green").IsZero())
	v := r("pink")
	assert.Equal(t, "validation.one_of", v.Key)
	assert.Equal(t, []validate.Param{{Key: "allowed", Value: []string{"red", "green", "blue"}}}, v.Params)
}

func TestEqualNotEqual(t *testing.T) {
	assert.True(t, validate.Equal("pw")("pw").IsZero())
	assert.Equal(t, "validation.equal", validate.Equal("pw")("nope").Key)

	assert.True(t, validate.NotEqual("old")("new").IsZero())
	assert.Equal(t, "validation.not_equal", validate.NotEqual("old")("old").Key)
}

func TestBeforeAfter(t *testing.T) {
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	early := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	assert.True(t, validate.Before(cutoff)(early).IsZero())
	assert.Equal(t, "validation.before", validate.Before(cutoff)(late).Key)

	assert.True(t, validate.After(cutoff)(late).IsZero())
	assert.Equal(t, "validation.after", validate.After(cutoff)(early).Key)
}
