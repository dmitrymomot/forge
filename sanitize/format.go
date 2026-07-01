package sanitize

import (
	"net/url"
	"strings"
	"unicode"
)

// Email canonicalizes an email address: trim surrounding whitespace and
// lowercase. It does NOT validate — see validate.Email.
func Email(s string) string {
	return strings.ToLower(Trim(s))
}

// Username canonicalizes a username: trim + lowercase, keep only [a-z0-9._-],
// then trim leading/trailing separators (., _, -). It does NOT validate.
func Username(s string) string {
	s = strings.ToLower(Trim(s))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "._-")
}

// Filename returns a safe base filename: strip any path (both / and \, so it is
// OS-independent), remove control characters, and strip leading dots. Returns
// "" when nothing safe remains.
func Filename(s string) string {
	s = Trim(s)
	// Reduce to the base name regardless of separator style.
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	s = StripControl(s)
	s = strings.TrimLeft(s, ".")
	return s
}

// HeaderValue strips CR, LF, and control characters to guard against HTTP
// header / response-splitting injection.
func HeaderValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// allowedURLSchemes is the allowlist for SanitizeURL. A relative URL (empty
// scheme) is also allowed.
var allowedURLSchemes = map[string]bool{
	"http":   true,
	"https":  true,
	"mailto": true,
}

// SanitizeURL neutralizes dangerous schemes (javascript:, data:, vbscript:, and
// anything else outside the allowlist) by returning "". Otherwise it returns the
// trimmed URL. http/https/mailto and relative URLs pass.
func SanitizeURL(s string) string {
	s = Trim(s)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.Scheme == "" {
		return s // relative URL
	}
	if allowedURLSchemes[strings.ToLower(u.Scheme)] {
		return s
	}
	return ""
}
