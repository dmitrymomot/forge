package queue

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
