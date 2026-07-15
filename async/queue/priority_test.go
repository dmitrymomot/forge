package queue_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

type orderPayload struct {
	Queue string `json:"queue"`
}

var kindOrder = queue.NewKind[orderPayload]("test.order")

// pushN pushes n kindOrder jobs to the named queue with run_at in the past so
// claim order is deterministic per queue.
func pushN(t *testing.T, c *queue.Client, q string, n int) {
	t.Helper()
	for range n {
		require.NoError(t, queue.Push(context.Background(), c, kindOrder, orderPayload{Queue: q}, queue.WithQueue(q)))
	}
}

func TestService_StrictPriorityDrainsHeavyFirst(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	cfg := testConfig()
	svc, err := queue.NewService(b,
		queue.WithConfig(cfg),
		queue.WithConcurrency(1),
		queue.WithQueues(map[string]int{"critical": 2, "low": 1}),
		queue.WithStrictPriority(),
	)
	require.NoError(t, err)

	var mu sync.Mutex
	var processed []string
	queue.Register(svc, kindOrder, func(_ context.Context, p orderPayload) error {
		mu.Lock()
		processed = append(processed, p.Queue)
		mu.Unlock()
		return nil
	})

	c := queue.NewClient(b)
	pushN(t, c, "low", 3)
	pushN(t, c, "critical", 3)

	stop := runService(t, svc)
	defer stop()

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(processed) == 6
	}, "all jobs must complete")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"critical", "critical", "critical", "low", "low", "low"}, processed, "strict mode drains critical fully before touching low")
}

func TestService_WeightedDoesNotStarveLightQueue(t *testing.T) {
	t.Parallel()
	b := queue.NewMemoryBroker()
	cfg := testConfig()
	svc, err := queue.NewService(b,
		queue.WithConfig(cfg),
		queue.WithConcurrency(1),
		queue.WithQueues(map[string]int{"heavy": 3, "light": 1}),
	)
	require.NoError(t, err)

	var mu sync.Mutex
	var processed []string
	queue.Register(svc, kindOrder, func(_ context.Context, p orderPayload) error {
		mu.Lock()
		processed = append(processed, p.Queue)
		mu.Unlock()
		return nil
	})

	c := queue.NewClient(b)
	pushN(t, c, "heavy", 6)
	pushN(t, c, "light", 2)

	stop := runService(t, svc)
	defer stop()

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(processed) == 8
	}, "all jobs must complete")

	mu.Lock()
	defer mu.Unlock()
	firstLight := -1
	for i, q := range processed {
		if q == "light" {
			firstLight = i
			break
		}
	}
	require.NotEqual(t, -1, firstLight)
	assert.LessOrEqual(t, firstLight, 4, "SWRR must interleave the light queue while heavy is backlogged (a,a,b cadence for 3/1), not append it at the end")
}
