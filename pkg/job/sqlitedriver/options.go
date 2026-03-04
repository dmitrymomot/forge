package sqlitedriver

import (
	"log/slog"
	"time"
)

// Option configures the SQLite driver.
type Option func(*SQLiteDriver)

// WithLogger sets the logger for the SQLite driver.
func WithLogger(l *slog.Logger) Option {
	return func(d *SQLiteDriver) {
		if l != nil {
			d.logger = l
		}
	}
}

// WithPollInterval sets how frequently the driver polls for pending jobs.
// Default is 1 second.
func WithPollInterval(interval time.Duration) Option {
	return func(d *SQLiteDriver) {
		if interval > 0 {
			d.pollInterval = interval
		}
	}
}
