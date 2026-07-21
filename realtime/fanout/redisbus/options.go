package redisbus

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/dmitrymomot/forge/ops/logger"
)

// defaultChannel is the Redis Pub/Sub channel used when WithChannel is not
// configured.
const defaultChannel = "forge:fanout"

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

// WithChannel sets the Redis Pub/Sub channel (default "forge:fanout"). Buses
// only see each other on the same channel; distinct channels isolate
// independent hubs sharing one Redis. Must be non-empty.
func WithChannel(name string) Option {
	return func(c *config) {
		if name == "" {
			c.errs = append(c.errs, fmt.Errorf("redisbus: empty channel"))
			return
		}
		c.channel = name
	}
}

// WithLogger sets the logger for receive-loop diagnostics. Defaults to a
// discard logger; nil is ignored.
func WithLogger(log *slog.Logger) Option {
	return func(c *config) {
		if log != nil {
			c.logger = log
		}
	}
}
