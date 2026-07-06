package httpserver

import (
	"errors"
	"fmt"
	"time"
)

// Config holds the serializable settings for a Server. The env struct tags are
// inert strings — this package imports no config loader. Populate Config with any
// loader that reads env struct tags, typically by seeding from DefaultConfig and
// parsing the environment over it. Field order is subject to the repo's betteralign
// tooling.
type Config struct {
	Addr              string        `env:"SERVER_ADDR"`          // listen address; ignored when WithListener is used
	Name              string        `env:"SERVER_NAME"`          // empty -> Name() derives from listener/Addr
	TLSCertFile       string        `env:"SERVER_TLS_CERT_FILE"` // both cert+key set -> serve HTTPS
	TLSKeyFile        string        `env:"SERVER_TLS_KEY_FILE"`
	ShutdownTimeout   time.Duration `env:"SERVER_SHUTDOWN_TIMEOUT"`    // drain bound; 0 = wait indefinitely
	ReadHeaderTimeout time.Duration `env:"SERVER_READ_HEADER_TIMEOUT"` // Slowloris guard
	ReadTimeout       time.Duration `env:"SERVER_READ_TIMEOUT"`        // full request read
	WriteTimeout      time.Duration `env:"SERVER_WRITE_TIMEOUT"`       // response write; set 0 for SSE/streaming
	IdleTimeout       time.Duration `env:"SERVER_IDLE_TIMEOUT"`        // keep-alive idle
	MaxHeaderBytes    int           `env:"SERVER_MAX_HEADER_BYTES"`    // 0 = net/http default
}

// DefaultConfig returns the optimal, secure-by-default settings and is the single
// source of truth for defaults (there are no envDefault tags to drift from it).
func DefaultConfig() Config {
	return Config{
		Addr:              ":8080",
		ShutdownTimeout:   15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Callers may call
// it after loading from env (zero-trust); Run also calls it defensively.
func (c Config) Validate() error {
	var errs []error
	if c.Addr == "" {
		errs = append(errs, fmt.Errorf("%w: Addr must not be empty", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"ShutdownTimeout", c.ShutdownTimeout},
		{"ReadHeaderTimeout", c.ReadHeaderTimeout},
		{"ReadTimeout", c.ReadTimeout},
		{"WriteTimeout", c.WriteTimeout},
		{"IdleTimeout", c.IdleTimeout},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.MaxHeaderBytes < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxHeaderBytes must be >= 0", ErrInvalidConfig))
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		errs = append(errs, fmt.Errorf("%w: TLSCertFile and TLSKeyFile must both be set or both empty", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
