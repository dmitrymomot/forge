package slug

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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
	// 1. custom replacements (literal, applied first). Go randomizes map
	//    iteration order, so apply keys in a deterministic order: longest key
	//    first (so a longer key wins over its prefixes), ties broken
	//    lexicographically. This keeps the output stable across runs.
	for _, old := range sortedReplaceKeys(cfg.customReplace) {
		s = strings.ReplaceAll(s, old, cfg.customReplace[old])
	}
	// 2. strip requested characters.
	for _, ch := range cfg.stripChars {
		s = strings.ReplaceAll(s, string(ch), "")
	}
	// 3. per-rune fold: ASCII [a-z0-9] passes through; letters fold via NFKD +
	//    special-case map; every other run collapses to a single separator.
	var b strings.Builder
	b.Grow(len(s))
	lastWasSep := true // true ⇒ suppress a leading separator
	for _, r := range s {
		if cfg.lowercase {
			r = unicode.ToLower(r)
		}
		if folded, ok := foldRune(r); ok {
			// NFKD can emit uppercase ASCII (㏞ ⇒ "Vm", ㎅ ⇒ "KB") even when the
			// source rune had no lowercase form, so lowercase the fold output too.
			if cfg.lowercase {
				folded = strings.ToLower(folded)
			}
			b.WriteString(folded)
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
		result = trimDanglingSeparator(result, cfg.separator)
	}

	// 5. determine whether a RANDOM suffix is required, and its length.
	suffixLen := cfg.suffixLength
	if suffixLen == 0 && isReserved(result, cfg.reservedSlugs) {
		suffixLen = defaultSuffixLength
	}
	if suffixLen > 0 {
		result = withRandomSuffix(result, suffixLen, cfg)
	}

	// 6. min-length padding (after any random suffix already applied).
	if cfg.minLength > 0 && utf8.RuneCountInString(result) < cfg.minLength {
		pad := cfg.minLength - utf8.RuneCountInString(result)
		if result != "" {
			pad += len([]rune(cfg.separator)) // account for the joining separator
		}
		result = withRandomSuffix(result, pad, cfg)
	}
	return result
}

// isReserved reports whether slug (lowercased) is in the reserved set.
func isReserved(slug string, reserved []string) bool {
	if len(reserved) == 0 {
		return false
	}
	lower := strings.ToLower(slug)
	return slices.Contains(reserved, lower)
}

// withRandomSuffix appends a random suffix of suffixLen runes to base, honoring
// cfg.maxLength by shrinking the suffix and, if needed, truncating the base so the
// total rune count never exceeds the cap.
func withRandomSuffix(base string, suffixLen int, cfg *config) string {
	sepLen := 0
	if base != "" {
		sepLen = utf8.RuneCountInString(cfg.separator)
	}
	if cfg.maxLength > 0 {
		// Shrink the suffix if the whole thing would overflow.
		if utf8.RuneCountInString(base)+sepLen+suffixLen > cfg.maxLength {
			// First try trimming the base to make room for the full suffix.
			maxBase := max(cfg.maxLength-sepLen-suffixLen, 0)
			base = truncateRunes(base, maxBase)
			base = trimDanglingSeparator(base, cfg.separator)
			if base == "" {
				sepLen = 0
			}
			// If even a zero-length base cannot fit the suffix, clamp the suffix.
			if suffixLen > cfg.maxLength-sepLen {
				suffixLen = cfg.maxLength - sepLen
			}
			if suffixLen < 0 {
				suffixLen = 0
			}
		}
	}
	return appendSuffix(base, randomSuffix(suffixLen, cfg.lowercase), cfg.separator)
}

// appendSuffix joins base and suffix with sep, or returns suffix alone when base
// is empty (no leading separator).
func appendSuffix(base, suffix, sep string) string {
	if suffix == "" {
		return base
	}
	if base == "" {
		return suffix
	}
	return base + sep + suffix
}

// sortedReplaceKeys returns the keys of repl in a deterministic order: longest
// first so a longer key is applied before (and thus wins over) any of its
// prefixes, with equal-length keys ordered lexicographically for a stable result.
func sortedReplaceKeys(repl map[string]string) []string {
	keys := make([]string, 0, len(repl))
	for k := range repl {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if d := len(b) - len(a); d != 0 { // longer key first
			return d
		}
		return strings.Compare(a, b) // stable lexicographic tie-break
	})
	return keys
}

// trimDanglingSeparator removes a trailing and leading run of separator
// characters left behind after truncation. Because a multi-rune separator can be
// cut mid-separator, a plain TrimSuffix would leave a partial fragment; this
// trims any trailing bytes that form a (non-empty) prefix of the separator, then
// removes any remaining whole separators from both ends.
func trimDanglingSeparator(s, sep string) string {
	if sep == "" || s == "" {
		return s
	}
	sepRunes := []rune(sep)
	// Trim a trailing partial-or-whole separator: try the longest separator
	// prefix that s ends with and strip it, repeating until none remains.
	for {
		trimmed := false
		for n := len(sepRunes); n >= 1; n-- {
			frag := string(sepRunes[:n])
			if strings.HasSuffix(s, frag) {
				s = s[:len(s)-len(frag)]
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	// Trim any leading whole separators (a leading fragment cannot occur because
	// folding never emits a leading separator, but be defensive and symmetric).
	for sep != "" && strings.HasPrefix(s, sep) {
		s = s[len(sep):]
	}
	return s
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
