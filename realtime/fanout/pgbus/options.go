package pgbus

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/dmitrymomot/forge/ops/logger"
)

// defaultChannel is the Postgres NOTIFY channel used when WithChannel is not
// configured.
const defaultChannel = "forge_fanout"

// maxChannelLen is Postgres's identifier limit (NAMEDATALEN-1). LISTEN
// silently truncates longer names while pg_notify does not, so longer
// channels would never match.
const maxChannelLen = 63

type config struct {
	logger  *slog.Logger
	channel string
	errs    []error
}

func defaultConfig() *config {
	return &config{
		logger:  logger.NewNope(),
		channel: defaultChannel,
	}
}

func (c *config) err() error {
	return errors.Join(c.errs...)
}

// Option configures New.
type Option func(*config)

// WithChannel sets the Postgres NOTIFY channel (default "forge_fanout").
// Buses only see each other on the same channel; distinct channels isolate
// independent hubs sharing one database. Must be non-empty and at most 63
// bytes.
func WithChannel(name string) Option {
	return func(c *config) {
		if name == "" || len(name) > maxChannelLen {
			c.errs = append(c.errs, fmt.Errorf("pgbus: channel must be 1..%d bytes, got %d", maxChannelLen, len(name)))
			return
		}
		c.channel = name
	}
}

// WithLogger sets the logger for reconnect and dropped-message diagnostics.
// Defaults to a discard logger; nil is ignored.
func WithLogger(log *slog.Logger) Option {
	return func(c *config) {
		if log != nil {
			c.logger = log
		}
	}
}
