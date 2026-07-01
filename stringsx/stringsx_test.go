package stringsx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/stringsx"
)

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", stringsx.Truncate("abcdef", 3))
	assert.Equal(t, "abc", stringsx.Truncate("abc", 5), "shorter than n unchanged")
	assert.Equal(t, "", stringsx.Truncate("abc", 0))
	assert.Equal(t, "hél", stringsx.Truncate("héllo", 3), "rune-safe (é is one rune)")
}

func TestEllipsis(t *testing.T) {
	assert.Equal(t, "abc…", stringsx.Ellipsis("abcdef", 3))
	assert.Equal(t, "abc", stringsx.Ellipsis("abc", 3), "exact length not truncated")
	assert.Equal(t, "", stringsx.Ellipsis("abc", 0))
}

func TestTruncateWords(t *testing.T) {
	assert.Equal(t, "one two", stringsx.TruncateWords("one two three four", 2))
	assert.Equal(t, "one two", stringsx.TruncateWords("one two", 5), "fewer words unchanged")
	assert.Equal(t, "", stringsx.TruncateWords("one two", 0))
}

func TestMask(t *testing.T) {
	assert.Equal(t, "******123", stringsx.Mask("secret123", 3))
	assert.Equal(t, "*********", stringsx.Mask("secret123", 0), "keep<=0 masks all")
	assert.Equal(t, "*****", stringsx.Mask("short", 10), "keep>=len masks all (no leak)")
	assert.Equal(t, "****", stringsx.Mask("café", 0), "rune count, not byte count")
}

func TestPluralize(t *testing.T) {
	cases := []struct {
		word string
		n    int
		want string
	}{
		{"cat", 2, "cats"},
		{"box", 2, "boxes"},
		{"bus", 2, "buses"},
		{"church", 2, "churches"},
		{"city", 2, "cities"},
		{"day", 2, "days"}, // vowel+y keeps y
		{"cat", 1, "cat"},  // n==1 unchanged
		{"", 2, ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, stringsx.Pluralize(c.word, c.n), "Pluralize(%q,%d)", c.word, c.n)
	}
}
