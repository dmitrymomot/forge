package slug

import "strings"

// Option configures slug generation. Options are the only knob — there is no builder.
type Option func(*config)

// config holds resolved slug-generation settings. It is unexported; callers mutate
// it only through Option values.
type config struct {
	customReplace map[string]string
	separator     string
	stripChars    string
	reservedSlugs []string // stored lowercase for case-insensitive matching
	maxLength     int
	minLength     int
	suffixLength  int
	lowercase     bool
}

// defaultConfig returns the baseline configuration: lowercase, "-" separated, no
// length limits, no suffix, no reserved words.
func defaultConfig() config {
	return config{
		separator: "-",
		lowercase: true,
	}
}

// WithSeparator sets the separator placed between word runs (default "-").
func WithSeparator(sep string) Option {
	return func(c *config) { c.separator = sep }
}

// WithLowercase controls lowercasing of the result (default true).
func WithLowercase(enabled bool) Option {
	return func(c *config) { c.lowercase = enabled }
}

// WithMaxLength truncates the slug to at most n runes (0 = unlimited).
func WithMaxLength(n int) Option {
	return func(c *config) { c.maxLength = n }
}

// WithMinLength pads the slug with a random suffix until it reaches n runes
// (0 = no minimum).
func WithMinLength(n int) Option {
	return func(c *config) { c.minLength = n }
}

// WithStripChars deletes every rune in chars from the input before folding.
func WithStripChars(chars string) Option {
	return func(c *config) { c.stripChars = chars }
}

// WithCustomReplace applies literal string replacements FIRST, before folding —
// e.g. {"&": "and", "@": "at"}.
func WithCustomReplace(repl map[string]string) Option {
	return func(c *config) { c.customReplace = repl }
}

// WithSuffix appends a random [a-z0-9] suffix of the given length (0 = no suffix),
// separated by the configured separator.
func WithSuffix(length int) Option {
	return func(c *config) { c.suffixLength = length }
}

// WithReservedSlugs appends a random suffix when the generated slug matches any of
// slugs (case-insensitive) — e.g. reserved route names like "admin"/"api".
func WithReservedSlugs(slugs ...string) Option {
	return func(c *config) {
		c.reservedSlugs = make([]string, len(slugs))
		for i, s := range slugs {
			c.reservedSlugs[i] = strings.ToLower(s)
		}
	}
}
