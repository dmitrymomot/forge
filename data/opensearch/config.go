package opensearch

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config holds the serializable settings for an OpenSearch client. The env struct
// tags are inert strings — this package imports no config loader. Populate Config
// with any loader that reads env struct tags, typically by seeding from
// DefaultConfig and parsing the environment over it. Addresses is a []string; a
// comma-separated env value (caarlos0/env's default) parses into it under the
// OPENSEARCH_ADDRESSES key. Field order is subject to the repo's betteralign tooling.
type Config struct {
	Username           string        `env:"OPENSEARCH_USERNAME"`             // HTTP basic auth user
	Password           string        `env:"OPENSEARCH_PASSWORD"`             // HTTP basic auth password
	Addresses          []string      `env:"OPENSEARCH_ADDRESSES"`            // node URLs, e.g. https://os:9200 (required)
	MaxRetries         int           `env:"OPENSEARCH_MAX_RETRIES"`          // driver retry count on retriable status codes
	RequestTimeout     time.Duration `env:"OPENSEARCH_REQUEST_TIMEOUT"`      // per-request timeout applied via transport + ctx
	RetryAttempts      int           `env:"OPENSEARCH_RETRY_ATTEMPTS"`       // Open's bounded connect-retry attempts
	RetryInterval      time.Duration `env:"OPENSEARCH_RETRY_INTERVAL"`       // base backoff between connect attempts
	InsecureSkipVerify bool          `env:"OPENSEARCH_INSECURE_SKIP_VERIFY"` // skip TLS verification (dev/self-signed only)
}

// DefaultConfig returns production-sane defaults and is the single source of truth
// for them (there are no envDefault tags to drift from it). Addresses is left empty
// and is required, so DefaultConfig alone fails Validate.
func DefaultConfig() Config {
	return Config{
		MaxRetries:     3,
		RequestTimeout: 10 * time.Second,
		RetryAttempts:  3,
		RetryInterval:  time.Second,
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Open calls it
// defensively; callers may call it after loading from env.
func (c Config) Validate() error {
	var errs []error
	if len(c.Addresses) == 0 {
		errs = append(errs, fmt.Errorf("%w: Addresses must not be empty", ErrInvalidConfig))
	}
	for i, a := range c.Addresses {
		if strings.TrimSpace(a) == "" {
			errs = append(errs, fmt.Errorf("%w: Addresses[%d] must not be blank", ErrInvalidConfig, i))
		}
	}
	if c.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("%w: MaxRetries must be >= 0", ErrInvalidConfig))
	}
	if c.RequestTimeout < 0 {
		errs = append(errs, fmt.Errorf("%w: RequestTimeout must be >= 0", ErrInvalidConfig))
	}
	if c.RetryAttempts < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryAttempts must be >= 0", ErrInvalidConfig))
	}
	if c.RetryInterval < 0 {
		errs = append(errs, fmt.Errorf("%w: RetryInterval must be >= 0", ErrInvalidConfig))
	}
	return errors.Join(errs...)
}
