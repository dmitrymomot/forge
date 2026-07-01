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
	// 3. per-rune fold into a list of WORDS: ASCII [a-z0-9] passes through;
	//    letters fold via NFKD + special-case map; every other run marks a word
	//    boundary. Keeping words separate (instead of pre-joining) lets truncation
	//    stay separator-boundary-aware — the joining separator is inserted by us,
	//    never by folded content, so it can never be mistaken for content.
	words := foldWords(s, cfg)

	// 4. max-length truncation over whole words (separator-boundary-aware). This
	//    only ever drops inserted joiners or cuts the FIRST word mid-word, so it
	//    never strips real content that coincidentally matches a separator prefix
	//    and never leaves a partial/whole trailing separator.
	result := joinWords(words, cfg.separator, cfg.maxLength)

	// 5. determine whether a RANDOM suffix is required, and its length.
	suffixLen := cfg.suffixLength
	if suffixLen == 0 && isReserved(result, cfg.reservedSlugs) {
		suffixLen = defaultSuffixLength
	}
	if suffixLen > 0 {
		result = withRandomSuffix(words, suffixLen, cfg)
	}

	// 6. min-length padding (after any random suffix already applied).
	if cfg.minLength > 0 && utf8.RuneCountInString(result) < cfg.minLength {
		pad := cfg.minLength - utf8.RuneCountInString(result)
		if result != "" {
			pad += len([]rune(cfg.separator)) // account for the joining separator
		}
		result = padStringWithSuffix(result, pad, cfg)
	}
	return result
}

// foldWords runs the per-rune fold and returns the sequence of folded word runs.
// ASCII [a-z0-9] passes through untouched; Latin letters fold via NFKD + the
// special-case map; every maximal run of non-sluggable runes ends the current
// word. No separators are inserted here — joining is the caller's job, which keeps
// the separator strictly out of the content so truncation can be boundary-aware.
func foldWords(s string, cfg *config) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
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
			cur.WriteString(folded)
			continue
		}
		flush() // non-sluggable run ends the current word
	}
	flush()
	return words
}

// joinWords joins words with sep, honoring a maxLen rune cap (0 = unlimited).
// Truncation is separator-boundary-aware: whole words are appended only while the
// running total (including the joining separator) stays within maxLen. If not even
// the first word fits, that single word is cut mid-word — a word contains no
// inserted separator, so this can never leave a partial/whole separator, and no
// word other than the (possibly cut) first is ever partially emitted.
func joinWords(words []string, sep string, maxLen int) string {
	if len(words) == 0 {
		return ""
	}
	if maxLen <= 0 {
		return strings.Join(words, sep)
	}
	sepLen := utf8.RuneCountInString(sep)
	var b strings.Builder
	total := 0
	for i, w := range words {
		wLen := utf8.RuneCountInString(w)
		if i == 0 {
			// The first word carries no leading separator. If it overflows on its
			// own, cut it mid-word to exactly maxLen (never a separator fragment).
			if wLen > maxLen {
				return truncateRunes(w, maxLen)
			}
			b.WriteString(w)
			total = wLen
			continue
		}
		// Subsequent words cost sep+word; stop before any word that would overflow.
		if total+sepLen+wLen > maxLen {
			break
		}
		b.WriteString(sep)
		b.WriteString(w)
		total += sepLen + wLen
	}
	return b.String()
}

// withRandomSuffix builds a slug from base words plus a random suffix of suffixLen
// runes, honoring cfg.maxLength. The suffix is treated as a trailing word: room is
// reserved for the separator + suffix, the base is truncated by whole words (or the
// first word mid-word) to fit, then base+sep+suffix are joined. This keeps the
// result free of partial/whole dangling separators and preserves base content.
func withRandomSuffix(words []string, suffixLen int, cfg *config) string {
	baseMax := cfg.maxLength
	if cfg.maxLength > 0 {
		sepLen := utf8.RuneCountInString(cfg.separator)
		// Reserve room for sep+suffix; clamp the suffix if it cannot fit at all.
		if suffixLen+sepLen > cfg.maxLength {
			suffixLen = max(cfg.maxLength-sepLen, 0)
		}
		baseMax = max(cfg.maxLength-sepLen-suffixLen, 0)
	}
	base := joinWords(words, cfg.separator, baseMax)
	return appendSuffix(base, randomSuffix(suffixLen, cfg.lowercase), cfg.separator)
}

// padStringWithSuffix appends a random suffix of suffixLen runes to an
// already-joined base string, honoring cfg.maxLength. Used by the min-length pad
// step where the base is a finished slug rather than a word list; it only shrinks
// the suffix (never the base) since padding never intends to drop base content.
func padStringWithSuffix(base string, suffixLen int, cfg *config) string {
	if cfg.maxLength > 0 {
		sepLen := 0
		if base != "" {
			sepLen = utf8.RuneCountInString(cfg.separator)
		}
		if utf8.RuneCountInString(base)+sepLen+suffixLen > cfg.maxLength {
			suffixLen = max(cfg.maxLength-utf8.RuneCountInString(base)-sepLen, 0)
		}
	}
	return appendSuffix(base, randomSuffix(suffixLen, cfg.lowercase), cfg.separator)
}

// isReserved reports whether slug (lowercased) is in the reserved set.
func isReserved(slug string, reserved []string) bool {
	if len(reserved) == 0 {
		return false
	}
	lower := strings.ToLower(slug)
	return slices.Contains(reserved, lower)
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
