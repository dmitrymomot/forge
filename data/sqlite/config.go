package sqlite

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config holds the serializable settings for a SQLite database. The env struct tags
// are inert strings — this package imports no config loader. Seed from DefaultConfig
// and overlay an env-parsed copy. Field order is subject to betteralign.
type Config struct {
	Path            string        `env:"SQLITE_PATH"`               // file path (required); ":memory:" special-cased
	JournalMode     string        `env:"SQLITE_JOURNAL_MODE"`       // writer-only pragma; default WAL
	Synchronous     string        `env:"SQLITE_SYNCHRONOUS"`        // default NORMAL (safe+fast under WAL)
	BusyTimeout     time.Duration `env:"SQLITE_BUSY_TIMEOUT"`       // busy_timeout; safety net vs external writers
	ConnMaxIdleTime time.Duration `env:"SQLITE_CONN_MAX_IDLE_TIME"` // reader pool only
	ConnMaxLifetime time.Duration `env:"SQLITE_CONN_MAX_LIFETIME"`  // reader pool only
	MmapSize        int64         `env:"SQLITE_MMAP_SIZE"`          // mmap_size bytes; 0 disables
	CacheSize       int           `env:"SQLITE_CACHE_SIZE"`         // cache_size; negative = KiB
	ReadPoolSize    int           `env:"SQLITE_READ_POOL_SIZE"`     // reader MaxOpenConns; 0 => runtime.NumCPU()
	ForeignKeys     bool          `env:"SQLITE_FOREIGN_KEYS"`       // per-connection foreign_keys pragma
}

// DefaultConfig returns production-sane, throughput-tuned defaults and is the single
// source of truth for them. Path is left empty and must be supplied; DefaultConfig
// alone therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		JournalMode:  "WAL",
		Synchronous:  "NORMAL",
		BusyTimeout:  5 * time.Second,
		ForeignKeys:  true,
		CacheSize:    -16000,    // ~16 MiB (negative = KiB)
		MmapSize:     268435456, // 256 MiB memory-mapped reads
		ReadPoolSize: 0,         // 0 => runtime.NumCPU() at Open
	}
}

var (
	validJournalModes = map[string]bool{"WAL": true, "DELETE": true, "TRUNCATE": true, "PERSIST": true, "MEMORY": true, "OFF": true}
	validSynchronous  = map[string]bool{"OFF": true, "NORMAL": true, "FULL": true, "EXTRA": true}
)

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped error (one wrapped cause per invalid field, joined)
// otherwise. Open also calls it defensively.
func (c Config) Validate() error {
	var errs []error
	if c.Path == "" {
		errs = append(errs, fmt.Errorf("%w: Path must not be empty", ErrInvalidConfig))
	}
	if c.ReadPoolSize < 0 {
		errs = append(errs, fmt.Errorf("%w: ReadPoolSize must be >= 0", ErrInvalidConfig))
	}
	if c.MmapSize < 0 {
		errs = append(errs, fmt.Errorf("%w: MmapSize must be >= 0", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"BusyTimeout", c.BusyTimeout},
		{"ConnMaxIdleTime", c.ConnMaxIdleTime},
		{"ConnMaxLifetime", c.ConnMaxLifetime},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	// JournalMode and Synchronous must be set to a recognized value: an empty
	// string is rejected rather than silently skipped, so a hand-built Config
	// cannot open without WAL and the advertised concurrency guarantees. Seed
	// from DefaultConfig to get them.
	if !validJournalModes[strings.ToUpper(c.JournalMode)] {
		errs = append(errs, fmt.Errorf("%w: JournalMode %q is not a recognized mode (WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF)", ErrInvalidConfig, c.JournalMode))
	}
	if !validSynchronous[strings.ToUpper(c.Synchronous)] {
		errs = append(errs, fmt.Errorf("%w: Synchronous %q is not a recognized level (OFF, NORMAL, FULL, EXTRA)", ErrInvalidConfig, c.Synchronous))
	}
	return errors.Join(errs...)
}
