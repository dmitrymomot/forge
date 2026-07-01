package slug

import "strconv"

// Unique returns Make(s, opts…) when exists reports false for it, otherwise the
// first of "<slug>-2", "<slug>-3", … for which exists(candidate) is false. The
// incrementing numeric suffix is human-friendly (blog-post style) and storage-
// agnostic via the predicate — distinct from the RANDOM suffix of WithSuffix /
// WithReservedSlugs. The joining separator is the one configured via
// WithSeparator (default "-").
func Unique(s string, exists func(candidate string) bool, opts ...Option) string {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	base := build(s, &cfg)
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := base + cfg.separator + strconv.Itoa(i)
		if !exists(candidate) {
			return candidate
		}
	}
}
