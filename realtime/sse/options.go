package sse

import (
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/realtime/fanout"
)

const (
	defaultKeepAlive   = 15 * time.Second
	defaultSendTimeout = 30 * time.Second
)

type writerConfig struct {
	errs        []error
	sendTimeout time.Duration
}

// WriterOption configures NewWriter.
type WriterOption func(*writerConfig)

// WithSendTimeout bounds how long one Send may block writing to a client.
// The writer clears the server-wide write deadline, so without this bound a
// connected client that stopped reading would pin the goroutine and the
// connection forever; with it the blocked Send fails and ends the stream.
// Zero disables the bound; negative is an error. Defaults to 30s.
//
// When the bound is positive, NewWriter fails closed if the response writer
// cannot set write deadlines (a flushable wrapper without Unwrap): the
// protection cannot be enforced, so pass WithSendTimeout(0) to accept
// unbounded writes explicitly.
func WithSendTimeout(d time.Duration) WriterOption {
	return func(c *writerConfig) {
		if d < 0 {
			c.errs = append(c.errs, fmt.Errorf("sse: send timeout must not be negative, got %s", d))
			return
		}
		c.sendTimeout = d
	}
}

type handlerConfig struct {
	encode     func(fanout.Message) (Event, error)
	log        *slog.Logger
	subOpts    []fanout.SubscribeOption
	writerOpts []WriterOption
	errs       []error
	keepAlive  time.Duration
	retry      time.Duration
}

func newHandlerConfig() *handlerConfig {
	return &handlerConfig{
		log:       logger.NewNope(),
		keepAlive: defaultKeepAlive,
	}
}

// HandlerOption configures NewHandler.
type HandlerOption func(*handlerConfig)

// WithKeepAlive sets how often an idle stream sends a heartbeat comment so
// proxies do not cut the connection. Zero disables heartbeats; negative is an
// error. Defaults to 15s.
func WithKeepAlive(d time.Duration) HandlerOption {
	return func(c *handlerConfig) {
		if d < 0 {
			c.errs = append(c.errs, fmt.Errorf("sse: keep-alive must not be negative, got %s", d))
			return
		}
		c.keepAlive = d
	}
}

// WithRetry announces the client's reconnection delay (a "retry:" frame,
// rounded up to a whole millisecond) at the start of every stream. Must be
// positive; by default nothing is announced and clients keep their own
// default.
func WithRetry(d time.Duration) HandlerOption {
	return func(c *handlerConfig) {
		if d <= 0 {
			c.errs = append(c.errs, fmt.Errorf("sse: retry must be positive, got %s", d))
			return
		}
		c.retry = d
	}
}

// WithEncoder replaces the default fanout.Message → Event mapping (an
// epoch-namespaced message ID as the "id:" resume cursor, topic as the event
// name, payload as data). An encoder that clears Event.ID opts that message out
// of Last-Event-ID resume. An encoder error skips the message — logged, never
// delivered — and the stream continues.
//
// A custom encoder owns the "id:" it emits, so the handler resumes on the raw
// Last-Event-ID (a bare uint64) rather than the default epoch-namespaced form —
// emit stable, cross-instance IDs if you want resume to survive restarts.
func WithEncoder(fn func(fanout.Message) (Event, error)) HandlerOption {
	return func(c *handlerConfig) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("sse: nil encoder"))
			return
		}
		c.encode = fn
	}
}

// WithSubscribeOptions forwards options to every per-request
// hub.Subscribe call — per-stream buffer size or overflow policy, typically.
// Do not pass fanout.WithResumeAfter here: the handler manages resume from
// the Last-Event-ID header.
func WithSubscribeOptions(opts ...fanout.SubscribeOption) HandlerOption {
	return func(c *handlerConfig) {
		c.subOpts = slices.Clone(opts)
	}
}

// WithWriterOptions forwards options to every per-request NewWriter call —
// the per-send timeout, typically.
func WithWriterOptions(opts ...WriterOption) HandlerOption {
	return func(c *handlerConfig) {
		c.writerOpts = slices.Clone(opts)
	}
}

// WithLogger sets the logger for encoder failures. Defaults to a no-op
// logger.
func WithLogger(l *slog.Logger) HandlerOption {
	return func(c *handlerConfig) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("sse: nil logger"))
			return
		}
		c.log = l
	}
}
