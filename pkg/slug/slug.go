package slug

import (
	"crypto/rand"
	"slices"
	"strings"
	"unicode"
)

// Option configures the slug generation behavior.
type Option func(*config)

// config holds the configuration for slug generation.
type config struct {
	customReplace map[string]string
	separator     string
	stripChars    string
	reservedSlugs []string // stored in lowercase for case-insensitive matching
	maxLength     int
	minLength     int
	suffixLength  int
	lowercase     bool
}

// defaultConfig returns the default configuration.
func defaultConfig() *config {
	return &config{
		maxLength:     0, // no limit
		minLength:     0, // no limit
		separator:     "-",
		lowercase:     true,
		stripChars:    "",
		customReplace: nil,
		suffixLength:  0,   // no suffix by default
		reservedSlugs: nil, // no reserved slugs by default
	}
}

// WithMaxLength sets the maximum length of the generated slug.
// If the slug exceeds this length, it will be truncated.
func WithMaxLength(n int) Option {
	return func(c *config) {
		c.maxLength = n
	}
}

// WithMinLength sets the minimum length of the generated slug.
// If the slug is shorter than this length, a random suffix will be appended.
func WithMinLength(n int) Option {
	return func(c *config) {
		c.minLength = n
	}
}

// WithSeparator sets the separator character for the slug.
// Default is "-".
func WithSeparator(s string) Option {
	return func(c *config) {
		c.separator = s
	}
}

// WithLowercase controls whether the slug should be converted to lowercase.
// Default is true.
func WithLowercase(enabled bool) Option {
	return func(c *config) {
		c.lowercase = enabled
	}
}

// WithStripChars sets additional characters to strip from the slug.
func WithStripChars(chars string) Option {
	return func(c *config) {
		c.stripChars = chars
	}
}

// WithCustomReplace sets custom string replacements to apply before slugification.
// For example: {"&": "and", "@": "at"}
func WithCustomReplace(replacements map[string]string) Option {
	return func(c *config) {
		c.customReplace = replacements
	}
}

// WithSuffix adds a random alphanumeric suffix to reduce collision possibility.
// The suffix is separated by the configured separator.
// Example: "hello-world-x7g3k2" (with length=6)
func WithSuffix(length int) Option {
	return func(c *config) {
		c.suffixLength = length
	}
}

// WithReservedSlugs sets a list of reserved slugs that cannot be used.
// If the generated slug matches any reserved slug (case-insensitive),
// a random suffix will be automatically appended.
// Example: slug.Make("admin", WithReservedSlugs("admin", "api")) returns "admin-x7g3k2"
func WithReservedSlugs(slugs ...string) Option {
	return func(c *config) {
		// Store reserved slugs in lowercase for case-insensitive comparison
		c.reservedSlugs = make([]string, len(slugs))
		for i, s := range slugs {
			c.reservedSlugs[i] = strings.ToLower(s)
		}
	}
}

// shouldBreakForLength checks if adding a separator would exceed the max length.
func shouldBreakForLength(cfg *config, currentRuneCount int) bool {
	return cfg.maxLength > 0 && currentRuneCount+len(cfg.separator) > cfg.maxLength
}

// Make creates a URL-safe slug from the input string.
// It normalizes the string by replacing spaces and special characters
// with the separator (default "-"), and optionally converts to lowercase.
func Make(s string, opts ...Option) string {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// Apply custom replacements first
	if cfg.customReplace != nil {
		for old, new := range cfg.customReplace {
			s = strings.ReplaceAll(s, old, new)
		}
	}

	// Strip specified characters
	if cfg.stripChars != "" {
		for _, char := range cfg.stripChars {
			s = strings.ReplaceAll(s, string(char), "")
		}
	}

	// Pre-allocate builder with estimated capacity
	var b strings.Builder
	b.Grow(len(s))

	lastWasSep := true // Start as true to avoid leading separator
	runeCount := 0

	for _, r := range s {
		// Check max length (counts runes, not bytes)
		if cfg.maxLength > 0 && runeCount >= cfg.maxLength {
			break
		}

		if cfg.lowercase {
			r = unicode.ToLower(r)
		}

		// ASCII letters and digits pass through unchanged
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastWasSep = false
			runeCount++
			continue
		}

		// Try to normalize diacritics (é → e, ñ → n, etc.)
		if normalized, ok := normalizeDiacritic(r); ok {
			if cfg.lowercase {
				normalized = unicode.ToLower(normalized)
			}
			b.WriteRune(normalized)
			lastWasSep = false
			runeCount++
			continue
		}

		// Replace all other characters with separator, but avoid consecutive separators
		if !lastWasSep {
			if shouldBreakForLength(cfg, runeCount) {
				break
			}
			b.WriteString(cfg.separator)
			lastWasSep = true
			runeCount += len([]rune(cfg.separator))
		}
	}

	result := strings.TrimSuffix(b.String(), cfg.separator)

	return applySuffix(result, cfg)
}

// applySuffix appends at most ONE random suffix to satisfy every constraint that
// requires one: an explicit WithSuffix, a reserved-slug collision, or padding to
// reach minLength. A single cooperating suffix is used (never stacked) so that
// WithReservedSlugs + WithMinLength does not produce a confusing double suffix.
//
// Two policies govern how the suffix interacts with maxLength:
//
//   - Explicit WithSuffix is a fixed-width uniqueness token: it keeps its full
//     requested length, and the slug is truncated (and the separator dropped) as
//     needed to fit within maxLength.
//   - Reserved-collision and minLength padding treat the slug as the priority:
//     the real slug content is preserved and the padding suffix is shrunk to fit;
//     the slug is only truncated as a last resort when even a 1-char suffix would
//     not otherwise fit.
//
// In all cases maxLength is a hard cap on the rune count of the result. minLength
// is a target, not a guarantee against maxLength: when minLength exceeds maxLength
// the cap wins and the result may be shorter than minLength.
func applySuffix(result string, cfg *config) string {
	resultLen := len([]rune(result))

	// Determine whether a suffix is required and from which source.
	explicitSuffix := cfg.suffixLength > 0
	reservedCollision := !explicitSuffix && len(cfg.reservedSlugs) > 0 &&
		slices.Contains(cfg.reservedSlugs, strings.ToLower(result))
	belowMinLength := cfg.minLength > 0 && resultLen < cfg.minLength

	if !explicitSuffix && !reservedCollision && !belowMinLength {
		return result
	}

	sepLen := len([]rune(cfg.separator))

	// Base suffix length: honor an explicit WithSuffix, otherwise use the default
	// of 6 characters for reserved-collision and min-length padding. A single
	// suffix satisfies all of these at once; a reserved slug that is also below
	// minLength gets one 6-char suffix, never two stacked suffixes.
	desiredLen := cfg.suffixLength
	if desiredLen == 0 {
		desiredLen = 6
	}

	if cfg.maxLength <= 0 {
		// No cap: append the full desired suffix.
		return joinSuffix(result, cfg.separator, generateSuffix(desiredLen, cfg.lowercase))
	}

	// Room for a suffix while keeping the full slug and separator.
	available := cfg.maxLength - resultLen - sepLen

	if explicitSuffix {
		// Uniqueness token wins: keep the suffix at full length, fit it by
		// truncating the slug (and dropping the separator) as needed.
		suffixLen := min(desiredLen, cfg.maxLength)
		keep := cfg.maxLength - sepLen - suffixLen
		if keep <= 0 {
			// No room for slug + separator: emit the suffix alone.
			return generateSuffix(suffixLen, cfg.lowercase)
		}
		if resultLen > keep {
			result = string([]rune(result)[:keep])
		}
		return joinSuffix(result, cfg.separator, generateSuffix(suffixLen, cfg.lowercase))
	}

	// Reserved / minLength padding: the slug is the priority.
	switch {
	case available >= desiredLen:
		// Full slug + full desired suffix fit.
		return joinSuffix(result, cfg.separator, generateSuffix(desiredLen, cfg.lowercase))
	case available > 0:
		// Keep the full slug; shrink the padding suffix to the remaining room.
		return joinSuffix(result, cfg.separator, generateSuffix(available, cfg.lowercase))
	default:
		// Even a separator + 1-char suffix does not fit alongside the full slug.
		// Truncate the slug as a last resort to keep a usable suffix.
		suffixLen := min(desiredLen, cfg.maxLength-sepLen)
		keep := cfg.maxLength - sepLen - suffixLen
		if keep <= 0 {
			// No room for any slug content: emit a suffix-only result capped at
			// maxLength, with no separator.
			return generateSuffix(min(desiredLen, cfg.maxLength), cfg.lowercase)
		}
		if resultLen > keep {
			result = string([]rune(result)[:keep])
		}
		return joinSuffix(result, cfg.separator, generateSuffix(suffixLen, cfg.lowercase))
	}
}

// joinSuffix appends a suffix to the slug, inserting the separator only when the
// slug is non-empty (an empty slug yields the bare suffix with no leading separator).
func joinSuffix(result, separator, suffix string) string {
	if result == "" {
		return suffix
	}
	return result + separator + suffix
}

// diacriticMap maps common Latin diacritics to ASCII equivalents.
// Covers major European languages but not exhaustive for all Unicode ranges.
//
// Note on single-character simplifications: a few ligatures and special letters
// are intentionally mapped to a SINGLE ASCII character rather than the more
// conventional two-character transliteration, because slugs favor compactness:
//   - 'ß' -> "s"  (commonly transliterated "ss";  e.g. "straße" -> "strase")
//   - 'æ'/'Æ' -> "a"/"A"  (commonly "ae")
//   - 'œ'/'Œ' -> "o"/"O"  (commonly "oe")
//   - 'ø'/'Ø' -> "o"/"O"  (commonly "o" or "oe")
//
// These choices are stable and documented in doc.go; callers needing the
// two-character forms can supply WithCustomReplace (e.g. {"ß": "ss"}) before
// slugification to override them.
var diacriticMap = map[rune]rune{
	// lowercase a
	'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a', 'ā': 'a', 'ă': 'a', 'ą': 'a',
	// uppercase A
	'À': 'A', 'Á': 'A', 'Â': 'A', 'Ã': 'A', 'Ä': 'A', 'Å': 'A', 'Ā': 'A', 'Ă': 'A', 'Ą': 'A',
	// c/C
	'ç': 'c', 'ć': 'c', 'č': 'c',
	'Ç': 'C', 'Ć': 'C', 'Č': 'C',
	// d/D
	'đ': 'd', 'ď': 'd',
	'Đ': 'D', 'Ď': 'D',
	// e/E
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e', 'ė': 'e', 'ę': 'e', 'ě': 'e',
	'È': 'E', 'É': 'E', 'Ê': 'E', 'Ë': 'E', 'Ē': 'E', 'Ė': 'E', 'Ę': 'E', 'Ě': 'E',
	// i/I
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i', 'į': 'i',
	'Ì': 'I', 'Í': 'I', 'Î': 'I', 'Ï': 'I', 'Ī': 'I', 'Į': 'I',
	// l/L
	'ł': 'l',
	'Ł': 'L',
	// n/N
	'ñ': 'n', 'ń': 'n', 'ň': 'n',
	'Ñ': 'N', 'Ń': 'N', 'Ň': 'N',
	// o/O
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o', 'ø': 'o', 'ō': 'o',
	'Ò': 'O', 'Ó': 'O', 'Ô': 'O', 'Õ': 'O', 'Ö': 'O', 'Ø': 'O', 'Ō': 'O',
	// r/R
	'ř': 'r',
	'Ř': 'R',
	// s/S
	'ś': 's', 'š': 's', 'ș': 's',
	'Ś': 'S', 'Š': 'S', 'Ș': 'S',
	// t/T
	'ť': 't', 'ț': 't',
	'Ť': 'T', 'Ț': 'T',
	// u/U
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u', 'ū': 'u', 'ů': 'u', 'ų': 'u',
	'Ù': 'U', 'Ú': 'U', 'Û': 'U', 'Ü': 'U', 'Ū': 'U', 'Ů': 'U', 'Ų': 'U',
	// y/Y
	'ý': 'y', 'ÿ': 'y',
	'Ý': 'Y', 'Ÿ': 'Y',
	// z/Z
	'ź': 'z', 'ž': 'z', 'ż': 'z',
	'Ź': 'Z', 'Ž': 'Z', 'Ż': 'Z',
	// special characters
	'æ': 'a', // Could also be "ae"
	'Æ': 'A', // Could also be "AE"
	'œ': 'o', // Could also be "oe"
	'Œ': 'O', // Could also be "OE"
	'ß': 's', // Could also be "ss"
}

// normalizeDiacritic attempts to convert a Unicode diacritic to its ASCII equivalent.
// Returns true if normalization was applied, false if character should be handled elsewhere.
func normalizeDiacritic(r rune) (rune, bool) {
	if normalized, ok := diacriticMap[r]; ok {
		return normalized, true
	}
	return r, false
}

// randRead is the source of cryptographic randomness for generateSuffix. It is a
// package-level variable (rather than a direct crypto/rand.Read call) solely so
// tests can substitute a failing reader to exercise the deterministic-fallback
// path. Production code never reassigns it.
var randRead = rand.Read

// generateSuffix creates a random alphanumeric suffix of the specified length.
//
// This is intentionally a bespoke, variable-length random token rather than an
// identifier from pkg/id: it is collision-reducing padding appended to a slug,
// not a primary ID, and its length must be sized dynamically to satisfy the
// minLength/maxLength constraints — pkg/id produces fixed-format IDs that cannot
// be padded or truncated to arbitrary widths. It is therefore not subject to the
// "all IDs via pkg/id" rule.
func generateSuffix(length int, lowercase bool) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	const charsUpper = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	if length <= 0 {
		return ""
	}

	charset := chars
	if !lowercase {
		charset = charsUpper
	}
	n := byte(len(charset))

	out := make([]byte, length)

	// Read a primary batch of random bytes.
	buf := make([]byte, length)
	if _, err := randRead(buf); err != nil {
		// Fallback to a deterministic suffix on randomness failure. This keeps
		// Make total (it never panics) at the cost of predictability, which is
		// acceptable because the suffix is non-cryptographic collision padding.
		for i := range out {
			out[i] = charset[i%len(charset)]
		}
		return string(out)
	}

	// Reject bytes in the biased tail so every character is uniformly likely.
	// The largest multiple of n that fits in a byte is the rejection threshold;
	// bytes at or above it would otherwise over-represent the first 256%n chars.
	limit := byte(256 - (256 % int(n)))
	for i := 0; i < length; {
		for _, v := range buf {
			if v >= limit {
				continue // biased tail: draw again
			}
			out[i] = charset[v%n]
			i++
			if i == length {
				break
			}
		}
		if i == length {
			break
		}
		// Not enough unbiased bytes yet: refill and continue. On a refill failure
		// fall back to deterministic padding for the remaining positions.
		if _, err := randRead(buf); err != nil {
			for ; i < length; i++ {
				out[i] = charset[i%len(charset)]
			}
			break
		}
	}

	return string(out)
}
