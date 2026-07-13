package clickhouse

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// config holds the resolved settings for one Open/OpenDB call. The embedded Config
// carries serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger      *slog.Logger
	withOptions func(*ch.Options)
	errs        []error
	Config
}

// Option configures Open and OpenDB. Invalid values accumulate in the config and are
// returned (joined, ErrInvalidConfig-wrapped) by the constructor before any I/O.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig() (or an env-parsed copy); a bare Config{} zeroes every limit and
// leaves DSN empty (which fails Validate). Options apply in order — place WithConfig
// before any code option you want layered on top of it.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close and lifecycle logging. Default is
// slog.Default(); a nil logger is rejected (ErrInvalidConfig). Pass a discard logger
// to silence logging instead.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithOptions registers the escape hatch that runs LAST in Open/OpenDB, after the
// Config overlay and the LZ4 default, on the fully-built *clickhouse.Options. Use it
// for anything the serializable fields do not cover — TLS config, the Settings map,
// block buffer size, a custom dialer, JWT auth. A nil func is rejected
// (ErrInvalidConfig).
func WithOptions(fn func(*ch.Options)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithOptions received a nil func", ErrInvalidConfig))
			return
		}
		c.withOptions = fn
	}
}

// buildOptions maps a validated Config onto a *clickhouse.Options. It is pure (the
// only work is ParseDSN plus field overlay), so the mapping is unit-testable without a
// server. A DSN parse failure is wrapped in ErrConnect.
func buildOptions(cfg Config) (*ch.Options, error) {
	opts, err := ch.ParseDSN(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("%w: parse dsn: %v", ErrConnect, err)
	}
	if cfg.MaxOpenConns > 0 {
		opts.MaxOpenConns = cfg.MaxOpenConns
	}
	if cfg.MaxIdleConns > 0 {
		opts.MaxIdleConns = cfg.MaxIdleConns
	}
	if cfg.ConnMaxLifetime > 0 {
		opts.ConnMaxLifetime = cfg.ConnMaxLifetime
	}
	if cfg.DialTimeout > 0 {
		opts.DialTimeout = cfg.DialTimeout
	}
	// LZ4-on-by-default: enable LZ4 wire compression only when the DSN did not mention
	// compression at all. ParseDSN leaves Compression nil both when the param is absent
	// AND when it is explicitly disabled (compress=false), so gating on opts.Compression
	// alone would silently re-enable compression the caller turned off — hence the raw
	// DSN check. A compress_level-only DSN yields a non-nil CompressionNone, which the
	// nil check preserves.
	if opts.Compression == nil && !dsnHasParam(cfg.DSN, "compress") {
		opts.Compression = &ch.Compression{Method: ch.CompressionLZ4}
	}
	return opts, nil
}

// dsnHasParam reports whether the DSN query string contains key. It parses only the
// query portion (after the first '?') so it is unaffected by ClickHouse's multi-host
// authority syntax (host1:9000,host2:9000), which net/url cannot parse.
func dsnHasParam(dsn, key string) bool {
	_, query, ok := strings.Cut(dsn, "?")
	if !ok {
		return false
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	return values.Has(key)
}
