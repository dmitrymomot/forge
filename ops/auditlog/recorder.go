package auditlog

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Recorder stamps and appends audit events to a Sink. Safe for concurrent
// use. Without WithChain, Record is lock-free apart from the monotonic ID
// generator; with it, writes serialize per stream (see WithChain).
type Recorder struct {
	sink  Sink
	gen   *id.Generator
	heads map[string]*chainHead
	cfg   config
	mu    sync.Mutex // guards heads
}

// chainHead is the running chain state of one stream. Its mutex serializes
// appends to the stream: hash(n) depends on hash(n-1), so two concurrent
// writers cannot both extend the same head. It is intentionally held
// across the Sink write — releasing earlier would let a second event chain
// onto a hash that may never persist.
type chainHead struct {
	hash   string
	mu     sync.Mutex
	seeded bool
}

// New builds a Recorder over sink. It panics on a nil sink — a wiring bug
// caught at startup.
func New(sink Sink, opts ...Option) *Recorder {
	if sink == nil {
		panic("auditlog: nil sink")
	}
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return &Recorder{
		sink:  sink,
		gen:   id.NewGenerator(id.WithClock(cfg.clock), id.WithMonotonic()),
		cfg:   cfg,
		heads: map[string]*chainHead{},
	}
}

// Record validates, stamps, and writes e, returning the finalized event
// (assigned ID, defaulted Time, resolved Tenant, chain hashes when
// enabled). An empty Action or Outcome fails with ErrInvalidEvent. Under
// WithScope the hook's tenant is stamped onto the event; a hook error or
// empty tenant fails closed with ErrScope, and an explicit e.Tenant that
// disagrees with the hook fails with ErrTenantMismatch. A Sink error is
// returned as-is and, under WithChain, leaves the stream head unchanged so
// the chain never references an event that was not persisted.
func (r *Recorder) Record(ctx context.Context, e Event) (Event, error) {
	if e.Action == "" || e.Outcome == "" {
		return Event{}, fmt.Errorf("%w: action and outcome are required", ErrInvalidEvent)
	}
	if r.cfg.scope != nil {
		tenant, err := r.cfg.scope(ctx)
		if err != nil {
			return Event{}, fmt.Errorf("%w: %w", ErrScope, err)
		}
		if tenant == "" {
			return Event{}, ErrScope
		}
		if e.Tenant != "" && e.Tenant != tenant {
			return Event{}, ErrTenantMismatch
		}
		e.Tenant = tenant
	}
	// After the scope hook, so a hook-derived tenant is validated too.
	if err := checkNUL(e); err != nil {
		return Event{}, err
	}
	if e.Time.IsZero() {
		e.Time = r.cfg.clock.Now()
	}
	// Microsecond precision survives a Postgres timestamptz round-trip, so
	// chain hashes recompute identically after a read-back.
	e.Time = e.Time.UTC().Truncate(time.Microsecond)

	if !r.cfg.chain {
		e.ID = r.gen.UUID()
		e.PrevHash, e.Hash = "", ""
		if err := r.sink.Write(ctx, e); err != nil {
			return Event{}, err
		}
		return e, nil
	}
	return r.recordChained(ctx, e)
}

func (r *Recorder) recordChained(ctx context.Context, e Event) (Event, error) {
	head := r.head(e.Tenant)
	head.mu.Lock()
	defer head.mu.Unlock()
	if !head.seeded {
		if ch, ok := r.sink.(ChainHead); ok {
			hash, err := ch.ChainHead(ctx, e.Tenant)
			if err != nil {
				return Event{}, fmt.Errorf("auditlog: seed chain head: %w", err)
			}
			head.hash = hash
		}
		head.seeded = true
	}
	// The ID is generated under the stream lock: the generator is
	// monotonic, so id order matches chain order and a verify pass can
	// walk the stream by id ascending.
	e.ID = r.gen.UUID()
	e.PrevHash = head.hash
	e.Hash = ComputeHash(e)
	if err := r.sink.Write(ctx, e); err != nil {
		// The write is ambiguous — it may have persisted despite the error
		// (e.g. context canceled after the insert committed). Re-seed from
		// the sink on the next Record so the chain continues from whatever
		// head actually persisted instead of forking.
		head.seeded = false
		return Event{}, err
	}
	head.hash = e.Hash
	return e, nil
}

// checkNUL rejects events carrying NUL bytes: Postgres text and jsonb
// cannot store them, and silently stripping them after hashing would make
// the persisted event fail verification. Rejecting up front keeps the
// failure at the audit point instead of an opaque driver error —
// consumers must sanitize untrusted input before putting it in an event.
func checkNUL(e Event) error {
	fields := [...]string{e.Tenant, e.Actor, e.Action, e.Resource, string(e.Outcome)}
	for _, f := range fields {
		if strings.ContainsRune(f, 0) {
			return fmt.Errorf("%w: NUL byte in field", ErrInvalidEvent)
		}
	}
	for k, v := range e.Meta {
		if strings.ContainsRune(k, 0) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("%w: NUL byte in meta", ErrInvalidEvent)
		}
	}
	return nil
}

// head returns the chain state for stream, creating it on first use. The
// map is bounded by the number of distinct tenants.
func (r *Recorder) head(stream string) *chainHead {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.heads[stream]
	if !ok {
		h = &chainHead{}
		r.heads[stream] = h
	}
	return h
}
