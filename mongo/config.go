package mongo

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// Config holds the serializable settings for a Mongo client. The env struct tags
// are inert strings — this package imports no config loader. Seed from
// DefaultConfig and parse the environment over it with whatever loader reads env
// tags. Defaults live solely in DefaultConfig (there are no envDefault tags to
// drift from it). Field order is subject to the repo's betteralign tooling.
type Config struct {
	URI                    string        `env:"URI"`             // mongodb://… (required)
	Database               string        `env:"DATABASE"`        // optional default db name; a passthrough the consumer reads (client.Database(cfg.Database)) — Open does NOT apply it
	ReadPreference         string        `env:"READ_PREFERENCE"` // primary, primaryPreferred, secondary, secondaryPreferred, nearest
	ReadConcern            string        `env:"READ_CONCERN"`    // local, majority, available, linearizable, snapshot
	WriteConcern           string        `env:"WRITE_CONCERN"`   // majority, journaled, unacknowledged, or a w-number ("1", "2", …)
	MaxPoolSize            uint64        `env:"MAX_POOL_SIZE"`
	MinPoolSize            uint64        `env:"MIN_POOL_SIZE"`
	ConnectTimeout         time.Duration `env:"CONNECT_TIMEOUT"`
	ServerSelectionTimeout time.Duration `env:"SERVER_SELECTION_TIMEOUT"`
	MaxConnIdleTime        time.Duration `env:"MAX_CONN_IDLE_TIME"`
	RetryInterval          time.Duration `env:"RETRY_INTERVAL"`
	RetryAttempts          int           `env:"RETRY_ATTEMPTS"`
}

// DefaultConfig returns production-sane defaults and is the single source of truth
// for them. URI has no default — it is required, so DefaultConfig alone fails
// Validate. Empty concern strings mean "use the driver default" (no override).
func DefaultConfig() Config {
	return Config{
		MaxPoolSize:            100,
		MinPoolSize:            0,
		ConnectTimeout:         10 * time.Second,
		ServerSelectionTimeout: 10 * time.Second,
		MaxConnIdleTime:        0,
		RetryAttempts:          3,
		RetryInterval:          time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Open calls it
// defensively; callers may call it after env-loading. Unknown concern strings are
// rejected here by deferring to the same pure parsers Open uses.
func (c Config) Validate() error {
	var errs []error
	if c.URI == "" {
		errs = append(errs, fmt.Errorf("%w: URI must not be empty", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"ConnectTimeout", c.ConnectTimeout},
		{"ServerSelectionTimeout", c.ServerSelectionTimeout},
		{"MaxConnIdleTime", c.MaxConnIdleTime},
		{"RetryInterval", c.RetryInterval},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	if _, err := parseReadPreference(c.ReadPreference); err != nil {
		errs = append(errs, fmt.Errorf("%w: %v", ErrInvalidConfig, err))
	}
	if _, err := parseReadConcern(c.ReadConcern); err != nil {
		errs = append(errs, fmt.Errorf("%w: %v", ErrInvalidConfig, err))
	}
	if _, err := parseWriteConcern(c.WriteConcern); err != nil {
		errs = append(errs, fmt.Errorf("%w: %v", ErrInvalidConfig, err))
	}
	return errors.Join(errs...)
}

// parseReadPreference maps a Config string to a driver read preference. An empty
// string yields (nil, nil): leave the driver default in place. An unknown value is
// an error. Pure and server-free so Validate and tests can call it directly.
func parseReadPreference(s string) (*readpref.ReadPref, error) {
	switch s {
	case "":
		return nil, nil
	case "primary":
		return readpref.Primary(), nil
	case "primaryPreferred":
		return readpref.PrimaryPreferred(), nil
	case "secondary":
		return readpref.Secondary(), nil
	case "secondaryPreferred":
		return readpref.SecondaryPreferred(), nil
	case "nearest":
		return readpref.Nearest(), nil
	default:
		return nil, fmt.Errorf("unknown ReadPreference %q", s)
	}
}

// parseReadConcern maps a Config string to a driver read concern. Empty => (nil,
// nil) (driver default); unknown => error.
func parseReadConcern(s string) (*readconcern.ReadConcern, error) {
	switch s {
	case "":
		return nil, nil
	case "local":
		return readconcern.Local(), nil
	case "majority":
		return readconcern.Majority(), nil
	case "available":
		return readconcern.Available(), nil
	case "linearizable":
		return readconcern.Linearizable(), nil
	case "snapshot":
		return readconcern.Snapshot(), nil
	default:
		return nil, fmt.Errorf("unknown ReadConcern %q", s)
	}
}

// parseWriteConcern maps a Config string to a driver write concern. Empty => (nil,
// nil) (driver default). Named concerns (majority, journaled, unacknowledged) and
// a plain w-number (e.g. "1", "2") are accepted; anything else is an error.
func parseWriteConcern(s string) (*writeconcern.WriteConcern, error) {
	switch s {
	case "":
		return nil, nil
	case "majority":
		return writeconcern.Majority(), nil
	case "journaled":
		return writeconcern.Journaled(), nil
	case "unacknowledged":
		return writeconcern.Unacknowledged(), nil
	default:
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return &writeconcern.WriteConcern{W: n}, nil
		}
		return nil, fmt.Errorf("unknown WriteConcern %q", s)
	}
}
