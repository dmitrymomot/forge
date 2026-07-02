package sanitize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/sanitize"
)

func TestEscapeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<b>hi</b>`, `&lt;b&gt;hi&lt;/b&gt;`},
		{`a & b`, `a &amp; b`},
		{`"quote"`, `&#34;quote&#34;`},
		{`'apos'`, `&#39;apos&#39;`},
		{"plain", "plain"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.EscapeHTML(c.in), "EscapeHTML(%q)", c.in)
	}
}

func TestUnescapeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{`&lt;b&gt;hi&lt;/b&gt;`, `<b>hi</b>`},
		{`a &amp; b`, `a & b`},
		{`&#34;quote&#34;`, `"quote"`},
		{"plain", "plain"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.UnescapeHTML(c.in), "UnescapeHTML(%q)", c.in)
	}
}

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	for _, s := range []string{`<a href="x">& 'y'</a>`, "plain text", "a & b < c > d"} {
		assert.Equal(t, s, sanitize.UnescapeHTML(sanitize.EscapeHTML(s)), "round-trip %q", s)
	}
}

func TestStripTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<b>hi</b>`, `hi`},
		{`<p>one</p><p>two</p>`, `onetwo`},
		{`a <a href="http://x">link</a> b`, `a link b`},
		{`<img src="x"/>caption`, `caption`},
		{`no tags here`, `no tags here`},
		{`unclosed <b tag`, `unclosed `},                  // trailing unclosed '<' onward dropped
		{`<script>alert(1)</script>keep`, `alert(1)keep`}, // NOTE: extraction, NOT XSS-safe
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.StripTags(c.in), "StripTags(%q)", c.in)
	}
}
