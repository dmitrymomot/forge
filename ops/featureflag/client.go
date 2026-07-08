package featureflag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// Client evaluates feature flags over an ordered provider chain. It is
// immutable after New and safe for unlimited concurrent use.
type Client struct {
	identity  func(context.Context) []string
	logger    *slog.Logger
	providers []Provider
}

// New builds a Client from source options (WithProvider/WithFlags/typed
// WithXxx, applied in order — the static set occupies the position of the
// first static option) and adjusters (WithRollout/WithAllow/WithDeny,
// applied after all sources).
func New(opts ...Option) (*Client, error) {
	cfg := config{staticIdx: -1, static: Flags{}}
	for _, opt := range opts {
		opt(&cfg)
	}
	for k, f := range cfg.static {
		if k == "" {
			return nil, ErrEmptyKey
		}
		if f.Rollout < 0 || f.Rollout > 100 {
			return nil, fmt.Errorf("%w: %q got %d", ErrInvalidRollout, k, f.Rollout)
		}
	}
	for _, adj := range cfg.adjust {
		if err := adj(cfg.static); err != nil {
			return nil, err
		}
	}
	if cfg.staticIdx >= 0 {
		cfg.chain[cfg.staticIdx] = staticProvider{flags: cfg.static.clone()}
	}
	for _, p := range cfg.chain {
		if p == nil {
			return nil, ErrNilProvider
		}
	}
	return &Client{providers: cfg.chain, identity: cfg.identity, logger: cfg.logger}, nil
}

// staticProvider serves the immutable option-built flag set.
type staticProvider struct {
	flags Flags
}

func (p staticProvider) Flag(_ context.Context, key string) (Flag, bool, error) {
	f, ok := p.flags[key]
	return f, ok, nil
}

func (p staticProvider) All(_ context.Context) (Flags, error) {
	return p.flags.clone(), nil
}

// lookup consults providers in order; errors are logged and treated as a
// miss for that provider so evaluation stays fail-safe.
func (c *Client) lookup(ctx context.Context, key string) (Flag, bool) {
	for _, p := range c.providers {
		f, ok, err := p.Flag(ctx, key)
		if err != nil {
			c.warn(ctx, "featureflag: provider error", slog.String("flag", key), slog.Any("error", err))
			continue
		}
		if ok {
			return f, true
		}
	}
	return Flag{}, false
}

func (c *Client) warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	if c.logger == nil {
		return
	}
	c.logger.LogAttrs(ctx, slog.LevelWarn, msg, attrs...)
}

// Bool returns the flag coerced to bool, or def on any miss.
func (c *Client) Bool(ctx context.Context, key string, def bool) bool {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseBool(s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "bool")
		return def
	}
	return v
}

// String returns the flag value, or def on any miss.
func (c *Client) String(ctx context.Context, key, def string) string {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	return s
}

// Int returns the flag coerced to int, or def on any miss.
func (c *Client) Int(ctx context.Context, key string, def int) int {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseInt[int](s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "int")
		return def
	}
	return v
}

// Float64 returns the flag coerced to float64, or def on any miss.
func (c *Client) Float64(ctx context.Context, key string, def float64) float64 {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseFloat[float64](s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "float64")
		return def
	}
	return v
}

// Duration returns the flag coerced to time.Duration, or def on any miss.
func (c *Client) Duration(ctx context.Context, key string, def time.Duration) time.Duration {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseDuration(s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "duration")
		return def
	}
	return v
}

func (c *Client) warnCoerce(ctx context.Context, key, val, typ string) {
	c.warn(ctx, "featureflag: coercion failed",
		slog.String("flag", key), slog.String("value", val), slog.String("type", typ))
}

// value resolves the final string value through the full pipeline:
// lookup → enabled → deny → allow → rollout.
func (c *Client) value(ctx context.Context, key string) (string, bool) {
	id, _ := subjectKey.From(ctx)
	var extra []string
	if c.identity != nil {
		extra = c.identity(ctx)
	}
	return c.valueFor(ctx, key, id, extra)
}

// valueFor is the ctx-carrier-free core shared with Evaluator.
func (c *Client) valueFor(ctx context.Context, key, id string, extra []string) (string, bool) {
	f, ok := c.lookup(ctx, key)
	if !ok || !f.Enabled {
		return "", false
	}
	return eval(f, key, id, extra)
}

// All merges the flag sets of every provider implementing Lister, in chain
// order with first-hit-wins per key (matching evaluation precedence).
// Providers without Lister are skipped. Per-provider errors are joined into
// the returned error while partial results are still returned. Debug/admin
// visibility only — not an evaluation path.
func (c *Client) All(ctx context.Context) (Flags, error) {
	out := Flags{}
	var errs []error
	for _, p := range c.providers {
		l, ok := p.(Lister)
		if !ok {
			continue
		}
		fs, err := l.All(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for k, f := range fs {
			if _, exists := out[k]; !exists {
				out[k] = f
			}
		}
	}
	return out, errors.Join(errs...)
}
