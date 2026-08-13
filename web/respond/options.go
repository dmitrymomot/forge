package respond

import (
	"net/http"
)

// config carries what every Response shares: the status, the headers to set before
// the body, and the side effects that must land before the status is committed.
type config struct {
	header http.Header
	before []func(http.ResponseWriter) error
	status int
}

// Option configures one Response. Options apply in order.
type Option func(*config)

func newConfig(status int, opts []Option) config {
	c := config{status: status}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// WithStatus overrides the response status.
func WithStatus(code int) Option {
	return func(c *config) { c.status = code }
}

// WithHeader sets a header before the body is written. Repeating a name replaces it;
// use WithAddedHeader to send a name twice.
func WithHeader(name, value string) Option {
	return func(c *config) {
		if c.header == nil {
			c.header = http.Header{}
		}
		c.header.Set(name, value)
	}
}

// WithAddedHeader appends a value for name rather than replacing it.
func WithAddedHeader(name, value string) Option {
	return func(c *config) {
		if c.header == nil {
			c.header = http.Header{}
		}
		c.header.Add(name, value)
	}
}

// WithBefore registers a side effect that must land before the status is committed —
// writing a flash cookie is the case it exists for, since a redirect's headers are
// gone the moment the status is written:
//
//	respond.SeeOther("/invoices", respond.WithBefore(
//		flash.Setter(flashes, r, flash.Success("the invoice is sent"))))
//
// A failing side effect fails the whole response: it reaches Responder.Fail and no
// body is written, because a redirect that silently lost its message is worse than
// an error. Several hooks run in registration order.
func WithBefore(fn func(w http.ResponseWriter) error) Option {
	return func(c *config) {
		if fn != nil {
			c.before = append(c.before, fn)
		}
	}
}

// applyHeaders writes the configured headers. It runs inside Respond, before the
// underlying render call sets Content-Type and commits the status.
func (c config) applyHeaders(w http.ResponseWriter) {
	if c.header == nil {
		return
	}
	dst := w.Header()
	for name, values := range c.header {
		dst[name] = values
	}
}

// runBefore executes the registered side effects, stopping at the first failure.
func (c config) runBefore(w http.ResponseWriter) error {
	for _, fn := range c.before {
		if err := fn(w); err != nil {
			return err
		}
	}
	return nil
}

// beforeRunner is implemented by every Response this package builds, so Wrap can run
// the side effects without knowing the concrete type. A Response from outside the
// package simply has none.
type beforeRunner interface {
	runBefore(w http.ResponseWriter) error
}
