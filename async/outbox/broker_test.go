package outbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/outbox"
	"github.com/dmitrymomot/forge/async/queue"
)

func TestWrap_PanicsOnNil(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { outbox.Wrap(nil, queue.NewMemoryBroker()) })
	assert.Panics(t, func() { outbox.Wrap(outbox.NewMemoryStore(), nil) })
}

func TestBroker_PushTxWritesStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := outbox.NewMemoryStore()
	inner := queue.NewMemoryBroker()
	b := outbox.Wrap(store, inner)

	require.NoError(t, b.PushTx(ctx, nil, makeJob("a", testEpoch)))

	st, err := store.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Pending, "PushTx lands in the store")
	bst, err := inner.Stats(ctx)
	require.NoError(t, err)
	assert.Empty(t, bst, "nothing reaches the broker before the relay runs")
}

func TestBroker_PushDelegates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := outbox.NewMemoryStore()
	inner := queue.NewMemoryBroker()
	b := outbox.Wrap(store, inner)

	require.NoError(t, b.Push(ctx, makeJob("a", testEpoch)))

	st, err := store.Stats(ctx)
	require.NoError(t, err)
	assert.Zero(t, st.Pending, "plain Push bypasses the store")
	claimed, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, b.Ack(ctx, claimed[0].ID, claimed[0].Token))
}

func TestBroker_DelegatesDLQAndStats(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inner := queue.NewMemoryBroker()
	b := outbox.Wrap(outbox.NewMemoryStore(), inner)

	require.NoError(t, b.Push(ctx, makeJob("a", testEpoch)))
	claimed, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, b.Extend(ctx, claimed[0].ID, claimed[0].Token, time.Minute))
	require.NoError(t, b.Kill(ctx, claimed[0].ID, claimed[0].Token, "poison"))

	dead, err := b.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)
	require.NoError(t, b.Requeue(ctx, dead[0].ID))

	claimed, err = b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, b.Nack(ctx, claimed[0].ID, claimed[0].Token, testEpoch, "later"))

	claimed, err = b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, b.Kill(ctx, claimed[0].ID, claimed[0].Token, "dead again"))
	dead, err = b.ListDead(ctx, "default", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)
	require.NoError(t, b.Purge(ctx, dead[0].ID))

	n, err := b.PurgeDeadBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
	stats, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.NotNil(t, stats)
}

type maintainBroker struct {
	queue.Broker
	called int
}

func (m *maintainBroker) Maintain(context.Context) error {
	m.called++
	return nil
}

func TestBroker_MaintainDelegates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inner := &maintainBroker{Broker: queue.NewMemoryBroker()}
	b := outbox.Wrap(outbox.NewMemoryStore(), inner)
	require.NoError(t, b.Maintain(ctx))
	assert.Equal(t, 1, inner.called)

	plain := outbox.Wrap(outbox.NewMemoryStore(), queue.NewMemoryBroker())
	require.NoError(t, plain.Maintain(ctx), "no-op when the wrapped broker has no Maintainer")
}
