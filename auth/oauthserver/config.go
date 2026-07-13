package oauthserver

import (
	"fmt"
	"time"
)

// Config is the env-loadable server configuration.
type Config struct {
	// Issuer is the iss claim on every issued token (the server's public URL).
	Issuer string `env:"OAUTHSERVER_ISSUER"`
	// Audience is the aud claim on access tokens; empty omits the claim.
	Audience string `env:"OAUTHSERVER_AUDIENCE"`
	// TokenTTL is the default access-token lifetime; per-client TokenTTL
	// overrides it.
	TokenTTL time.Duration `env:"OAUTHSERVER_TOKEN_TTL"`
}

// DefaultConfig returns the default policy: 15-minute tokens.
func DefaultConfig() Config {
	return Config{TokenTTL: 15 * time.Minute}
}

// Validate checks required fields.
func (c Config) Validate() error {
	if c.Issuer == "" {
		return fmt.Errorf("%w: Issuer required", ErrInvalidConfig)
	}
	if c.TokenTTL <= 0 {
		return fmt.Errorf("%w: TokenTTL must be positive", ErrInvalidConfig)
	}
	return nil
}
