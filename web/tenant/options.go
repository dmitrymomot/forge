package tenant

import (
	"log/slog"

	"github.com/dmitrymomot/forge/ops/logger"
)

type config struct {
	onError ErrorHandler
	log     *slog.Logger
	sources []Source
}

// Option configures New. Options apply in order and panic on invalid input.
type Option func(*config)

func newConfig(opts ...Option) config {
	c := config{
		onError: defaultErrorHandler,
		log:     logger.NewNope(),
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// WithSources appends sources to the precedence chain in the order given;
// repeated calls keep appending. The first source extracting an identifier
// the Lookup resolves to a live tenant wins. Panics with ErrNilSource when
// any source is nil.
func WithSources(sources ...Source) Option {
	for _, src := range sources {
		if src == nil {
			panic(ErrNilSource)
		}
	}
	return func(c *config) {
		c.sources = append(c.sources, sources...)
	}
}

// WithErrorHandler replaces the response written when resolution fails
// (e.g. to render problem+json instead of the plain-text defaults). It does
// not run when nothing resolves — that passes through untenanted. Last
// wins. Panics with ErrNilComponent when h is nil.
func WithErrorHandler(h ErrorHandler) Option {
	if h == nil {
		panic(ErrNilComponent)
	}
	return func(c *config) { c.onError = h }
}

// WithLogger sets the logger used by Middleware when resolution fails
// (Debug for tenant-rejection errors, Error for infrastructure errors).
// Defaults to a no-op logger. A nil logger is ignored.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.log = l
		}
	}
}
