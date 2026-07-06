package secheaders

import (
	"fmt"
	"time"
)

// Config is the env-loadable deployment policy. CSP is code-shaped and lives
// in options (WithCSP/WithNonce), not env.
type Config struct {
	FrameOptions          string        `env:"SECURITY_HEADERS_FRAME_OPTIONS"` // DENY (default) | SAMEORIGIN | off
	CSPReportURI          string        `env:"SECURITY_HEADERS_CSP_REPORT_URI"`
	HSTSMaxAge            time.Duration `env:"SECURITY_HEADERS_HSTS_MAX_AGE"` // 0 disables HSTS
	HSTSIncludeSubdomains bool          `env:"SECURITY_HEADERS_HSTS_SUBDOMAINS"`
}

// DefaultConfig returns FrameOptions=DENY with HSTS and CSP reporting off.
func DefaultConfig() Config {
	return Config{FrameOptions: "DENY"}
}

// Validate checks enum fields. An empty FrameOptions normalizes to DENY.
func (c Config) Validate() error {
	switch c.FrameOptions {
	case "", "DENY", "SAMEORIGIN", "off":
	default:
		return fmt.Errorf("%w: FrameOptions %q (want DENY, SAMEORIGIN, or off)", ErrInvalidConfig, c.FrameOptions)
	}
	if c.HSTSMaxAge < 0 {
		return fmt.Errorf("%w: negative HSTSMaxAge", ErrInvalidConfig)
	}
	return nil
}
