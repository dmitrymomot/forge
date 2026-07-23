package session

import "time"

// Config holds the expiry policy. A zero MaxTTL or RememberMax means no
// absolute cap: the session lives until logout or the idle timeout.
type Config struct {
	Idle         time.Duration `env:"SESSION_IDLE"`
	MaxTTL       time.Duration `env:"SESSION_MAX_TTL"`
	RememberIdle time.Duration `env:"SESSION_REMEMBER_IDLE"`
	RememberMax  time.Duration `env:"SESSION_REMEMBER_MAX_TTL"`
	Touch        time.Duration `env:"SESSION_TOUCH"`
}

// DefaultConfig returns a 24h idle / 7d absolute session, a 30d idle /
// 1y absolute remembered session, and a 5-minute touch interval.
func DefaultConfig() Config {
	return Config{
		Idle:         24 * time.Hour,
		MaxTTL:       7 * 24 * time.Hour,
		RememberIdle: 30 * 24 * time.Hour,
		RememberMax:  365 * 24 * time.Hour,
		Touch:        5 * time.Minute,
	}
}

// Validate reports whether the Config is usable.
func (c Config) Validate() error {
	if c.Idle <= 0 || c.RememberIdle <= 0 {
		return ErrBadIdle
	}
	if c.MaxTTL < 0 || (c.MaxTTL > 0 && c.MaxTTL < c.Idle) {
		return ErrBadMaxTTL
	}
	if c.RememberMax < 0 || (c.RememberMax > 0 && c.RememberMax < c.RememberIdle) {
		return ErrBadMaxTTL
	}
	// A nonzero Touch must sit strictly below both idle windows: at Touch ==
	// Idle the refresh lands exactly when the session expires, so sliding
	// expiry never actually slides.
	if c.Touch < 0 || (c.Touch > 0 && (c.Touch >= c.Idle || c.Touch >= c.RememberIdle)) {
		return ErrBadTouch
	}
	return nil
}
