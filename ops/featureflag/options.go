package featureflag

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// Option configures New.
type Option func(*config)

type config struct {
	static    Flags
	identity  func(context.Context) []string
	logger    *slog.Logger
	chain     []Provider // nil placeholder at staticIdx until New builds the static provider
	adjust    []func(Flags) error
	staticIdx int
}

// WithProvider appends a Provider to the lookup chain; providers are
// consulted in option order, first hit wins.
//
// Declare WithProvider before any typed static option (WithBool/WithString/
// …/WithFlags) when you want the provider to override the static set: all
// static options collapse into a single provider pinned at the position of
// the first static option.
func WithProvider(p Provider) Option {
	return func(c *config) { c.chain = append(c.chain, p) }
}

// WithFlags merges a flag set (typically loaded from the app's config via
// ops/config) into the static set. The static set occupies the chain
// position of the first static option.
func WithFlags(f Flags) Option {
	return func(c *config) {
		c.reserveStatic()
		maps.Copy(c.static, f)
	}
}

// WithBool declares a static bool flag (enabled, rollout 100).
func WithBool(key string, v bool) Option { return withStatic(key, typeconv.Format(v)) }

// WithString declares a static string flag (enabled, rollout 100).
func WithString(key, v string) Option { return withStatic(key, v) }

// WithInt declares a static int flag (enabled, rollout 100).
func WithInt(key string, v int) Option { return withStatic(key, typeconv.Format(v)) }

// WithFloat64 declares a static float flag (enabled, rollout 100).
func WithFloat64(key string, v float64) Option { return withStatic(key, typeconv.Format(v)) }

// WithDuration declares a static duration flag (enabled, rollout 100).
func WithDuration(key string, v time.Duration) Option { return withStatic(key, v.String()) }

func withStatic(key, value string) Option {
	return func(c *config) {
		c.reserveStatic()
		c.static[key] = Flag{Value: value, Enabled: true, Rollout: 100}
	}
}

func (c *config) reserveStatic() {
	if c.staticIdx >= 0 {
		return
	}
	c.staticIdx = len(c.chain)
	c.chain = append(c.chain, nil)
}

// WithRollout sets the rollout percent of an existing static flag.
func WithRollout(key string, percent int) Option {
	return func(c *config) {
		c.adjust = append(c.adjust, func(fs Flags) error {
			if percent < 0 || percent > 100 {
				return fmt.Errorf("%w: %q got %d", ErrInvalidRollout, key, percent)
			}
			f, ok := fs[key]
			if !ok {
				return fmt.Errorf("%w: %q", ErrUnknownFlag, key)
			}
			f.Rollout = percent
			fs[key] = f
			return nil
		})
	}
}

// WithAllow appends always-on tokens to an existing static flag.
func WithAllow(key string, tokens ...string) Option {
	return adjustTokens(key, tokens, func(f *Flag, ts []string) { f.Allow = append(slices.Clone(f.Allow), ts...) })
}

// WithDeny appends always-off tokens to an existing static flag.
func WithDeny(key string, tokens ...string) Option {
	return adjustTokens(key, tokens, func(f *Flag, ts []string) { f.Deny = append(slices.Clone(f.Deny), ts...) })
}

func adjustTokens(key string, tokens []string, apply func(*Flag, []string)) Option {
	return func(c *config) {
		c.adjust = append(c.adjust, func(fs Flags) error {
			if slices.Contains(tokens, "") {
				return fmt.Errorf("%w: empty token for %q", ErrInvalidFlag, key)
			}
			f, ok := fs[key]
			if !ok {
				return fmt.Errorf("%w: %q", ErrUnknownFlag, key)
			}
			apply(&f, tokens)
			fs[key] = f
			return nil
		})
	}
}

// WithIdentity registers the resolver producing the subject's extra tokens
// (role:staff, segment:vip). It runs on every getter call and MUST be O(1)
// over pre-loaded request state — never a database call.
func WithIdentity(fn func(ctx context.Context) []string) Option {
	return func(c *config) { c.identity = fn }
}

// WithLogger sets the logger for provider errors and coercion warnings.
// Without it evaluation is silent.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}
