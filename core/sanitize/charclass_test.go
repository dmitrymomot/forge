package sanitize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/sanitize"
)

func TestKeepAlpha(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc123", "abc"},
		{"a b c", "a b c"}, // spaces kept
		{"a1!b2@c", "abc"},
		{"héllo", "héllo"}, // Unicode letters kept
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.KeepAlpha(c.in), "KeepAlpha(%q)", c.in)
	}
}

func TestKeepDigits(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc123", "123"},
		{"1 2 3", "123"}, // spaces NOT kept
		{"+1 (555) 123-4567", "15551234567"},
		{"nope", ""},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.KeepDigits(c.in), "KeepDigits(%q)", c.in)
	}
}

func TestKeepAlphanumeric(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc123", "abc123"},
		{"a b 1 2", "a b 1 2"}, // spaces kept
		{"a-b_c!1@2", "abc12"},
		{"héllo9", "héllo9"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.KeepAlphanumeric(c.in), "KeepAlphanumeric(%q)", c.in)
	}
}

func TestRemoveChars(t *testing.T) {
	cases := []struct{ in, chars, want string }{
		{"a-b-c", "-", "abc"},
		{"a.b,c;", ".,;", "abc"},
		{"hello", "", "hello"}, // no chars to remove
		{"héllo", "é", "hllo"}, // multibyte rune removal
		{"", "abc", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.RemoveChars(c.in, c.chars), "RemoveChars(%q,%q)", c.in, c.chars)
	}
}
