package fingerprint

import "time"

// Config configures a Fingerprinter. Secret peppers the identity HMAC and signs
// JS-probe tokens/cookies; it is required.
type Config struct {
	Secret      string        `env:"FINGERPRINT_SECRET"`
	Version     int           `env:"FINGERPRINT_VERSION"`
	TokenTTL    time.Duration `env:"FINGERPRINT_TOKEN_TTL"`
	ProbeCanvas bool          `env:"FINGERPRINT_PROBE_CANVAS"`
	ProbeWebGL  bool          `env:"FINGERPRINT_PROBE_WEBGL"`
}

// DefaultConfig returns a Config with schema version 1 and a 10-minute probe
// token TTL. Secret is still required (set it before use).
func DefaultConfig() Config { return Config{Version: 1, TokenTTL: 10 * time.Minute} }

// Validate reports whether the Config is usable.
func (c Config) Validate() error {
	switch {
	case c.Secret == "":
		return ErrNoSecret
	case c.Version <= 0:
		return ErrBadVersion
	case c.TokenTTL <= 0:
		return ErrBadTokenTTL
	}
	return nil
}
