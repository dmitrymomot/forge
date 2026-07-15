package logger

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// defaultAsyncBufferSize is the record buffer capacity when WithAsyncBufferSize is unset.
const defaultAsyncBufferSize = 8192

// CloseFunc drains the async buffer and stops the worker; ctx bounds the drain wait. It is
// idempotent — every call waits for the same completion — and returns ctx.Err() if the
// drain does not finish in time (the worker keeps draining in the background).
type CloseFunc func(ctx context.Context) error

// NewAsync is New with a buffered, single-worker async core beneath context extraction:
// log calls extract context attributes, clone the record, and enqueue without blocking;
// one worker goroutine formats and writes to every destination. When the buffer is full
// new records are dropped and counted, and the worker reports the tally as a Warn record
// ("logger: dropped log records", dropped=N) once it catches up. Records logged after
// Close are silently dropped; records buffered at crash/os.Exit are lost. The returned
// CloseFunc must be called on shutdown, before flushing any downstream sinks.
func NewAsync(opts ...Option) (*slog.Logger, CloseFunc, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}

	base, err := buildBase(c)
	if err != nil {
		return nil, nil, err
	}
	bufSize := c.asyncBufferSize
	if bufSize == 0 {
		bufSize = defaultAsyncBufferSize
	}
	ah := newAsyncHandler(base, bufSize)
	var top slog.Handler = ah
	if len(c.extractors) > 0 {
		top = newContextHandler(top, c.extractors...)
	}
	return slog.New(top), ah.core.close, nil
}

// asyncItem is one queued record bound to the handler that must process it (the base as
// seen through any WithAttrs/WithGroup derivations at enqueue time).
type asyncItem struct {
	ctx    context.Context
	target slog.Handler
	rec    slog.Record
}

// asyncCore is the state shared by an asyncHandler and every handler derived from it via
// WithAttrs/WithGroup: one queue, one worker, one drop counter, one lifecycle.
type asyncCore struct {
	ch      chan asyncItem
	stop    chan struct{}
	done    chan struct{}
	root    slog.Handler // construction-time base; receives drop reports
	dropped atomic.Int64
	closed  atomic.Bool
	once    sync.Once
}

// asyncHandler enqueues records for the shared worker. Enabled delegates to the wrapped
// base, so records below every destination's level are never cloned or enqueued.
type asyncHandler struct {
	core *asyncCore
	base slog.Handler
}

// newAsyncHandler builds the handler and starts the single worker goroutine.
func newAsyncHandler(base slog.Handler, bufSize int) *asyncHandler {
	core := &asyncCore{
		ch:   make(chan asyncItem, bufSize),
		stop: make(chan struct{}),
		done: make(chan struct{}),
		root: base,
	}
	go core.run()
	return &asyncHandler{core: core, base: base}
}

func (h *asyncHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

// Handle clones the record (it shares an attr backing array with the caller) and enqueues
// it without blocking; a full buffer drops the record and counts it. Always returns nil —
// in async mode delivery problems are reported via the drop tally, never to the log call.
func (h *asyncHandler) Handle(ctx context.Context, rec slog.Record) error {
	if h.core.closed.Load() {
		return nil
	}
	select {
	case h.core.ch <- asyncItem{ctx: context.WithoutCancel(ctx), target: h.base, rec: rec.Clone()}:
	default:
		h.core.dropped.Add(1)
	}
	return nil
}

func (h *asyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &asyncHandler{core: h.core, base: h.base.WithAttrs(attrs)}
}

func (h *asyncHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &asyncHandler{core: h.core, base: h.base.WithGroup(name)}
}

// run is the worker loop: process items as they arrive; once stop closes, drain whatever
// remains and exit. Downstream Handle errors are ignored — identical to what slog.Logger
// does with handler errors in sync mode.
func (c *asyncCore) run() {
	defer close(c.done)
	for {
		select {
		case item := <-c.ch:
			c.reportDrops()
			_ = item.target.Handle(item.ctx, item.rec)
		case <-c.stop:
			for {
				select {
				case item := <-c.ch:
					c.reportDrops()
					_ = item.target.Handle(item.ctx, item.rec)
				default:
					c.reportDrops()
					return
				}
			}
		}
	}
}

// reportDrops emits the accumulated drop tally as a Warn record to the construction-time
// base handler, so every destination (gated by its own level) sees drop incidents.
func (c *asyncCore) reportDrops() {
	n := c.dropped.Swap(0)
	if n == 0 {
		return
	}
	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "logger: dropped log records", 0)
	rec.AddAttrs(slog.Int64("dropped", n))
	_ = c.root.Handle(context.Background(), rec)
}

// close implements CloseFunc; NewAsync hands it out as a method value. Records enqueued by
// Handle calls racing with close are either drained or abandoned — both within the
// post-Close silent-drop contract. The data channel is never closed, so a racing send can
// never panic.
func (c *asyncCore) close(ctx context.Context) error {
	c.once.Do(func() {
		c.closed.Store(true)
		close(c.stop)
	})
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
