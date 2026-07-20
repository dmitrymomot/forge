package collector_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/collector"
)

// TestNoStrandedEventsOnShutdown races Add against shutdown: an Add that
// passed the closed check must have its event drained, never stranded in the
// buffer after Run returns. After Run and all adders finish, every accepted
// event is accounted for as flushed or lost.
func TestNoStrandedEventsOnShutdown(t *testing.T) {
	t.Parallel()
	for range 300 {
		sink := collector.SinkFunc[int](func(context.Context, []int) error { return nil })
		c, err := collector.New(sink, collector.WithConfig(collector.Config{BufferSize: 8, BatchSize: 8, FlushInterval: time.Millisecond, FlushTimeout: time.Second}))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = c.Run(ctx) }()

		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for {
					if err := c.Add(context.Background(), 1); errors.Is(err, collector.ErrClosed) {
						return
					}
				}
			})
		}
		cancel()
		wg.Wait()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return")
		}

		if st := c.Stats(); st.Added != st.Flushed+st.Lost {
			t.Fatalf("stats %+v: %d accepted events stranded in the buffer", st, st.Added-st.Flushed-st.Lost)
		}
	}
}
