package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/validate"
)

func nonEmpty(s string) validate.Violation {
	if s == "" {
		return validate.Violation{Key: "validation.required"}
	}
	return validate.Violation{}
}
func min2(s string) validate.Violation {
	if len(s) < 2 {
		return validate.Violation{Key: "validation.min_len", Params: []validate.Param{{Key: "min", Value: 2}}}
	}
	return validate.Violation{}
}

func TestAnd_FirstFailureWins(t *testing.T) {
	r := validate.And(nonEmpty, min2)
	assert.True(t, r("ab").IsZero())
	assert.Equal(t, "validation.required", r("").Key) // nonEmpty fails first
	assert.Equal(t, "validation.min_len", r("a").Key)
}

func TestOr_AnyPass(t *testing.T) {
	r := validate.Or("validation.either", nonEmpty, min2)
	assert.True(t, r("").IsZero() == false) // "" fails both → violation with the Or key
	assert.Equal(t, "validation.either", r("").Key)
	assert.True(t, r("ab").IsZero()) // passes both
	assert.True(t, r("a").IsZero())  // nonEmpty passes → Or passes
}

func TestNot_Inverts(t *testing.T) {
	r := validate.Not(nonEmpty, "validation.must_be_empty")
	assert.Equal(t, "validation.must_be_empty", r("x").Key) // nonEmpty passed → Not fails
	assert.True(t, r("").IsZero())                          // nonEmpty failed → Not passes
}

func TestEach_ReportsIndex(t *testing.T) {
	r := validate.Each(nonEmpty)
	assert.True(t, r([]string{"a", "b"}).IsZero())
	v := r([]string{"a", "", "c"})
	assert.Equal(t, "validation.required", v.Key)
	// index of the first failing element travels as a param.
	assert.Contains(t, v.Params, validate.Param{Key: "index", Value: 1})
}

func TestMsg_InterpolatesParams(t *testing.T) {
	r := validate.Msg(min2, "must be at least {min} chars")
	assert.True(t, r("ab").IsZero())
	v := r("a")
	assert.Equal(t, "must be at least 2 chars", v.Message)
	assert.Equal(t, "validation.min_len", v.Key) // key preserved
}

func TestMsg_UnknownPlaceholderVerbatim(t *testing.T) {
	v := validate.Msg(min2, "min {nope}")("a")
	assert.Equal(t, "min {nope}", v.Message)
}

func TestWithKey_SwapsKey(t *testing.T) {
	v := validate.WithKey(nonEmpty, "signup.name_required")("")
	assert.Equal(t, "signup.name_required", v.Key)
	assert.True(t, validate.WithKey(nonEmpty, "x")("ok").IsZero()) // no-op on pass
}

func TestWhen_LazyOnFalse(t *testing.T) {
	// When cond is false the guarded rule is never evaluated.
	called := false
	spy := func(string) validate.Violation { called = true; return validate.Violation{Key: "k"} }
	assert.True(t, validate.When(false, spy)("x").IsZero())
	assert.False(t, called)
	assert.Equal(t, "k", validate.When(true, spy)("x").Key)
}

func TestWhenField(t *testing.T) {
	assert.Nil(t, validate.WhenField(false, validate.Apply("f", "", nonEmpty)))
	got := validate.WhenField(true, validate.Apply("f", "", nonEmpty))
	require.Len(t, got, 1)
	assert.Equal(t, "f", got[0].Field)
}
