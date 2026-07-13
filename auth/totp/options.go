package totp

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	clk          clock.Clock
	scope        func(context.Context) (string, error)
	issuer       string
	digits       int
	period       time.Duration
	algorithm    Algorithm
	skew         int
	backupCount  int
	backupLength int
}

func defaultConfig() config {
	return config{
		digits:       6,
		period:       30 * time.Second,
		algorithm:    SHA1,
		skew:         1,
		clk:          clock.System(),
		backupCount:  10,
		backupLength: 10,
	}
}

// Option configures New and NewManager.
type Option func(*config)

// WithIssuer sets the issuer shown in authenticator apps and embedded in
// provisioning URIs. Empty (the default) omits the issuer from the URI.
func WithIssuer(s string) Option { return func(c *config) { c.issuer = s } }

// WithDigits sets the code length: 6 (default) or 8.
func WithDigits(n int) Option { return func(c *config) { c.digits = n } }

// WithPeriod sets the TOTP time step (default 30s). Whole seconds, >= 1s.
// Authenticator apps widely assume 30s — change only for closed ecosystems.
func WithPeriod(d time.Duration) Option { return func(c *config) { c.period = d } }

// WithAlgorithm selects the HMAC hash (default SHA1 — the variant
// authenticator apps support universally).
func WithAlgorithm(a Algorithm) Option { return func(c *config) { c.algorithm = a } }

// WithSkew sets clock-drift tolerance in steps either side of now
// (default 1; 0 = exact step only).
func WithSkew(n int) Option { return func(c *config) { c.skew = n } }

// WithClock injects a clock for deterministic tests.
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithScope derives a tenant from the request context on every Manager
// operation, so multi-tenant isolation is wired once at construction and no
// call site can forget it. Fail-closed: a hook error or empty scope aborts
// the operation with ErrScope. Single-tenant applications omit this option.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithBackupCodeCount sets how many backup codes ConfirmEnroll and
// RegenerateBackupCodes hand out (default 10, >= 1).
func WithBackupCodeCount(n int) Option { return func(c *config) { c.backupCount = n } }

// WithBackupCodeLength sets backup-code length in characters, excluding
// display dashes (default 10, >= 8).
func WithBackupCodeLength(n int) Option { return func(c *config) { c.backupLength = n } }
