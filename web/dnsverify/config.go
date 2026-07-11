package dnsverify

import (
	"fmt"
	"time"
)

// Config is the env-loadable deployment config. The resolver is a code-shaped
// seam and lives in options (WithResolver), not env.
type Config struct {
	Label      string        `env:"DNSVERIFY_LABEL"`       // TXT ownership host prefix
	Timeout    time.Duration `env:"DNSVERIFY_TIMEOUT"`     // per-lookup deadline
	TokenBytes int           `env:"DNSVERIFY_TOKEN_BYTES"` // entropy (bytes) of minted tokens
}

// DefaultConfig returns a 5s per-lookup timeout, the "_forge-verify" TXT label,
// and 16-byte tokens.
func DefaultConfig() Config {
	return Config{
		Timeout:    5 * time.Second,
		Label:      "_forge-verify",
		TokenBytes: 16,
	}
}

// Validate rejects a non-positive Timeout, an empty Label, and TokenBytes < 8.
func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: non-positive Timeout", ErrInvalidConfig)
	}
	if c.Label == "" {
		return fmt.Errorf("%w: empty Label", ErrInvalidConfig)
	}
	if c.TokenBytes < 8 {
		return fmt.Errorf("%w: TokenBytes %d (want >= 8)", ErrInvalidConfig, c.TokenBytes)
	}
	return nil
}
