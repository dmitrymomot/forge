package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
)

func newWeightedService(t *testing.T, weights map[string]int) *Service {
	t.Helper()
	s, err := NewService(NewMemoryBroker(), WithQueues(weights))
	require.NoError(t, err)
	return s
}

func TestPickNext_ProportionalSequence(t *testing.T) {
	t.Parallel()
	s := newWeightedService(t, map[string]int{"a": 6, "b": 3, "c": 1})
	counts := map[string]int{}
	var seq []string
	for range 10 {
		n := s.pickNext()
		counts[n]++
		seq = append(seq, n)
	}
	assert.Equal(t, map[string]int{"a": 6, "b": 3, "c": 1}, counts, "one full SWRR cycle is exactly proportional")
	assert.Equal(t, []string{"a", "b", "a", "a", "b", "a", "c", "a", "b", "a"}, seq, "canonical nginx SWRR order for 6/3/1")
}

func TestClaimPlan_FullBudgetMatchesWeights(t *testing.T) {
	t.Parallel()
	s := newWeightedService(t, map[string]int{"a": 6, "b": 3, "c": 1})
	order, quota := s.claimPlan(10)
	assert.Equal(t, map[string]int{"a": 6, "b": 3, "c": 1}, quota)
	assert.Equal(t, "a", order[0], "heaviest queue claims first on a fresh service")
	assert.Len(t, order, 3)
}

func TestClaimPlan_SingleSlotRotates(t *testing.T) {
	t.Parallel()
	s := newWeightedService(t, map[string]int{"a": 6, "b": 3, "c": 1})
	counts := map[string]int{}
	for range 10 {
		order, quota := s.claimPlan(1)
		require.Len(t, order, 1)
		assert.Equal(t, 1, quota[order[0]])
		counts[order[0]]++
	}
	assert.Equal(t, map[string]int{"a": 6, "b": 3, "c": 1}, counts, "free=1 polls must rotate proportionally, never starving light queues")
}

// fakeQueueBroker scripts Claim per call and records every call it received.
// Unlike the black-box countingBroker in service_test.go (which observes
// wall-clock claim rates over many polls), this lets a single pollOnce call
// be pinned exactly: which queues were attempted, with what budget, in what
// order.
type fakeQueueBroker struct {
	claim func(queue string, n int) ([]ClaimedJob, error)
	calls []claimCall
}

type claimCall struct {
	queue string
	n     int
}

func (b *fakeQueueBroker) Claim(_ context.Context, queue string, n int, _ time.Duration) ([]ClaimedJob, error) {
	b.calls = append(b.calls, claimCall{queue, n})
	return b.claim(queue, n)
}

func (b *fakeQueueBroker) Push(context.Context, ...Job) error                            { return nil }
func (b *fakeQueueBroker) Extend(context.Context, string, string, time.Duration) error   { return nil }
func (b *fakeQueueBroker) Ack(context.Context, string, string) error                     { return nil }
func (b *fakeQueueBroker) Nack(context.Context, string, string, time.Time, string) error { return nil }
func (b *fakeQueueBroker) Kill(context.Context, string, string, string) error            { return nil }
func (b *fakeQueueBroker) ListDead(context.Context, string, int) ([]Job, error)          { return nil, nil }
func (b *fakeQueueBroker) Requeue(context.Context, string) error                         { return nil }
func (b *fakeQueueBroker) Purge(context.Context, string) error                           { return nil }
func (b *fakeQueueBroker) PurgeDeadBefore(context.Context, time.Time) (int, error)       { return 0, nil }
func (b *fakeQueueBroker) Stats(context.Context) (Stats, error)                          { return Stats{}, nil }

// TestPollOnce_SweepClaimsUnpickedQueueWithBacklog pins the Task 8 review
// finding: gating the leftover sweep on "something was claimed this poll"
// (total > 0) is the wrong condition. When free capacity is smaller than the
// sum of queue weights, claimPlan(free) can pick only a subset of queues —
// here weights 9:1 with free=2 make claimPlan pick "hot" both times (see
// TestClaimPlan_SingleSlotRotates for the SWRR mechanics), so "cold" never
// enters `order`. If "hot" happens to be transiently empty this poll, total
// stays 0 even though "cold" has a real job waiting and free capacity is
// untouched. The sweep must still spend that capacity probing the queues
// claimPlan excluded — gating on remaining capacity (free > 0) does this;
// gating on total > 0 skips the sweep and leaves "cold" waiting for SWRR's
// own multi-poll rotation to reach it.
func TestPollOnce_SweepClaimsUnpickedQueueWithBacklog(t *testing.T) {
	t.Parallel()
	s, err := NewService(NewMemoryBroker(), WithQueues(map[string]int{"hot": 9, "cold": 1}), WithConcurrency(2))
	require.NoError(t, err)

	fb := &fakeQueueBroker{claim: func(queue string, n int) ([]ClaimedJob, error) {
		if queue == "cold" {
			return []ClaimedJob{{Token: "t", Job: Job{ID: "cold-1", Queue: "cold", Type: "test.kind"}}}, nil
		}
		return nil, nil // hot: transiently empty this poll
	}}
	s.broker = fb

	sem := make(chan struct{}, s.cfg.Concurrency)
	var wg sync.WaitGroup
	total, allErrored := s.pollOnce(context.Background(), context.Background(), sem, &wg)
	wg.Wait()

	require.Len(t, fb.calls, 2, "setup sanity: exactly one order pass (hot) plus one sweep pass (cold)")
	assert.Equal(t, "hot", fb.calls[0].queue, "claimPlan must pick only the heavy queue with free=2 against weights 9:1")
	assert.Equal(t, "cold", fb.calls[1].queue, "the sweep must probe the queue claimPlan excluded")
	assert.False(t, allErrored)
	assert.Equal(t, 1, total, "the backlogged cold job must be claimed within this single poll, not deferred to a later SWRR cycle")
}

// TestPollOnce_ErroredQueueNotResweptSamePoll pins the round-1 regression a
// re-review caught: gating the leftover sweep on free > 0 alone reintroduces
// double-claiming, this time on the error path. claim's error branch
// increments errored and returns before setting drained[qname] and before
// decrementing free, so a sweep gated only on remaining capacity re-probes
// every queue that just errored — hammering a broker at exactly the moment
// TestService_ClaimErrorBackoff's backoff is supposed to widen away from it.
// This mirrors that test's exact fixture: one "default" queue,
// Concurrency=10, broker always errors. Under the free > 0 (round 1) gate
// this test fails with 2 calls (order pass + sweep re-probe); the fix
// (free > 0 && errored == 0) brings it back to 1.
func TestPollOnce_ErroredQueueNotResweptSamePoll(t *testing.T) {
	t.Parallel()
	s, err := NewService(NewMemoryBroker(), WithConcurrency(10))
	require.NoError(t, err)

	fb := &fakeQueueBroker{claim: func(queue string, n int) ([]ClaimedJob, error) {
		return nil, errors.New("broker down")
	}}
	s.broker = fb

	sem := make(chan struct{}, s.cfg.Concurrency)
	var wg sync.WaitGroup
	total, allErrored := s.pollOnce(context.Background(), context.Background(), sem, &wg)
	wg.Wait()

	require.Len(t, fb.calls, 1, "an errored queue must not be re-probed by the leftover sweep in the same poll")
	assert.Equal(t, 0, total)
	assert.True(t, allErrored)
}

type sweepSpyBroker struct {
	*MemoryBroker
	mu         sync.Mutex
	maintains  int
	purges     int
	lastCutoff time.Time
}

func (b *sweepSpyBroker) Maintain(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maintains++
	return nil
}

func (b *sweepSpyBroker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	b.mu.Lock()
	b.purges++
	b.lastCutoff = cutoff
	b.mu.Unlock()
	return b.MemoryBroker.PurgeDeadBefore(ctx, cutoff)
}

func TestSweepLoop_MaintainsAndPurges(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	b := &sweepSpyBroker{MemoryBroker: NewMemoryBroker()}
	cfg := DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.DeadRetention = 24 * time.Hour
	s, err := NewService(b, WithConfig(cfg), WithServiceClock(clock.NewMock(fixed)))
	require.NoError(t, err)
	s.sweepEvery = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.maintains >= 2 && b.purges >= 2
	}, 5*time.Second, 5*time.Millisecond, "sweep must invoke Maintain and PurgeDeadBefore repeatedly")

	cancel()
	<-done
	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Equal(t, fixed.Add(-24*time.Hour), b.lastCutoff, "cutoff = clock now - DeadRetention")
}

func TestSweepLoop_RetentionZeroSkipsPurge(t *testing.T) {
	t.Parallel()
	b := &sweepSpyBroker{MemoryBroker: NewMemoryBroker()}
	cfg := DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.DeadRetention = 0
	s, err := NewService(b, WithConfig(cfg))
	require.NoError(t, err)
	s.sweepEvery = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.maintains >= 2
	}, 5*time.Second, 5*time.Millisecond, "Maintain still runs with retention disabled")

	cancel()
	<-done
	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Zero(t, b.purges, "DeadRetention=0 must never purge")
}
