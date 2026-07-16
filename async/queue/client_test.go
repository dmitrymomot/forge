package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
)

var kindWelcome = queue.NewKind[welcomePayload]("email.send_welcome")

func TestPush_Defaults(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u1"}))

	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	j := got[0]
	assert.NotEmpty(t, j.ID)
	assert.Equal(t, "default", j.Queue)
	assert.Equal(t, "email.send_welcome", j.Type)
	assert.JSONEq(t, `{"user_id":"u1"}`, string(j.Payload))
	assert.Empty(t, j.Scope)
	assert.Zero(t, j.MaxAttempts, "0 = worker default")
	assert.False(t, j.CreatedAt.IsZero())
}

func TestPush_QueueAndMaxAttempts(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u1"},
		queue.WithQueue("critical"), queue.WithMaxAttempts(3)))

	got, err := b.Claim(ctx, "critical", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].MaxAttempts)
}

func TestPush_DelayAndRunAt(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	mock := clock.NewMock(start)
	b := queue.NewMemoryBroker(queue.WithMemoryClock(mock))
	c := queue.NewClient(b, queue.WithClientClock(mock))
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "d"}, queue.WithDelay(5*time.Minute)))
	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "r"}, queue.WithRunAt(start.Add(10*time.Minute))))

	got, err := b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "delayed jobs must not be due yet")

	mock.Advance(6 * time.Minute)
	got, err = b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the 5m-delayed job is due")
	require.NoError(t, b.Ack(ctx, got[0].ID, got[0].Token)) // ack so an expired lease cannot double-claim below

	mock.Advance(5 * time.Minute)
	got, err = b.Claim(ctx, "default", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1, "the run-at job is due now")
}

func TestPush_ScopeFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("hook error", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker(), queue.WithScope(func(context.Context) (string, error) {
			return "", errors.New("no tenant")
		}))
		err := queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrScopeMissing)
	})
	t.Run("empty scope", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker(), queue.WithScope(func(context.Context) (string, error) {
			return "", nil
		}))
		err := queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrScopeMissing)
	})
	t.Run("scope captured", func(t *testing.T) {
		t.Parallel()
		b := queue.NewMemoryBroker()
		c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) {
			return "tenant-a", nil
		}))
		require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"}))
		got, err := b.Claim(ctx, "default", 1, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "tenant-a", got[0].Scope)
	})
}

func TestPushRaw(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, c.PushRaw(ctx, "legacy.import", json.RawMessage(`{"x":1}`)))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "legacy.import", got[0].Type)

	assert.Error(t, c.PushRaw(ctx, "", json.RawMessage(`{}`)), "empty name rejected")
	assert.Error(t, c.PushRaw(ctx, "x", json.RawMessage(`not json`)), "invalid JSON rejected")
}

type txBroker struct {
	*queue.MemoryBroker
	gotTx  any
	gotJob queue.Job
}

func (b *txBroker) PushTx(_ context.Context, tx any, jobs ...queue.Job) error {
	b.gotTx = tx
	if len(jobs) > 0 {
		b.gotJob = jobs[0]
	}
	return nil
}

func TestPushTx(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("capability present", func(t *testing.T) {
		t.Parallel()
		b := &txBroker{MemoryBroker: queue.NewMemoryBroker()}
		c := queue.NewClient(b)
		fakeTx := struct{ name string }{"tx"}
		require.NoError(t, queue.PushTx(ctx, c, fakeTx, kindWelcome, welcomePayload{UserID: "u"}, queue.WithQueue("critical")))
		assert.Equal(t, fakeTx, b.gotTx)
		assert.Equal(t, "critical", b.gotJob.Queue)
		assert.Equal(t, "email.send_welcome", b.gotJob.Type)
	})
	t.Run("capability absent", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker())
		err := queue.PushTx(ctx, c, "tx", kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrTxUnsupported)
	})
	t.Run("scope enforced on PushTx", func(t *testing.T) {
		t.Parallel()
		b := &txBroker{MemoryBroker: queue.NewMemoryBroker()}
		c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) { return "", nil }))
		err := queue.PushTx(ctx, c, "tx", kindWelcome, welcomePayload{UserID: "u"})
		assert.ErrorIs(t, err, queue.ErrScopeMissing)
	})
}

func TestClient_DLQPassthrough(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	c := queue.NewClient(b)
	ctx := context.Background()

	require.NoError(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"}))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, b.Kill(ctx, got[0].ID, got[0].Token, "poison"))

	dead, err := c.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)

	st, err := c.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st["default"].Dead)

	require.NoError(t, c.Requeue(ctx, dead[0].ID))
	reclaimed, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.NoError(t, b.Kill(ctx, reclaimed[0].ID, reclaimed[0].Token, "again"))
	require.NoError(t, c.Purge(ctx, reclaimed[0].ID))
	dead, err = c.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	assert.Empty(t, dead)
}

func TestPushMany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("batch claims back in order with unique ids", func(t *testing.T) {
		t.Parallel()
		b := queue.NewMemoryBroker()
		c := queue.NewClient(b)
		require.NoError(t, queue.PushMany(ctx, c, kindWelcome, []welcomePayload{{UserID: "a"}, {UserID: "b"}, {UserID: "c"}}))
		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 3)
		seen := map[string]bool{}
		for _, j := range got {
			assert.False(t, seen[j.ID], "ids must be unique")
			seen[j.ID] = true
		}
	})
	t.Run("scope hook runs once per batch", func(t *testing.T) {
		t.Parallel()
		b := queue.NewMemoryBroker()
		var hookCalls int
		c := queue.NewClient(b, queue.WithScope(func(context.Context) (string, error) {
			hookCalls++
			return "tenant-a", nil
		}))
		require.NoError(t, queue.PushMany(ctx, c, kindWelcome, []welcomePayload{{UserID: "a"}, {UserID: "b"}}))
		assert.Equal(t, 1, hookCalls, "one scope resolution per batch, not per job")
		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "tenant-a", got[0].Scope)
		assert.Equal(t, "tenant-a", got[1].Scope)
	})
	t.Run("empty slice is a no-op", func(t *testing.T) {
		t.Parallel()
		c := queue.NewClient(queue.NewMemoryBroker())
		require.NoError(t, queue.PushMany(ctx, c, kindWelcome, nil))
	})
}

func TestPush_EmptyQueueNameRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := queue.NewClient(queue.NewMemoryBroker())
	assert.Error(t, queue.Push(ctx, c, kindWelcome, welcomePayload{UserID: "u"}, queue.WithQueue("")))
	assert.Error(t, queue.PushMany(ctx, c, kindWelcome, []welcomePayload{{UserID: "u"}}, queue.WithQueue("")))
	assert.Error(t, c.PushRaw(ctx, "raw.kind", []byte(`{}`), queue.WithQueue("")))
}
