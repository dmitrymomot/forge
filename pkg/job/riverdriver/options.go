package riverdriver

import "log/slog"

// Option configures the River driver.
type Option func(*RiverDriver)

// WithLogger sets the logger for the River driver.
func WithLogger(l *slog.Logger) Option {
	return func(d *RiverDriver) {
		if l != nil {
			d.logger = l
		}
	}
}
