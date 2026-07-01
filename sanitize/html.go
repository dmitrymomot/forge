package sanitize

import (
	"html"
	"regexp"
)

// tagPattern matches an HTML tag from '<' up to the next '>' (or end of string
// if unclosed). It is intentionally simple: StripTags is plain-text extraction,
// NOT an XSS-safe output filter.
var tagPattern = regexp.MustCompile(`(?s)<[^>]*>?`)

// EscapeHTML escapes special characters (<, >, &, ', ") so s renders as literal
// text inside HTML (html.EscapeString).
func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

// UnescapeHTML reverses EscapeHTML, converting entities back to their runes
// (html.UnescapeString).
func UnescapeHTML(s string) string {
	return html.UnescapeString(s)
}

// StripTags removes <...> tags, yielding the plain text between them.
//
// This is EXTRACTION, not sanitization: the output is NOT safe to inject back
// into HTML. To render untrusted text as HTML, use EscapeHTML instead.
func StripTags(s string) string {
	return tagPattern.ReplaceAllString(s, "")
}
