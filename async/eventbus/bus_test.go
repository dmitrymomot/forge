package eventbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
)

type userCreated struct {
	Email string `json:"email"`
}

func TestNewEvent(t *testing.T) {
	t.Parallel()

	t.Run("name", func(t *testing.T) {
		t.Parallel()
		evt := eventbus.NewEvent[userCreated]("user.created")
		assert.Equal(t, "user.created", evt.Name())
	})

	t.Run("panics on empty name", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { eventbus.NewEvent[userCreated]("") })
	})
}

func TestNew_PanicsOnNilBroker(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { eventbus.New(nil) })
}

func TestSubscribe_WiringPanics(t *testing.T) {
	t.Parallel()
	evt := eventbus.NewEvent[userCreated]("user.created")
	handler := func(context.Context, eventbus.Delivery[userCreated]) error { return nil }

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		assert.Panics(t, func() { eventbus.Subscribe(bus, evt, "", handler) })
	})

	t.Run("nil handler", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		assert.Panics(t, func() { eventbus.Subscribe(bus, evt, "send_welcome", nil) })
	})

	t.Run("duplicate subscription", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		assert.Panics(t, func() { eventbus.Subscribe(bus, evt, "send_welcome", handler) })
	})

	t.Run("same name on different events is fine", func(t *testing.T) {
		t.Parallel()
		other := eventbus.NewEvent[userCreated]("user.deleted")
		bus := eventbus.NewSync()
		eventbus.Subscribe(bus, evt, "cleanup", handler)
		assert.NotPanics(t, func() { eventbus.Subscribe(bus, other, "cleanup", handler) })
	})

	t.Run("conflicting payload types for one event name", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		eventbus.Subscribe(bus, evt, "typed", handler)
		clash := eventbus.NewEvent[struct {
			Other int `json:"other"`
		}]("user.created")
		assert.Panics(t, func() {
			eventbus.Subscribe(bus, clash, "clashing", func(context.Context, eventbus.Delivery[struct {
				Other int `json:"other"`
			}]) error {
				return nil
			})
		})
	})
}

func TestPublish_PayloadTypeConflict(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[userCreated]("user.created")
	eventbus.Subscribe(bus, evt, "typed", func(context.Context, eventbus.Delivery[userCreated]) error { return nil })

	clash := eventbus.NewEvent[struct {
		Other int `json:"other"`
	}]("user.created")
	err := eventbus.Publish(context.Background(), bus, clash, struct {
		Other int `json:"other"`
	}{Other: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload type")
}

func TestSyncPublish(t *testing.T) {
	t.Parallel()
	evt := eventbus.NewEvent[userCreated]("user.created")

	t.Run("delivers to every subscription with shared metadata", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
		bus := eventbus.NewSync(eventbus.WithClock(clock.NewMock(now)))
		var first, second eventbus.Delivery[userCreated]
		eventbus.Subscribe(bus, evt, "first", func(_ context.Context, d eventbus.Delivery[userCreated]) error {
			first = d
			return nil
		})
		eventbus.Subscribe(bus, evt, "second", func(_ context.Context, d eventbus.Delivery[userCreated]) error {
			second = d
			return nil
		})

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{Email: "a@b.c"}))

		assert.Equal(t, "a@b.c", first.Payload.Email)
		assert.Equal(t, "a@b.c", second.Payload.Email)
		assert.Equal(t, "user.created", first.Name)
		assert.NotEmpty(t, first.ID)
		assert.Equal(t, first.ID, second.ID, "one publish = one event id across subscriptions")
		assert.Equal(t, now, first.OccurredAt)
	})

	t.Run("no subscriptions is a no-op", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		assert.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))
	})

	t.Run("unmarshalable payload errors", func(t *testing.T) {
		t.Parallel()
		poison := eventbus.NewEvent[chan int]("bad.payload")
		bus := eventbus.NewSync()
		err := eventbus.Publish(context.Background(), bus, poison, make(chan int))
		assert.Error(t, err)
	})

	t.Run("joins handler errors and keeps going", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		sentinel := errors.New("boom")
		ran := 0
		eventbus.Subscribe(bus, evt, "failing", func(context.Context, eventbus.Delivery[userCreated]) error {
			ran++
			return sentinel
		})
		eventbus.Subscribe(bus, evt, "healthy", func(context.Context, eventbus.Delivery[userCreated]) error {
			ran++
			return nil
		})

		err := eventbus.Publish(context.Background(), bus, evt, userCreated{})
		require.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), `"user.created.failing"`)
		assert.Equal(t, 2, ran, "a failing observer must not starve the rest")
	})

	t.Run("queue.Cancel counts as success", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		eventbus.Subscribe(bus, evt, "moot", func(context.Context, eventbus.Delivery[userCreated]) error {
			return queue.Cancel
		})
		assert.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))
	})

	t.Run("recovers handler panic", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		eventbus.Subscribe(bus, evt, "panicky", func(context.Context, eventbus.Delivery[userCreated]) error {
			panic("kaboom")
		})
		err := eventbus.Publish(context.Background(), bus, evt, userCreated{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "handler panic")
	})

	t.Run("cancelled context stops the walk", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		ran := false
		eventbus.Subscribe(bus, evt, "never", func(context.Context, eventbus.Delivery[userCreated]) error {
			ran = true
			return nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := eventbus.Publish(ctx, bus, evt, userCreated{})
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, ran)
	})
}

// decodeEnvelope pulls the shared event metadata out of a fanned-out job's
// payload without depending on the unexported wire type.
func decodeEnvelope(t *testing.T, payload []byte) (id string, name string, raw json.RawMessage) {
	t.Helper()
	var env struct {
		ID      string          `json:"id"`
		Name    string          `json:"n"`
		Payload json.RawMessage `json:"p"`
		V       int             `json:"v"`
	}
	require.NoError(t, json.Unmarshal(payload, &env))
	require.Equal(t, 1, env.V)
	return env.ID, env.Name, env.Payload
}

func claimAll(t *testing.T, broker queue.Broker, q string) []queue.ClaimedJob {
	t.Helper()
	jobs, err := broker.Claim(context.Background(), q, 10, time.Minute)
	require.NoError(t, err)
	return jobs
}

func TestDurablePublish(t *testing.T) {
	t.Parallel()
	evt := eventbus.NewEvent[userCreated]("user.created")

	t.Run("fans out one job per subscription", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)
		handler := func(context.Context, eventbus.Delivery[userCreated]) error { return nil }
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		eventbus.Subscribe(bus, evt, "provision", handler)

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{Email: "a@b.c"}))

		welcome := claimAll(t, broker, "user.created.send_welcome")
		provision := claimAll(t, broker, "user.created.provision")
		require.Len(t, welcome, 1)
		require.Len(t, provision, 1)

		assert.Equal(t, "user.created.send_welcome", welcome[0].Type)
		assert.Equal(t, "user.created.provision", provision[0].Type)
		assert.NotEqual(t, welcome[0].ID, provision[0].ID, "distinct job ids per subscription")

		wID, wName, wRaw := decodeEnvelope(t, welcome[0].Payload)
		pID, _, _ := decodeEnvelope(t, provision[0].Payload)
		assert.Equal(t, wID, pID, "same event id across subscriptions")
		assert.Equal(t, "user.created", wName)
		var p userCreated
		require.NoError(t, json.Unmarshal(wRaw, &p))
		assert.Equal(t, "a@b.c", p.Email)
	})

	t.Run("no subscriptions pushes nothing", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)
		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))
		st, err := broker.Stats(context.Background())
		require.NoError(t, err)
		assert.Empty(t, st)
	})

	t.Run("other events' subscriptions are not fanned out", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker)
		other := eventbus.NewEvent[userCreated]("user.deleted")
		handler := func(context.Context, eventbus.Delivery[userCreated]) error { return nil }
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		eventbus.Subscribe(bus, other, "cleanup", handler)

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))

		assert.Len(t, claimAll(t, broker, "user.created.send_welcome"), 1)
		assert.Empty(t, claimAll(t, broker, "user.deleted.cleanup"))
	})
}

func TestScope(t *testing.T) {
	t.Parallel()
	evt := eventbus.NewEvent[userCreated]("user.created")
	handler := func(context.Context, eventbus.Delivery[userCreated]) error { return nil }

	t.Run("captured into fanned-out jobs", func(t *testing.T) {
		t.Parallel()
		broker := queue.NewMemoryBroker()
		bus := eventbus.New(broker, eventbus.WithScope(func(context.Context) (string, error) {
			return "tenant-1", nil
		}))
		eventbus.Subscribe(bus, evt, "send_welcome", handler)

		require.NoError(t, eventbus.Publish(context.Background(), bus, evt, userCreated{}))
		jobs := claimAll(t, broker, "user.created.send_welcome")
		require.Len(t, jobs, 1)
		assert.Equal(t, "tenant-1", jobs[0].Scope)
	})

	t.Run("fail closed on empty scope", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.New(queue.NewMemoryBroker(), eventbus.WithScope(func(context.Context) (string, error) {
			return "", nil
		}))
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		err := eventbus.Publish(context.Background(), bus, evt, userCreated{})
		assert.ErrorIs(t, err, eventbus.ErrScopeMissing)
	})

	t.Run("fail closed on hook error", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("no tenant in ctx")
		bus := eventbus.New(queue.NewMemoryBroker(), eventbus.WithScope(func(context.Context) (string, error) {
			return "", cause
		}))
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		err := eventbus.Publish(context.Background(), bus, evt, userCreated{})
		assert.ErrorIs(t, err, eventbus.ErrScopeMissing)
		assert.ErrorIs(t, err, cause)
	})

	t.Run("sync bus enforces the hook too", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync(eventbus.WithScope(func(context.Context) (string, error) {
			return "", nil
		}))
		ran := false
		eventbus.Subscribe(bus, evt, "send_welcome", func(context.Context, eventbus.Delivery[userCreated]) error {
			ran = true
			return nil
		})
		err := eventbus.Publish(context.Background(), bus, evt, userCreated{})
		assert.ErrorIs(t, err, eventbus.ErrScopeMissing)
		assert.False(t, ran, "fail-closed publish must not deliver")
	})
}

// txRecorder wraps the memory broker with a queue.TxPusher that records the
// tx it was handed and the jobs pushed through it.
type txRecorder struct {
	*queue.MemoryBroker
	tx   any
	jobs []queue.Job
}

func (r *txRecorder) PushTx(_ context.Context, tx any, jobs ...queue.Job) error {
	r.tx = tx
	r.jobs = append(r.jobs, jobs...)
	return nil
}

func TestPublishTx(t *testing.T) {
	t.Parallel()
	evt := eventbus.NewEvent[userCreated]("user.created")
	handler := func(context.Context, eventbus.Delivery[userCreated]) error { return nil }

	t.Run("routes through TxPusher with the caller's tx", func(t *testing.T) {
		t.Parallel()
		rec := &txRecorder{MemoryBroker: queue.NewMemoryBroker()}
		bus := eventbus.New(rec)
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		eventbus.Subscribe(bus, evt, "provision", handler)

		tx := struct{ name string }{"fake-tx"}
		require.NoError(t, eventbus.PublishTx(context.Background(), bus, tx, evt, userCreated{Email: "a@b.c"}))
		assert.Equal(t, tx, rec.tx)
		assert.Len(t, rec.jobs, 2)
	})

	t.Run("no subscriptions is a no-op", func(t *testing.T) {
		t.Parallel()
		rec := &txRecorder{MemoryBroker: queue.NewMemoryBroker()}
		bus := eventbus.New(rec)
		require.NoError(t, eventbus.PublishTx(context.Background(), bus, "tx", evt, userCreated{}))
		assert.Empty(t, rec.jobs)
	})

	t.Run("broker without TxPusher", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.New(queue.NewMemoryBroker())
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		err := eventbus.PublishTx(context.Background(), bus, "tx", evt, userCreated{})
		assert.ErrorIs(t, err, eventbus.ErrTxUnsupported)
	})

	t.Run("sync bus", func(t *testing.T) {
		t.Parallel()
		bus := eventbus.NewSync()
		eventbus.Subscribe(bus, evt, "send_welcome", handler)
		err := eventbus.PublishTx(context.Background(), bus, "tx", evt, userCreated{})
		assert.ErrorIs(t, err, eventbus.ErrTxUnsupported)
	})
}
