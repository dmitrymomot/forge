package sanitize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/sanitize"
)

func TestTrim(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  abc  ", "abc"},
		{"\t\nabc\r\n", "abc"},
		{"abc", "abc"},
		{"   ", ""},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.Trim(c.in), "Trim(%q)", c.in)
	}
}

func TestCollapse(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Ann   Lee ", "Ann Lee"},
		{"a\t\tb", "a b"},
		{"a\n\nb", "a b"},
		{"one    two\tthree", "one two three"},
		{"   ", ""},
		{"single", "single"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.Collapse(c.in), "Collapse(%q)", c.in)
	}
}

func TestSingleLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"line1\nline2", "line1 line2"},
		{"line1\r\nline2", "line1 line2"},
		{"a b c", "a b c"}, // Unicode line/paragraph separators
		{"  a\n\n  b  ", "a b"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.SingleLine(c.in), "SingleLine(%q)", c.in)
	}
}

func TestNoSpaces(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a b c", "abc"},
		{" a\tb\nc ", "abc"},
		{"no break", "nobreak"}, // NBSP is whitespace
		{"clean", "clean"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.NoSpaces(c.in), "NoSpaces(%q)", c.in)
	}
}

func TestStripControl(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\x00b", "ab"},               // NUL dropped
		{"a\x07b", "ab"},               // BEL (Cc) dropped
		{"a\u200bb", "ab"},             // zero-width space (Cf) dropped
		{"keep me", "keep me"},         // normal spaces preserved
		{"tab\there", "tab\there"},     // tab is a normal space, kept
		{"line\nbreak", "line\nbreak"}, // newline is a normal space, kept
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.StripControl(c.in), "StripControl(%q)", c.in)
	}
}
