package clickhouse_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := clickhouse.DefaultConfig()
	if cfg.DSN != "" {
		t.Fatalf("DefaultConfig DSN = %q, want empty", cfg.DSN)
	}
	if cfg.MaxOpenConns != 10 {
		t.Fatalf("MaxOpenConns = %d, want 10", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Fatalf("MaxIdleConns = %d, want 5", cfg.MaxIdleConns)
	}
	if cfg.RetryAttempts != 3 {
		t.Fatalf("RetryAttempts = %d, want 3", cfg.RetryAttempts)
	}
	// DefaultConfig alone must fail Validate because DSN is empty.
	if err := cfg.Validate(); !errors.Is(err, clickhouse.ErrInvalidConfig) {
		t.Fatalf("DefaultConfig().Validate() = %v, want ErrInvalidConfig", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	valid := func() clickhouse.Config {
		c := clickhouse.DefaultConfig()
		c.DSN = "clickhouse://localhost:9000/db"
		return c
	}
	tests := []struct {
		name    string
		mutate  func(*clickhouse.Config)
		wantErr bool
	}{
		{"valid", func(*clickhouse.Config) {}, false},
		{"empty DSN", func(c *clickhouse.Config) { c.DSN = "" }, true},
		{"negative MaxOpenConns", func(c *clickhouse.Config) { c.MaxOpenConns = -1 }, true},
		{"negative MaxIdleConns", func(c *clickhouse.Config) { c.MaxIdleConns = -1 }, true},
		{"idle exceeds open", func(c *clickhouse.Config) { c.MaxOpenConns = 4; c.MaxIdleConns = 8 }, true},
		{"negative RetryAttempts", func(c *clickhouse.Config) { c.RetryAttempts = -1 }, true},
		{"negative duration", func(c *clickhouse.Config) { c.DialTimeout = -time.Second }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := valid()
			tc.mutate(&c)
			err := c.Validate()
			if tc.wantErr {
				if !errors.Is(err, clickhouse.ErrInvalidConfig) {
					t.Fatalf("Validate() = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
