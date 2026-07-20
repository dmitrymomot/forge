package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmitrymomot/forge/ops/logger"
)

// Sink receives flushed batches. Flush is called from the single flusher
// goroutine, so implementations need no internal synchronization against
// concurrent flushes. The batch slice is owned by the sink after the call. A
// returned error loses the batch (counted in Stats.Lost and logged) — the
// collector never retries; wrap the sink with resilience/retry when a
// transient backend warrants attempts.
type Sink[T any] interface {
	Flush(ctx context.Context, batch []T) error
}

// SinkFunc adapts a function to Sink.
type SinkFunc[T any] func(ctx context.Context, batch []T) error

// Flush calls f.
func (f SinkFunc[T]) Flush(ctx context.Context, batch []T) error { return f(ctx, batch) }

// Stats is a snapshot of the collector's event counters.
type Stats struct {
	// Added is the number of events accepted into the buffer.
	Added uint64
	// Dropped is the number of events rejected by Add on a full buffer.
	Dropped uint64
	// Flushed is the number of events successfully delivered to the sink.
	Flushed uint64
	// Lost is the number of buffered events discarded because Flush failed.
	Lost uint64
}

// entry pairs a buffered event with the scope captured at Add time.
type entry[T any] struct {
	event T
	scope string
}

// Collector buffers events in a bounded in-memory buffer and delivers them to
// the sink in batches. Construct with New, run under ops/supervisor.
type Collector[T any] struct {
	sink Sink[T]
	buf  chan entry[T]
	settings

	added   atomic.Uint64
	dropped atomic.Uint64
	flushed atomic.Uint64
	lost    atomic.Uint64

	// mu makes the closed check and the buffer send atomic against shutdown:
	// Add holds the read side (across the scope hook too, which is a trivial
	// context read by contract), Run's shutdown takes the write side to flip
	// closed, so once drain starts no accepted event can still be in flight
	// into the buffer. A bare WaitGroup would race Add(1) against Wait on a
	// zero counter (documented misuse) for Adds arriving after shutdown.
	mu     sync.RWMutex
	closed bool
}

// New builds a Collector delivering into sink. The default configuration is
// DefaultConfig; returns ErrNilSink on a nil sink and ErrInvalidConfig
// (matchable via errors.Is) on bad knobs.
func New[T any](sink Sink[T], opts ...Option) (*Collector[T], error) {
	if sink == nil {
		return nil, ErrNilSink
	}
	s := settings{cfg: DefaultConfig(), name: "collector", log: logger.NewNope()}
	for _, opt := range opts {
		opt(&s)
	}
	if err := s.cfg.Validate(); err != nil {
		return nil, err
	}
	return &Collector[T]{sink: sink, buf: make(chan entry[T], s.cfg.BufferSize), settings: s}, nil
}

// Name returns the supervisor service name.
func (c *Collector[T]) Name() string { return c.name }

// Add buffers one event and returns immediately — it never blocks and never
// performs I/O. On a full buffer the event is dropped (drop-newest), counted,
// and ErrBufferFull returned; fire-and-forget callers may ignore the error.
// Returns ErrScopeMissing when a configured scope hook errors or yields an
// empty scope, and ErrClosed once shutdown has begun; ErrClosed takes
// precedence, so shutdown never pays for scope hook calls.
func (c *Collector[T]) Add(ctx context.Context, event T) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return ErrClosed
	}
	var scope string
	if c.scope != nil {
		s, err := c.scope(ctx)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return ErrScopeMissing
		}
		scope = s
	}
	select {
	case c.buf <- entry[T]{event: event, scope: scope}:
		c.added.Add(1)
		return nil
	default:
		c.dropped.Add(1)
		return ErrBufferFull
	}
}

// Stats returns a snapshot of the event counters.
func (c *Collector[T]) Stats() Stats {
	return Stats{Added: c.added.Load(), Dropped: c.dropped.Load(), Flushed: c.flushed.Load(), Lost: c.lost.Load()}
}

// Run is the flusher loop: it accumulates buffered events and flushes when a
// batch reaches Config.BatchSize or Config.FlushInterval elapses, whichever
// comes first. On ctx cancellation it stops accepting (Add returns ErrClosed),
// drains everything already buffered through the sink, and returns nil: after
// Run returns, every accepted event is accounted for — Stats.Added equals
// Stats.Flushed plus Stats.Lost.
func (c *Collector[T]) Run(ctx context.Context) error {
	c.log.InfoContext(ctx, "collector started", slog.String("service", c.name), slog.Int("buffer", c.cfg.BufferSize), slog.Int("batch", c.cfg.BatchSize), slog.Duration("interval", c.cfg.FlushInterval))
	batch := make([]entry[T], 0, c.cfg.BatchSize)
	ticker := time.NewTicker(c.cfg.FlushInterval)
	defer ticker.Stop()
	var reported uint64
	for {
		select {
		case e := <-c.buf:
			batch = append(batch, e)
			if len(batch) >= c.cfg.BatchSize {
				c.flush(ctx, batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			// The tick fires every interval even under sustained size-triggered
			// flushing, so an event waits at most one interval and drop deltas
			// keep getting reported under exactly the load that causes drops.
			if len(batch) > 0 {
				c.flush(ctx, batch)
				batch = batch[:0]
			}
			reported = c.reportDrops(ctx, reported)
		case <-ctx.Done():
			c.mu.Lock()
			c.closed = true
			c.mu.Unlock()
			c.log.InfoContext(ctx, "collector draining", slog.String("service", c.name))
			c.drain(ctx, batch)
			c.reportDrops(ctx, reported)
			c.log.InfoContext(ctx, "collector stopped", slog.String("service", c.name))
			return nil
		}
	}
}

// drain empties the buffer after shutdown began and flushes everything,
// including the partial batch carried in from the main loop.
func (c *Collector[T]) drain(ctx context.Context, batch []entry[T]) {
	for {
		select {
		case e := <-c.buf:
			batch = append(batch, e)
			if len(batch) >= c.cfg.BatchSize {
				c.flush(ctx, batch)
				batch = batch[:0]
			}
		default:
			if len(batch) > 0 {
				c.flush(ctx, batch)
			}
			return
		}
	}
}

// flush partitions the batch by captured scope (preserving first-seen order
// and per-scope event order) and delivers one Flush call per scope. Without a
// scope hook every event carries the empty scope and the batch flushes whole.
func (c *Collector[T]) flush(ctx context.Context, batch []entry[T]) {
	if c.scope == nil {
		events := make([]T, len(batch))
		for i, e := range batch {
			events[i] = e.event
		}
		c.deliver(ctx, "", events)
		return
	}
	grouped := make(map[string][]T)
	order := make([]string, 0, 1)
	for _, e := range batch {
		if _, ok := grouped[e.scope]; !ok {
			order = append(order, e.scope)
		}
		grouped[e.scope] = append(grouped[e.scope], e.event)
	}
	for _, scope := range order {
		c.deliver(ctx, scope, grouped[scope])
	}
}

// deliver flushes one scoped batch. The sink context survives Run's
// cancellation so shutdown drain still delivers, is bounded by
// Config.FlushTimeout, and passes through the scope restore hook when one is
// configured.
func (c *Collector[T]) deliver(ctx context.Context, scope string, events []T) {
	fctx := context.WithoutCancel(ctx)
	if c.scopeCtx != nil {
		fctx = c.scopeCtx(fctx, scope)
	}
	if c.cfg.FlushTimeout > 0 {
		var cancel context.CancelFunc
		fctx, cancel = context.WithTimeout(fctx, c.cfg.FlushTimeout)
		defer cancel()
	}
	if err := c.sink.Flush(fctx, events); err != nil {
		c.lost.Add(uint64(len(events)))
		c.log.ErrorContext(ctx, "collector flush failed, batch lost", slog.String("service", c.name), slog.String("scope", scope), slog.Int("events", len(events)), slog.Any("error", err))
		return
	}
	c.flushed.Add(uint64(len(events)))
}

// reportDrops logs the drop delta since the last report and returns the new
// watermark, keeping loss visible without per-event log spam.
func (c *Collector[T]) reportDrops(ctx context.Context, reported uint64) uint64 {
	d := c.dropped.Load()
	if d > reported {
		c.log.WarnContext(ctx, "collector dropped events, buffer full", slog.String("service", c.name), slog.Uint64("dropped", d-reported), slog.Uint64("dropped_total", d))
	}
	return d
}
