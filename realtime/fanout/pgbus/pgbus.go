package pgbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxNotifyPayload is the Postgres NOTIFY payload cap (8000 bytes) applied to
// the whole encoded envelope.
const maxNotifyPayload = 8000

const (
	initialBackoff = 200 * time.Millisecond
	maxBackoff     = 10 * time.Second
)

// envelope frames one message inside a NOTIFY payload. encoding/json encodes
// P as base64, keeping arbitrary binary payloads text-safe.
type envelope struct {
	T string `json:"t"`
	P []byte `json:"p,omitempty"`
}

// Bus is the LISTEN/NOTIFY backplane. Construct with New; it implements
// fanout.Bus, and its Run loop (a supervisor.Service) is the receive path.
type Bus struct {
	pool    *pgxpool.Pool
	log     *slog.Logger
	handler atomic.Pointer[func(topic string, payload []byte)]
	channel string
	quoted  string
}

// New builds a Bus over the caller's pool. The pool stays owned by the
// caller; the bus publishes through it and dedicates one acquired connection
// to LISTEN while Run is live.
func New(pool *pgxpool.Pool, opts ...Option) (*Bus, error) {
	if pool == nil {
		return nil, fmt.Errorf("pgbus: nil pool")
	}
	c := defaultConfig()
	for _, opt := range opts {
		opt(c)
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	return &Bus{
		pool:    pool,
		log:     c.logger,
		channel: c.channel,
		quoted:  pgx.Identifier{c.channel}.Sanitize(),
	}, nil
}

// Publish broadcasts one message to every listening instance — including
// this one — via pg_notify. It fails with ErrPayloadTooLarge when the
// envelope exceeds the NOTIFY cap.
func (b *Bus) Publish(ctx context.Context, topic string, payload []byte) error {
	env, err := json.Marshal(envelope{T: topic, P: payload})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPublish, err)
	}
	if len(env) > maxNotifyPayload {
		return fmt.Errorf("%w: envelope is %d bytes, limit %d", ErrPayloadTooLarge, len(env), maxNotifyPayload)
	}
	if _, err := b.pool.Exec(ctx, "SELECT pg_notify($1, $2)", b.channel, string(env)); err != nil {
		return fmt.Errorf("%w: %w", ErrPublish, err)
	}
	return nil
}

// Subscribe registers the delivery callback, replacing any previous one. The
// attached hub calls this from fanout.New; messages received while no
// callback is registered are discarded.
func (b *Bus) Subscribe(fn func(topic string, payload []byte)) {
	if fn == nil {
		b.handler.Store(nil)
		return
	}
	b.handler.Store(&fn)
}

// Name identifies the service in supervisor logs.
func (b *Bus) Name() string { return "fanout.pgbus:" + b.channel }

// Run is the receive loop: it dedicates a pooled connection to LISTEN and
// dispatches every notification to the subscribed callback, reconnecting
// with exponential backoff until ctx is cancelled. Messages published while
// the listener is down are lost (at-most-once).
func (b *Bus) Run(ctx context.Context) error {
	backoff := initialBackoff
	for {
		connected, err := b.listen(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if connected {
			backoff = initialBackoff
		}
		b.log.WarnContext(ctx, "pgbus listener disconnected, reconnecting",
			slog.String("channel", b.channel),
			slog.Duration("backoff", backoff),
			slog.Any("error", err))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// listen holds one LISTEN connection until it fails or ctx ends. connected
// reports whether LISTEN was established, resetting the caller's backoff.
// The connection is closed on exit — never released back into the pool with
// LISTEN state attached.
func (b *Bus) listen(ctx context.Context) (connected bool, err error) {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		// Closing the underlying connection makes Release destroy it.
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
		conn.Release()
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+b.quoted); err != nil {
		return false, err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return true, err
		}
		if n != nil {
			b.deliver(ctx, n.Payload)
		}
	}
}

// deliver decodes one NOTIFY payload and hands it to the callback. Foreign
// or corrupt payloads on the channel are logged and skipped.
func (b *Bus) deliver(ctx context.Context, payload string) {
	fn := b.handler.Load()
	if fn == nil {
		return
	}
	var env envelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		b.log.WarnContext(ctx, "pgbus dropped undecodable notification",
			slog.String("channel", b.channel),
			slog.Any("error", err))
		return
	}
	(*fn)(env.T, env.P)
}
