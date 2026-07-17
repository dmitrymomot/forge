package i18n

import "fmt"

// Config holds the serializable settings for New. The env struct tags are
// inert strings — this package imports no config loader.
type Config struct {
	// DefaultLocale is the fallback locale for every unresolved lookup. It
	// must have messages in the loaded catalogs.
	DefaultLocale string `env:"I18N_DEFAULT_LOCALE"`
	// CookieName is the cookie the default resolver chain reads. Empty
	// disables the cookie resolver.
	CookieName string `env:"I18N_COOKIE_NAME"`
	// QueryParam is the query parameter the default resolver chain reads.
	// Empty disables the query resolver.
	QueryParam string `env:"I18N_QUERY_PARAM"`
}

// DefaultConfig returns the default settings: English default, "lang" cookie
// and query parameter.
func DefaultConfig() Config {
	return Config{
		DefaultLocale: "en",
		CookieName:    "lang",
		QueryParam:    "lang",
	}
}

// Validate reports whether the config is usable. Only DefaultLocale is
// required, and only as a well-formed tag — whether the catalogs actually
// define it is New's business, not Validate's. An empty CookieName or
// QueryParam disables that resolver and is not an error.
func (c Config) Validate() error {
	if normalizeTag(c.DefaultLocale) == "" {
		return fmt.Errorf("%w: DefaultLocale %q is not a language tag", ErrInvalidConfig, c.DefaultLocale)
	}
	return nil
}
