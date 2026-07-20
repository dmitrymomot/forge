package workflow_test

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/workflow"
)

type benchState struct {
	N int `json:"n"`
}

func benchWorkflow(name string, steps int) *workflow.Workflow[benchState] {
	defs := make([]workflow.Step[benchState], 0, steps)
	for i := range steps {
		defs = append(defs, workflow.Step[benchState]{
			Name: "step_" + strconv.Itoa(i),
			Run:  func(_ context.Context, s *benchState) error { s.N++; return nil },
		})
	}
	return workflow.New(name, defs...)
}

func BenchmarkStart_Memory(b *testing.B) {
	wf := benchWorkflow("bench.start", 3)
	eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore())
	workflow.Register(eng, wf)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := workflow.Start(ctx, eng, wf, benchState{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRunThroughput_Memory measures end-to-end runs per second through
// the worker service: N runs started up front, the service drains them, and
// each iteration is one completed run (steps, checkpoints, and queue
// round-trips included).
func BenchmarkRunThroughput_Memory(b *testing.B) {
	for _, steps := range []int{1, 5} {
		b.Run(fmt.Sprintf("steps_%d", steps), func(b *testing.B) {
			var completed atomic.Int64
			done := make(chan struct{}, 1)
			wf := workflow.New("bench.throughput",
				append(benchWorkflowSteps(steps-1), workflow.Step[benchState]{
					Name: "last",
					Run: func(context.Context, *benchState) error {
						if completed.Add(1) == int64(b.N) {
							done <- struct{}{}
						}
						return nil
					},
				})...)
			eng := workflow.NewEngine(queue.NewMemoryBroker(), workflow.NewMemoryStore())
			workflow.Register(eng, wf)
			cfg := queue.DefaultConfig()
			cfg.PollInterval = time.Millisecond
			svc, err := workflow.NewService(eng, queue.WithConfig(cfg))
			if err != nil {
				b.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			go func() { _ = svc.Run(ctx); close(stopped) }()
			defer func() { cancel(); <-stopped }()

			bctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := workflow.Start(bctx, eng, wf, benchState{}); err != nil {
					b.Fatal(err)
				}
			}
			<-done
		})
	}
}

func benchWorkflowSteps(n int) []workflow.Step[benchState] {
	defs := make([]workflow.Step[benchState], 0, n)
	for i := range n {
		defs = append(defs, workflow.Step[benchState]{
			Name: "step_" + strconv.Itoa(i),
			Run:  func(_ context.Context, s *benchState) error { s.N++; return nil },
		})
	}
	return defs
}
