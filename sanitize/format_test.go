package sanitize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/sanitize"
)

func TestEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Ann.Lee@Example.COM  ", "ann.lee@example.com"},
		{"USER@DOMAIN.io", "user@domain.io"},
		{"already@lower.com", "already@lower.com"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.Email(c.in), "Email(%q)", c.in)
	}
}

func TestUsername(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  John_Doe  ", "john_doe"},
		{"Ann.Lee!!", "ann.lee"},
		{"__bob__", "bob"}, // leading/trailing separators trimmed
		{"a b c", "abc"},   // spaces are not allowed chars
		{"weird***name", "weirdname"},
		{"user-name.v2", "user-name.v2"},
		{"...", ""}, // only separators -> empty
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.Username(c.in), "Username(%q)", c.in)
	}
}

func TestFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  report.pdf  ", "report.pdf"},
		{"/etc/passwd", "passwd"},
		{`C:\Windows\evil.exe`, "evil.exe"},
		{"../../secret.txt", "secret.txt"},
		{".hidden", "hidden"},    // leading dots stripped
		{"..", ""},               // nothing safe remains
		{"a\x00b.txt", "ab.txt"}, // control chars removed
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.Filename(c.in), "Filename(%q)", c.in)
	}
}

func TestHeaderValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"normal value", "normal value"},
		{"inject\r\nX-Evil: 1", "injectX-Evil: 1"},
		{"line\nfeed", "linefeed"},
		{"tab\there", "tabhere"}, // tab is a control char, removed
		{"a\x00b", "ab"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.HeaderValue(c.in), "HeaderValue(%q)", c.in)
	}
}

func TestSanitizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  https://example.com/x  ", "https://example.com/x"},
		{"http://ok.com", "http://ok.com"},
		{"HTTP://Ok.com/Path", "HTTP://Ok.com/Path"}, // scheme case-insensitive check, value preserved
		{"/relative/path", "/relative/path"},
		{"mailto:a@b.com", "mailto:a@b.com"},
		{"javascript:alert(1)", ""},
		{"JavaScript:alert(1)", ""}, // scheme match is case-insensitive
		{"data:text/html,<script>", ""},
		{"vbscript:msgbox(1)", ""},
		{"file:///etc/passwd", ""},           // file scheme is not on the allowlist
		{"ftp://host/x", ""},                 // any other scheme is rejected
		{"   ", ""},                          // whitespace-only trims to empty
		{"MAILTO:a@b.com", "MAILTO:a@b.com"}, // allowlisted scheme, case-insensitive
		{"relative/path", "relative/path"},   // schemeless relative URL passes
		{"#fragment", "#fragment"},           // fragment-only is schemeless
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize.SanitizeURL(c.in), "SanitizeURL(%q)", c.in)
	}
}

// TestSanitizeURLParseError drives the url.Parse error branch: inputs that
// survive Trim (no surrounding whitespace to strip) yet fail to parse are
// rejected with "".
func TestSanitizeURLParseError(t *testing.T) {
	cases := []string{
		"http://foo.com/%zz",     // invalid percent-encoding in path
		"http://[::1]:namedport", // invalid port after host
		"foo\x7f.com",            // invalid control character in URL
		"%",                      // bare invalid percent-escape
		"://noscheme",            // missing protocol scheme
	}
	for _, in := range cases {
		assert.Empty(t, sanitize.SanitizeURL(in), "SanitizeURL(%q)", in)
	}
}
