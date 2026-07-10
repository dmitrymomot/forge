package logsample

import "log/slog"

type config struct {
	rate     int
	minLevel slog.Level
}

// Option configures New.
type Option func(*config)

// WithRate keeps 1 of every n sub-threshold records (default 10). n < 1 is
// clamped to 1 (keep everything).
func WithRate(n int) Option {
	return func(c *config) {
		if n < 1 {
			n = 1
		}
		c.rate = n
	}
}

// WithMinLevel sets the level at or above which records always pass unsampled
// (default slog.LevelWarn).
func WithMinLevel(l slog.Level) Option { return func(c *config) { c.minLevel = l } }
