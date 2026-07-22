package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
)

func TestDefaultConfigIsValid(t *testing.T) {
	if err := session.DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() = %v, want nil", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*session.Config)
		want error
	}{
		{"zero idle", func(c *session.Config) { c.Idle = 0 }, session.ErrBadIdle},
		{"negative idle", func(c *session.Config) { c.Idle = -time.Second }, session.ErrBadIdle},
		{"negative maxttl", func(c *session.Config) { c.MaxTTL = -time.Second }, session.ErrBadMaxTTL},
		{"maxttl below idle", func(c *session.Config) { c.Idle = 48 * time.Hour; c.MaxTTL = time.Hour }, session.ErrBadMaxTTL},
		{"zero remember idle", func(c *session.Config) { c.RememberIdle = 0 }, session.ErrBadIdle},
		{"remembermax below remember idle", func(c *session.Config) { c.RememberIdle = 60 * 24 * time.Hour; c.RememberMax = time.Hour }, session.ErrBadMaxTTL},
		{"negative touch", func(c *session.Config) { c.Touch = -time.Second }, session.ErrBadTouch},
		{"touch above idle", func(c *session.Config) { c.Touch = 48 * time.Hour }, session.ErrBadTouch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := session.DefaultConfig()
			tc.mut(&cfg)
			if err := cfg.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestConfigZeroMaxTTLMeansNoCap(t *testing.T) {
	cfg := session.DefaultConfig()
	cfg.MaxTTL = 0
	cfg.RememberMax = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with zero caps = %v, want nil (0 means no absolute cap)", err)
	}
}

func TestDigestIsStableAndNotTheToken(t *testing.T) {
	const token = "s_abc123"
	d1, d2 := session.Digest(token), session.Digest(token)
	if d1 != d2 {
		t.Fatalf("Digest not stable: %q != %q", d1, d2)
	}
	if d1 == token {
		t.Fatal("Digest returned the raw token; the raw token must never be persisted")
	}
	if session.Digest("other") == d1 {
		t.Fatal("Digest collided across distinct tokens")
	}
}
