package validate_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestContentClasses(t *testing.T) {
	assert.True(t, validate.Alpha("abcDEF").IsZero())
	assert.Equal(t, "validation.alpha", validate.Alpha("abc1").Key)

	assert.True(t, validate.Alphanumeric("abc123").IsZero())
	assert.Equal(t, "validation.alphanumeric", validate.Alphanumeric("abc 123").Key)

	assert.True(t, validate.Numeric("0123").IsZero())
	assert.Equal(t, "validation.numeric", validate.Numeric("12a").Key)

	assert.True(t, validate.ASCII("hello!~").IsZero())
	assert.Equal(t, "validation.ascii", validate.ASCII("héllo").Key)

	assert.True(t, validate.Lowercase("abc").IsZero())
	assert.Equal(t, "validation.lowercase", validate.Lowercase("Abc").Key)

	assert.True(t, validate.Uppercase("ABC").IsZero())
	assert.Equal(t, "validation.uppercase", validate.Uppercase("ABc").Key)

	// empty string passes the class checks (use Required/NotBlank for presence).
	assert.True(t, validate.Alpha("").IsZero())
}

func TestContainsPrefixSuffixMatch(t *testing.T) {
	assert.True(t, validate.Contains("ell")("hello").IsZero())
	v := validate.Contains("zzz")("hello")
	assert.Equal(t, "validation.contains", v.Key)
	assert.Equal(t, []validate.Param{{Key: "sub", Value: "zzz"}}, v.Params)

	assert.True(t, validate.HasPrefix("he")("hello").IsZero())
	assert.Equal(t, "validation.has_prefix", validate.HasPrefix("xx")("hello").Key)

	assert.True(t, validate.HasSuffix("lo")("hello").IsZero())
	assert.Equal(t, "validation.has_suffix", validate.HasSuffix("xx")("hello").Key)

	re := regexp.MustCompile(`^[a-z]+$`)
	assert.True(t, validate.Match(re, "validation.lower_word")("abc").IsZero())
	assert.Equal(t, "validation.lower_word", validate.Match(re, "validation.lower_word")("Abc").Key)
}
