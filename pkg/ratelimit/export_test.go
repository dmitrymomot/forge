package ratelimit

import "time"

// SetClock overrides the Limiter's time source for deterministic testing of the
// sliding-window decay math. It is only available to tests via export_test.go.
func (l *Limiter) SetClock(now func() time.Time) {
	l.now = now
}
