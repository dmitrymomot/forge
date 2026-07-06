package cookie

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config is the env-loadable cookie policy. Key material rides Keys in
// keyset.WithBase64Keys format ("version:base64,..."). Defaults live in
// DefaultConfig; the canonical loading flow preserves them:
//
//	cfg := cookie.DefaultConfig()
//	err := appconfig.Populate(&cfg)
type Config struct {
	Keys     string        `env:"COOKIE_KEYS"`
	Path     string        `env:"COOKIE_PATH"`
	Domain   string        `env:"COOKIE_DOMAIN"`
	SameSite string        `env:"COOKIE_SAME_SITE"` // lax | strict | none
	MaxAge   time.Duration `env:"COOKIE_MAX_AGE"`
	Secure   bool          `env:"COOKIE_SECURE"`
	HTTPOnly bool          `env:"COOKIE_HTTP_ONLY"`
}

// DefaultConfig returns the secure-by-default policy: Path=/, SameSite=lax,
// Secure and HTTPOnly on, session-lifetime cookies.
func DefaultConfig() Config {
	return Config{Path: "/", SameSite: "lax", Secure: true, HTTPOnly: true}
}

// Validate checks enum fields and the SameSite=none + Secure interaction.
func (c Config) Validate() error {
	if _, err := parseSameSite(c.SameSite); err != nil {
		return err
	}
	if strings.EqualFold(c.SameSite, "none") && !c.Secure {
		return fmt.Errorf("%w: SameSite=none requires Secure", ErrInvalidConfig)
	}
	if c.Path != "" && !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("%w: path %q must start with /", ErrInvalidConfig, c.Path)
	}
	return nil
}

func parseSameSite(s string) (http.SameSite, error) {
	switch strings.ToLower(s) {
	case "", "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	default:
		return 0, fmt.Errorf("%w: unknown SameSite %q", ErrInvalidConfig, s)
	}
}
