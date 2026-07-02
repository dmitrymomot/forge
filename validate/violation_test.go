package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestViolation_IsZero(t *testing.T) {
	assert.True(t, validate.Violation{}.IsZero())
	assert.False(t, validate.Violation{Key: "validation.required"}.IsZero())
	assert.False(t, validate.Violation{Message: "boom"}.IsZero())
}

func TestViolation_String(t *testing.T) {
	// Message wins when set; else the Key is the fallback.
	assert.Equal(t, "too short", validate.Violation{Key: "validation.min_len", Message: "too short"}.String())
	assert.Equal(t, "validation.min_len", validate.Violation{Key: "validation.min_len"}.String())
	assert.Equal(t, "", validate.Violation{}.String())
}

func TestRule_IsFuncType(t *testing.T) {
	// A Rule[T] is just func(T) Violation; a literal is a valid rule.
	var r validate.Rule[string] = func(s string) validate.Violation {
		if s == "" {
			return validate.Violation{Key: "empty"}
		}
		return validate.Violation{}
	}
	assert.True(t, r("x").IsZero())
	assert.Equal(t, "empty", r("").Key)
}
