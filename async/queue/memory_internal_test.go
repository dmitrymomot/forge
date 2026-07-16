package queue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
)

func newBucketTestJob(jobID, q string) Job {
	return Job{
		ID:          jobID,
		Queue:       q,
		Type:        "test.kind",
		Payload:     []byte(`{}`),
		MaxAttempts: 5,
		RunAt:       time.Now().Add(-time.Second),
		CreatedAt:   time.Now(),
	}
}

// TestMemoryBroker_PruneBucketOnAck pins the property that draining a queue
// via Ack removes its bucket, so pushing to many short-lived,
// dynamically-named queues does not leak a *memQueue per name forever.
func TestMemoryBroker_PruneBucketOnAck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := NewMemoryBroker()

	require.NoError(t, b.Push(ctx, newBucketTestJob("job-1", "q-ack")))
	_, ok := b.queues["q-ack"]
	require.True(t, ok, "bucket should exist right after Push")

	claimed, err := b.Claim(ctx, "q-ack", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, b.Ack(ctx, claimed[0].ID, claimed[0].Token))

	_, ok = b.queues["q-ack"]
	assert.False(t, ok, "bucket should be pruned once Ack empties both live and dead")
}

// TestMemoryBroker_PruneBucketOnPurge exercises the Kill-then-Purge path:
// Kill relocates the job from live to dead (bucket stays non-empty), and only
// the subsequent Purge empties and prunes it.
func TestMemoryBroker_PruneBucketOnPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := NewMemoryBroker()

	job := newBucketTestJob("job-2", "q-purge")
	require.NoError(t, b.Push(ctx, job))

	claimed, err := b.Claim(ctx, "q-purge", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, b.Kill(ctx, claimed[0].ID, claimed[0].Token, "boom"))
	_, ok := b.queues["q-purge"]
	require.True(t, ok, "Kill relocates live to dead within the same bucket, it must not prune it")

	require.NoError(t, b.Purge(ctx, job.ID))
	_, ok = b.queues["q-purge"]
	assert.False(t, ok, "bucket should be pruned once Purge empties the dead entry")
}

// TestMemoryBroker_PruneBucketOnPurgeDeadBefore mirrors the Purge case for the
// retention sweep entry point, including the map-delete-during-range pattern
// in PurgeDeadBefore.
func TestMemoryBroker_PruneBucketOnPurgeDeadBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mc := clock.NewMock(time.Now())
	b := NewMemoryBroker(WithMemoryClock(mc))

	require.NoError(t, b.Push(ctx, newBucketTestJob("job-3", "q-sweep")))
	claimed, err := b.Claim(ctx, "q-sweep", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.NoError(t, b.Kill(ctx, claimed[0].ID, claimed[0].Token, "boom"))

	mc.Advance(time.Hour)
	n, err := b.PurgeDeadBefore(ctx, mc.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	_, ok := b.queues["q-sweep"]
	assert.False(t, ok, "bucket should be pruned once PurgeDeadBefore empties its dead map")
}

// TestMemoryBroker_InFlightClaimKeepsBucket guards against pruning a bucket
// out from under an in-flight claimed job: Claim never removes from live, so
// a claimed-but-unfinalized job must keep its bucket alive.
func TestMemoryBroker_InFlightClaimKeepsBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := NewMemoryBroker()

	require.NoError(t, b.Push(ctx, newBucketTestJob("job-4", "q-inflight")))

	claimed, err := b.Claim(ctx, "q-inflight", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	mq, ok := b.queues["q-inflight"]
	require.True(t, ok, "a bucket with an in-flight claimed job must not be pruned")
	assert.Len(t, mq.live, 1)
	assert.Empty(t, mq.dead)
}
