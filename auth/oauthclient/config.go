package oauthclient

import (
	"fmt"
	"time"
)

// Config is the env-loadable client configuration. Key material rides Keys
// in keyset.WithBase64Keys format ("version:base64,..."). The canonical
// loading flow preserves defaults:
//
//	cfg := oauthclient.DefaultConfig()
//	err := appconfig.Populate(&cfg)
type Config struct {
	Keys        string        `env:"OAUTHCLIENT_KEYS"`
	RedirectURL string        `env:"OAUTHCLIENT_REDIRECT_URL"`
	CookieName  string        `env:"OAUTHCLIENT_COOKIE_NAME"`
	FlowTTL     time.Duration `env:"OAUTHCLIENT_FLOW_TTL"`
}

// DefaultConfig returns the default flow policy: 10-minute flows in an
// "oauth_flow" cookie.
func DefaultConfig() Config {
	return Config{CookieName: "oauth_flow", FlowTTL: 10 * time.Minute}
}

// Validate checks required fields.
func (c Config) Validate() error {
	if c.Keys == "" {
		return fmt.Errorf("%w: Keys required", ErrInvalidConfig)
	}
	if c.CookieName == "" {
		return fmt.Errorf("%w: CookieName required", ErrInvalidConfig)
	}
	if c.FlowTTL <= 0 {
		return fmt.Errorf("%w: FlowTTL must be positive", ErrInvalidConfig)
	}
	return nil
}
