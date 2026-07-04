// Package sanitize normalizes and escapes UNTRUSTED plain text at trust
// boundaries, and provides a generic Apply/Compose pipeline so the one-arg
// sanitizers (all func(string) string) chain cleanly and interleave with stdlib
// string funcs (strings.ToLower, strings.TrimSpace).
//
// It covers whitespace/control normalization (Trim, Collapse, SingleLine,
// NoSpaces, StripControl), character-class filters (KeepAlpha, KeepDigits,
// KeepAlphanumeric, RemoveChars), HTML escape/strip (EscapeHTML, UnescapeHTML,
// StripTags), and format canonicalizers (Email, Username, Filename,
// HeaderValue, SanitizeURL).
//
// What this is NOT:
//   - NOT a rich-HTML / policy sanitizer (no bluemonday). StripTags is
//     plain-text EXTRACTION and is explicitly NOT XSS-safe output — to render
//     untrusted text as HTML, escape it (EscapeHTML) instead.
//   - NOT a validator. Email/Username/Filename CANONICALIZE; they do not
//     validate. For validation see the validate package.
//   - NOT for trusted developer-facing string shaping (case conversion,
//     Truncate, Mask) — that lives in stringsx.
//   - NOT an injection-prevention layer for SQL/LDAP/shell (forge uses pgx
//     parameterized queries); NOT PII/locale formatting; NOT path
//     normalization (path/filepath); NOT numeric/slice/map helpers
//     (slicex/set).
//
// # Usage
//
//	clean := sanitize.Compose(sanitize.Trim, strings.ToLower, sanitize.Collapse)
//	clean("  Ann   Lee ") // == "ann lee"
//
//	sanitize.Email("  Ann.Lee@Example.COM  ") // == "ann.lee@example.com"
//	sanitize.EscapeHTML("<b>hi</b>")          // == "&lt;b&gt;hi&lt;/b&gt;"
package sanitize
