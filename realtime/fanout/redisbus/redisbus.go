package redisbus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// receiveRetryDelay paces the receive loop after a transient error so a
// down Redis does not spin the CPU; go-redis handles the actual reconnect.
const receiveRetryDelay = 250 * time.Millisecond

// Bus is the Redis Pub/Sub backplane. Construct with New; it implements
// fanout.Bus, and its Run loop (a supervisor.Service) is the receive path.
type Bus struct {
	client  goredis.UniversalClient
	log     *slog.Logger
	handler atomic.Pointer[func(topic string, payload []byte)]
	channel string
}

// New builds a Bus over the caller's client. The client stays owned by the
// caller — *goredis.Client, *goredis.ClusterClient, and *goredis.Ring all
// satisfy goredis.UniversalClient.
func New(client goredis.UniversalClient, opts ...Option) (*Bus, error) {
	if client == nil {
		return nil, fmt.Errorf("redisbus: nil client")
	}
	c := defaultConfig()
	for _, opt := range opts {
		opt(c)
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	return &Bus{
		client:  client,
		log:     c.logger,
		channel: c.channel,
	}, nil
}

// Publish broadcasts one message to every subscribed instance — including
// this one. Topics containing NUL are rejected (the frame separator).
func (b *Bus) Publish(ctx context.Context, topic string, payload []byte) error {
	if strings.IndexByte(topic, 0) >= 0 {
		return fmt.Errorf("%w: %q contains NUL", ErrInvalidTopic, topic)
	}
	frame := make([]byte, 0, len(topic)+1+len(payload))
	frame = append(frame, topic...)
	frame = append(frame, 0)
	frame = append(frame, payload...)
	if err := b.client.Publish(ctx, b.channel, frame).Err(); err != nil {
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
func (b *Bus) Name() string { return "fanout.redisbus:" + b.channel }

// Run is the receive loop: it subscribes to the bus channel and dispatches
// every message to the subscribed callback until ctx is cancelled. go-redis
// reconnects and resubscribes on its own; messages published while the
// connection is down are lost (at-most-once).
func (b *Bus) Run(ctx context.Context) error {
	pubsub := b.client.Subscribe(ctx, b.channel)
	defer func() { _ = pubsub.Close() }()
	// ReceiveMessage blocks on the socket without observing ctx; closing the
	// PubSub is what unblocks it on shutdown.
	stop := context.AfterFunc(ctx, func() { _ = pubsub.Close() })
	defer stop()
	for {
		msg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.WarnContext(ctx, "redisbus receive failed, retrying",
				slog.String("channel", b.channel),
				slog.Any("error", err))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(receiveRetryDelay):
			}
			continue
		}
		b.deliver(ctx, msg.Payload)
	}
}

// deliver splits one frame and hands it to the callback. Foreign payloads on
// the channel (no NUL separator) are logged and skipped.
func (b *Bus) deliver(ctx context.Context, frame string) {
	fn := b.handler.Load()
	if fn == nil {
		return
	}
	topic, payload, ok := strings.Cut(frame, "\x00")
	if !ok {
		b.log.WarnContext(ctx, "redisbus dropped unframed message",
			slog.String("channel", b.channel))
		return
	}
	(*fn)(topic, []byte(payload))
}
