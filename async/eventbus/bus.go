package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// Bus is the event registry and publisher. Wire it once at startup: declare
// every subscription with Subscribe, then share the Bus app-wide — it is
// cheap and safe for concurrent publishing. Subscribe is not safe to call
// concurrently with Publish or a running Service.
//
// A durable Bus (New) fans every published event out as one queue.Job per
// subscription; a sync Bus (NewSync) invokes handlers in-process during
// Publish. Publisher-only processes construct the same wiring but simply do
// not run the Service.
type Bus struct {
	broker queue.Broker // nil on a sync bus
	scope  func(ctx context.Context) (string, error)
	clk    clock.Clock
	subs   map[string][]*subscription // event name → subscriptions, in Subscribe order
	names  map[string]struct{}        // subscription names, duplicate guard
	types  map[string]any             // event name → (*T)(nil) marker, payload-type-drift guard
}

// subscription is one named binding of an event to a handler. Its name is
// both the queue and the job type of the fanned-out jobs.
type subscription struct {
	name   string
	handle func(ctx context.Context, env envelope) error
	hopts  []queue.HandlerOption
}

// New builds a durable Bus over broker: Publish fans out one job per
// subscription and handlers run in a worker Service with the queue engine's
// retry, backoff, and dead-letter semantics. Panics on a nil broker — the
// bus is startup wiring, not runtime data.
func New(broker queue.Broker, opts ...Option) *Bus {
	if broker == nil {
		panic("eventbus: New requires a broker (use NewSync for the in-process bus)")
	}
	return newBus(broker, opts)
}

// NewSync builds a sync in-process Bus: Publish invokes every subscription
// handler synchronously on the caller's goroutine and joins their errors. No
// durability, no retries — a crash between handlers loses the event. Use it
// for tests, dev, and single-process apps that can afford loss.
func NewSync(opts ...Option) *Bus {
	return newBus(nil, opts)
}

func newBus(broker queue.Broker, opts []Option) *Bus {
	b := &Bus{
		broker: broker,
		clk:    clock.System(),
		subs:   make(map[string][]*subscription),
		names:  make(map[string]struct{}),
		types:  make(map[string]any),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Subscribe declares a named subscription of evt: fn runs once per published
// event. The full subscription name — "<event>.<name>" — is the durable
// queue (and job type) the event fans out to, so it must be unique across
// the app; competing worker instances drain it together. Panics on an empty
// name, a nil fn, or a duplicate (event, name) pair: subscriptions are
// startup wiring, and failing fast beats silently double-routing.
//
// HANDLERS MUST BE IDEMPOTENT: durable delivery is at-least-once, and a
// lease lost mid-handler redelivers the event. Dedup side effects with the
// Inbox keyed by Delivery.ID.
func Subscribe[T any](bus *Bus, evt Event[T], name string, fn func(ctx context.Context, d Delivery[T]) error, opts ...SubscribeOption) {
	if name == "" {
		panic(fmt.Sprintf("eventbus: Subscribe(%q) requires a non-empty subscription name", evt.Name()))
	}
	if fn == nil {
		panic(fmt.Sprintf("eventbus: Subscribe(%q, %q) with nil handler", evt.Name(), name))
	}
	full := evt.Name() + "." + name
	if _, dup := bus.names[full]; dup {
		panic(fmt.Sprintf("eventbus: duplicate subscription %q", full))
	}
	// Two independent NewEvent declarations sharing a name but not a payload
	// type would otherwise cross-deliver: lenient JSON decoding silently zeroes
	// the mismatched fields instead of failing.
	if tok, ok := bus.types[evt.Name()]; ok {
		if _, same := tok.(*T); !same {
			panic(fmt.Sprintf("eventbus: event %q subscribed with conflicting payload types (%T vs *%T)", evt.Name(), tok, *new(T)))
		}
	} else {
		bus.types[evt.Name()] = (*T)(nil)
	}
	sub := &subscription{
		name: full,
		handle: func(ctx context.Context, env envelope) error {
			if env.V != wireVersion {
				return queue.SkipRetry(fmt.Errorf("eventbus: subscription %q: unsupported envelope version %d", full, env.V))
			}
			// A foreign producer omitting the event id would poison every
			// inbox-using handler; that is malformed input, not a transient
			// failure, so dead-letter instead of burning the retry budget.
			if env.ID == "" {
				return queue.SkipRetry(fmt.Errorf("eventbus: subscription %q: envelope without event id", full))
			}
			var p T
			if len(env.Payload) > 0 {
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					return queue.SkipRetry(fmt.Errorf("eventbus: unmarshal payload for %q: %w", full, err))
				}
			}
			return fn(ctx, Delivery[T]{
				OccurredAt: time.UnixMilli(env.AtMS).UTC(),
				ID:         env.ID,
				Name:       env.Name,
				Payload:    p,
			})
		},
	}
	for _, opt := range opts {
		opt(sub)
	}
	bus.names[full] = struct{}{}
	bus.subs[evt.Name()] = append(bus.subs[evt.Name()], sub)
}

// Publish emits an event to every subscription of evt. On a durable bus this
// pushes one job per subscription in a single all-or-nothing broker batch; on
// a sync bus it runs every handler in-process and returns their joined errors
// (a failing handler does not stop the others; queue.Cancel counts as
// success). An event with no subscriptions delivers nothing — publishers
// never couple to subscriber existence — though a configured scope hook still
// fails closed and an unmarshalable payload still errors.
func Publish[T any](ctx context.Context, bus *Bus, evt Event[T], payload T) error {
	if err := checkType[T](bus, evt.Name()); err != nil {
		return err
	}
	env, scope, err := bus.prepare(ctx, evt.Name(), payload)
	if err != nil {
		return err
	}
	subs := bus.subs[evt.Name()]
	if len(subs) == 0 {
		return nil
	}
	if bus.broker == nil {
		return bus.dispatchSync(ctx, env, subs)
	}
	jobs, err := bus.fanout(env, subs, scope)
	if err != nil {
		return err
	}
	return bus.broker.Push(ctx, jobs...)
}

// PublishTx emits an event inside a caller-owned database transaction: the
// fan-out commits or rolls back with the caller's writes. tx is
// driver-specific, exactly as in queue.PushTx (pgqueue asserts pgx.Tx).
// Requires a durable bus whose broker implements queue.TxPusher; otherwise
// ErrTxUnsupported.
func PublishTx[T any](ctx context.Context, bus *Bus, tx any, evt Event[T], payload T) error {
	if bus.broker == nil {
		return fmt.Errorf("%w: sync bus", ErrTxUnsupported)
	}
	tp, ok := bus.broker.(queue.TxPusher)
	if !ok {
		return fmt.Errorf("%w: broker does not implement queue.TxPusher", ErrTxUnsupported)
	}
	if err := checkType[T](bus, evt.Name()); err != nil {
		return err
	}
	env, scope, err := bus.prepare(ctx, evt.Name(), payload)
	if err != nil {
		return err
	}
	subs := bus.subs[evt.Name()]
	if len(subs) == 0 {
		return nil
	}
	jobs, err := bus.fanout(env, subs, scope)
	if err != nil {
		return err
	}
	return tp.PushTx(ctx, tx, jobs...)
}

// checkType rejects a publish whose payload type differs from the one the
// event's subscribers declared — reachable only through duplicate NewEvent
// declarations sharing a name, where lenient JSON decoding would otherwise
// deliver silently zeroed fields.
func checkType[T any](bus *Bus, name string) error {
	if tok, ok := bus.types[name]; ok {
		if _, same := tok.(*T); !same {
			return fmt.Errorf("eventbus: publish %q: payload type *%T conflicts with subscribed type %T", name, *new(T), tok)
		}
	}
	return nil
}

// prepare marshals the payload, resolves the tenancy scope (fail-closed on
// both bus modes, so a sync dev setup catches missing tenant context exactly
// like the durable production one), and stamps the envelope.
func (b *Bus) prepare(ctx context.Context, name string, payload any) (envelope, string, error) {
	scope := ""
	if b.scope != nil {
		s, err := b.scope(ctx)
		if err != nil {
			return envelope{}, "", fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return envelope{}, "", ErrScopeMissing
		}
		scope = s
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return envelope{}, "", fmt.Errorf("eventbus: marshal payload for %q: %w", name, err)
	}
	return envelope{
		V:       wireVersion,
		ID:      id.NewUUID().String(),
		Name:    name,
		AtMS:    b.clk.Now().UnixMilli(),
		Payload: raw,
	}, scope, nil
}

// fanout builds one job per subscription, all carrying the same encoded
// envelope: same event id everywhere, distinct job ids per queue.
func (b *Bus) fanout(env envelope, subs []*subscription, scope string) ([]queue.Job, error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("eventbus: encode envelope for %q: %w", env.Name, err)
	}
	now := time.UnixMilli(env.AtMS).UTC()
	jobs := make([]queue.Job, 0, len(subs))
	for _, s := range subs {
		jobs = append(jobs, queue.Job{
			ID:        id.NewUUID().String(),
			Queue:     s.name,
			Type:      s.name,
			Payload:   raw,
			Scope:     scope,
			RunAt:     now,
			CreatedAt: now,
		})
	}
	return jobs, nil
}

// dispatchSync runs every handler on the caller's goroutine, joining their
// errors so one failing observer never starves the rest. A cancelled context
// stops the walk: remaining handlers are skipped and ctx.Err() joins the
// result. queue.Cancel keeps its durable-mode meaning — the handler discarded
// a moot event, which is success — so mode-agnostic handlers behave
// identically on both buses.
func (b *Bus) dispatchSync(ctx context.Context, env envelope, subs []*subscription) error {
	var errs []error
	for _, s := range subs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := invokeSync(ctx, s, env); err != nil && !errors.Is(err, queue.Cancel) {
			errs = append(errs, fmt.Errorf("eventbus: subscription %q: %w", s.name, err))
		}
	}
	return errors.Join(errs...)
}

// invokeSync runs one handler with panic recovery: a panicking observer is a
// failed observer, not a crashed publisher — parity with the queue engine's
// handler recovery in durable mode.
func invokeSync(ctx context.Context, s *subscription, env envelope) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return s.handle(ctx, env)
}
