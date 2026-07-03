package validate_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/validate"
)

// pass and fail are tiny local rules for exercising composition.
func pass(string) validate.Violation { return validate.Violation{} }
func fail(s string) validate.Violation {
	if s == "" {
		return validate.Violation{Key: "validation.required"}
	}
	return validate.Violation{}
}

func TestApply_TagsFailuresDropsPasses(t *testing.T) {
	// All pass → nil Result.
	assert.Nil(t, validate.Apply("name", "ok", pass, fail))
	// One fails → tagged with the field.
	r := validate.Apply("name", "", pass, fail)
	require.Len(t, r, 1)
	assert.Equal(t, "name", r[0].Field)
	assert.Equal(t, "validation.required", r[0].Key)
}

func TestCheck_NilWhenClean(t *testing.T) {
	err := validate.Check(
		validate.Apply("a", "x", fail),
		validate.Apply("b", "y", fail),
	)
	assert.NoError(t, err) // untyped nil
}

func TestCheck_AggregatesAndErrorString(t *testing.T) {
	err := validate.Check(
		validate.Apply("b", "", fail),
		validate.Apply("a", "", fail),
	)
	require.Error(t, err)
	// Error() is sorted for determinism.
	assert.Equal(t, "a: validation.required; b: validation.required", err.Error())
}

func TestManualAndByField(t *testing.T) {
	err := validate.Check(
		validate.Manual("email", "Email is taken"),
		validate.ManualKey("age", "validation.min", validate.Param{Key: "min", Value: 18}),
	)
	require.Error(t, err)
	errs, ok := err.(validate.Errors)
	require.True(t, ok)
	by := errs.ByField()
	require.Len(t, by["email"], 1)
	assert.Equal(t, "Email is taken", by["email"][0].Message)
	require.Len(t, by["age"], 1)
	assert.Equal(t, "validation.min", by["age"][0].Key)
}

func TestErrors_JSON(t *testing.T) {
	err := validate.Check(validate.Manual("email", "taken"))
	b, e := json.Marshal(err)
	require.NoError(t, e)
	assert.JSONEq(t, `[{"field":"email","message":"taken"}]`, string(b))
}
