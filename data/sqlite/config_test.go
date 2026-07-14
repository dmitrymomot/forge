package sqlite_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func TestDefaultConfig_FailsValidateWithoutPath(t *testing.T) {
	if err := sqlite.DefaultConfig().Validate(); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig for empty Path, got %v", err)
	}
}

func TestValidate_AcceptsMinimalValidConfig(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidate_RejectsBadValues(t *testing.T) {
	base := func() sqlite.Config { c := sqlite.DefaultConfig(); c.Path = "app.db"; return c }
	cases := map[string]func(*sqlite.Config){
		"negative read pool":  func(c *sqlite.Config) { c.ReadPoolSize = -1 },
		"negative mmap":       func(c *sqlite.Config) { c.MmapSize = -1 },
		"negative busy":       func(c *sqlite.Config) { c.BusyTimeout = -time.Second },
		"unknown journal":     func(c *sqlite.Config) { c.JournalMode = "BOGUS" },
		"unknown synchronous": func(c *sqlite.Config) { c.Synchronous = "SOMETIMES" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base()
			mutate(&c)
			if err := c.Validate(); !errors.Is(err, sqlite.ErrInvalidConfig) {
				t.Fatalf("want ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestValidate_AcceptsZeroMmapAndCaseInsensitiveModes(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	cfg.MmapSize = 0
	cfg.JournalMode = "wal"
	cfg.Synchronous = "normal"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
