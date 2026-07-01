package slug

import (
	"strings"
	"unicode"
)

// Make folds arbitrary text to a URL-safe slug. By default the result is
// lowercase, "-" separated, and drawn from [a-z0-9] with a single separator
// between word runs. It returns "" for input with no sluggable characters (unless
// a suffix/min-length option forces a random slug).
func Make(s string, opts ...Option) string {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return build(s, &cfg)
}

// build runs the folding pipeline for the resolved config.
func build(s string, cfg *config) string {
	// 1. custom replacements (literal, applied first).
	for old, repl := range cfg.customReplace {
		s = strings.ReplaceAll(s, old, repl)
	}
	// 2. strip requested characters.
	for _, ch := range cfg.stripChars {
		s = strings.ReplaceAll(s, string(ch), "")
	}
	// 3. per-rune fold: ASCII [a-z0-9] passes through; everything else collapses
	//    to a single separator (Unicode folding is added in a later task).
	var b strings.Builder
	b.Grow(len(s))
	lastWasSep := true // true ⇒ suppress a leading separator
	for _, r := range s {
		if cfg.lowercase {
			r = unicode.ToLower(r)
		}
		if isASCIIAlphaNum(r) {
			b.WriteRune(r)
			lastWasSep = false
			continue
		}
		if !lastWasSep {
			b.WriteString(cfg.separator)
			lastWasSep = true
		}
	}
	result := strings.TrimSuffix(b.String(), cfg.separator)

	// 4. max-length truncation (rune-safe), trimming a dangling separator.
	if cfg.maxLength > 0 {
		result = truncateRunes(result, cfg.maxLength)
		result = strings.TrimSuffix(result, cfg.separator)
	}
	return result
}

// isASCIIAlphaNum reports whether r is a-z, A-Z, or 0-9.
func isASCIIAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// truncateRunes returns at most n runes of s.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
