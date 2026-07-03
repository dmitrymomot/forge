package slug

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Unique returns Make(s, opts…) when exists reports false for it, otherwise the
// first of "<slug>-2", "<slug>-3", … for which exists(candidate) is false. The
// incrementing numeric suffix is human-friendly (blog-post style) and storage-
// agnostic via the predicate — distinct from the RANDOM suffix of WithSuffix /
// WithReservedSlugs. The joining separator is the one configured via
// WithSeparator (default "-").
//
// The result honors WithMaxLength like Make does: the base is shrunk (never the
// numeric suffix, which must stay intact for uniqueness) to keep the whole
// candidate within the cap, trimming any separator the cut left dangling. When
// the base folds to "" the candidate is the bare number, with no leading
// separator. If even "<sep><n>" cannot fit maxLength the base is dropped and the
// number is emitted alone (which may exceed maxLength for large n — an inherent
// limit, since the counter cannot be truncated).
func Unique(s string, exists func(candidate string) bool, opts ...Option) string {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	base := build(s, &cfg)
	if !exists(base) {
		return base
	}
	sepLen := utf8.RuneCountInString(cfg.separator)
	for i := 2; ; i++ {
		num := strconv.Itoa(i)
		b := base
		if cfg.maxLength > 0 {
			budget := cfg.maxLength - sepLen - utf8.RuneCountInString(num)
			if budget <= 0 {
				b = ""
			} else {
				b = strings.TrimRight(truncateRunes(base, budget), cfg.separator)
			}
		}
		candidate := num // no leading separator when the base is empty
		if b != "" {
			candidate = b + cfg.separator + num
		}
		if !exists(candidate) {
			return candidate
		}
	}
}
