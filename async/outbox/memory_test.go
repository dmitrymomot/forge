package outbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/outbox"
	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
)

var testEpoch = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func makeJob(id string, created time.Time) queue.Job {
	return queue.Job{
		ID:        id,
		Queue:     "default",
		Type:      "test.job",
		Payload:   []byte(`{"n":1}`),
		RunAt:     created,
		CreatedAt: created,
	}
}

func TestMemoryStore_AddClaimOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := outbox.NewMemoryStore()

	require.NoError(t, s.Add(ctx, nil,
		makeJob("b", testEpoch.Add(time.Second)),
		makeJob("a", testEpoch),
		makeJob("c", testEpoch.Add(2*time.Second)),
	))

	got, err := s.Claim(ctx, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Job.ID)
	assert.Equal(t, "b", got[1].Job.ID)
	assert.Equal(t, 1, got[0].Attempts)
	assert.Empty(t, got[0].LastError)

	got, err = s.Claim(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "c", got[0].Job.ID)

	got, err = s.Claim(ctx, 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "all rows leased")
}

func TestMemoryStore_TieBreakOnID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := outbox.NewMemoryStore()
	require.NoError(t, s.Add(ctx, nil, makeJob("y", testEpoch), makeJob("x", testEpoch)))

	got, err := s.Claim(ctx, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "x", got[0].Job.ID)
	assert.Equal(t, "y", got[1].Job.ID)
}

func TestMemoryStore_LeaseExpiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewMock(testEpoch)
	s := outbox.NewMemoryStore(outbox.WithMemoryClock(clk))
	require.NoError(t, s.Add(ctx, nil, makeJob("a", testEpoch)))

	got, err := s.Claim(ctx, 1, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, got, 1)

	got, err = s.Claim(ctx, 1, 30*time.Second)
	require.NoError(t, err)
	assert.Empty(t, got, "leased row is hidden")

	clk.Advance(31 * time.Second)
	got, err = s.Claim(ctx, 1, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].Attempts, "reclaim after lease expiry increments attempts")
}

func TestMemoryStore_FailReschedules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewMock(testEpoch)
	s := outbox.NewMemoryStore(outbox.WithMemoryClock(clk))
	require.NoError(t, s.Add(ctx, nil, makeJob("a", testEpoch)))

	_, err := s.Claim(ctx, 1, time.Minute)
	require.NoError(t, err)
	require.NoError(t, s.Fail(ctx, "a", testEpoch.Add(10*time.Second), "boom"))

	got, err := s.Claim(ctx, 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "row in backoff is hidden")

	clk.Advance(10 * time.Second)
	got, err = s.Claim(ctx, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].Attempts)
	assert.Equal(t, "boom", got[0].LastError)
}

func TestMemoryStore_PickByOverdueReturnByCreated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clock.NewMock(testEpoch)
	s := outbox.NewMemoryStore(outbox.WithMemoryClock(clk))

	// Three rows in created order r1 < r2 < r3, then a retry backlog that
	// reverses their availability: r3 most overdue, r1 least.
	require.NoError(t, s.Add(ctx, nil,
		makeJob("r1", testEpoch),
		makeJob("r2", testEpoch.Add(time.Second)),
		makeJob("r3", testEpoch.Add(2*time.Second)),
	))
	_, err := s.Claim(ctx, 3, time.Minute)
	require.NoError(t, err)
	require.NoError(t, s.Fail(ctx, "r1", testEpoch.Add(30*time.Second), "later"))
	require.NoError(t, s.Fail(ctx, "r2", testEpoch.Add(20*time.Second), "later"))
	require.NoError(t, s.Fail(ctx, "r3", testEpoch.Add(10*time.Second), "later"))
	clk.Advance(31 * time.Second) // all due again

	// Pick order is most-overdue first (r3, r2); return order is (created, id).
	got, err := s.Claim(ctx, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "r2", got[0].Job.ID)
	assert.Equal(t, "r3", got[1].Job.ID)

	got, err = s.Claim(ctx, 2, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1, "r1 was least overdue and must not have been picked")
	assert.Equal(t, "r1", got[0].Job.ID)
}

func TestMemoryStore_FailUnknownIgnored(t *testing.T) {
	t.Parallel()
	s := outbox.NewMemoryStore()
	require.NoError(t, s.Fail(context.Background(), "ghost", testEpoch, "boom"))
}

func TestMemoryStore_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := outbox.NewMemoryStore()
	require.NoError(t, s.Add(ctx, nil, makeJob("a", testEpoch), makeJob("b", testEpoch)))

	require.NoError(t, s.Delete(ctx, "a", "ghost"))
	st, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Pending)

	require.NoError(t, s.Delete(ctx), "empty batch is a no-op")
}

func TestMemoryStore_Stats(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := outbox.NewMemoryStore()

	st, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, outbox.Stats{}, st)

	require.NoError(t, s.Add(ctx, nil, makeJob("b", testEpoch.Add(time.Hour)), makeJob("a", testEpoch)))
	st, err = s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, st.Pending)
	assert.False(t, st.PendingCapped)
	assert.True(t, st.Oldest.Equal(testEpoch))
}

func TestMemoryStore_ClaimNonPositive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := outbox.NewMemoryStore()
	require.NoError(t, s.Add(ctx, nil, makeJob("a", testEpoch)))

	got, err := s.Claim(ctx, 0, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMemoryStore_AddEmpty(t *testing.T) {
	t.Parallel()
	s := outbox.NewMemoryStore()
	require.NoError(t, s.Add(context.Background(), nil))
	st, err := s.Stats(context.Background())
	require.NoError(t, err)
	assert.Zero(t, st.Pending)
}
