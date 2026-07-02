package sanitize_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/sanitize"
)

func TestApply(t *testing.T) {
	upper := func(s string) string { return strings.ToUpper(s) }
	trim := strings.TrimSpace

	assert.Equal(t, "ABC", sanitize.Apply("  abc  ", trim, upper))
	assert.Equal(t, "  abc  ", sanitize.Apply("  abc  "), "no transforms returns input unchanged")
	assert.Equal(t, "abc", sanitize.Apply("abc", func(s string) string { return s }), "identity transform")
}

func TestApplyGenericNonString(t *testing.T) {
	inc := func(n int) int { return n + 1 }
	dbl := func(n int) int { return n * 2 }
	// left-to-right: (1+1)*2 = 4
	assert.Equal(t, 4, sanitize.Apply(1, inc, dbl))
}

func TestCompose(t *testing.T) {
	pipeline := sanitize.Compose(strings.TrimSpace, strings.ToLower)
	assert.Equal(t, "ann lee", pipeline("  Ann lee "))

	// A composed pipeline is reusable across calls.
	assert.Equal(t, "bob", pipeline("  BOB "))

	// Empty compose is identity.
	id := sanitize.Compose[string]()
	assert.Equal(t, "x", id("x"))
}

func TestComposeOrderMatchesApply(t *testing.T) {
	inc := func(n int) int { return n + 1 }
	dbl := func(n int) int { return n * 2 }
	p := sanitize.Compose(inc, dbl)
	assert.Equal(t, sanitize.Apply(3, inc, dbl), p(3), "Compose runs in the same order as Apply")
}
