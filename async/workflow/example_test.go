package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/workflow"
)

type payout struct {
	DebitID string `json:"debit_id"`
	Amount  int    `json:"amount"`
}

func Example() {
	broker := queue.NewMemoryBroker()
	store := workflow.NewMemoryStore()

	done := make(chan struct{})
	wf := workflow.New("payout.execute",
		workflow.Step[payout]{
			Name: "debit_ledger",
			Run: func(_ context.Context, p *payout) error {
				p.DebitID = "debit-42"
				fmt.Println("debited", p.Amount)
				return nil
			},
			Compensate: func(_ context.Context, p *payout) error {
				fmt.Println("refunded", p.DebitID)
				return nil
			},
		},
		workflow.Step[payout]{
			Name: "send_transfer",
			Run: func(_ context.Context, p *payout) error {
				defer close(done)
				// A business failure no retry can fix: unwind the debit.
				return workflow.Fail(errors.New("recipient account closed"))
			},
		},
	)

	eng := workflow.NewEngine(broker, store)
	workflow.Register(eng, wf)

	cfg := queue.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	svc, err := workflow.NewService(eng, queue.WithConfig(cfg))
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = svc.Run(ctx); close(stopped) }()

	runID, err := workflow.Start(ctx, eng, wf, payout{Amount: 100})
	if err != nil {
		panic(err)
	}
	<-done
	cancel()
	<-stopped

	run, err := store.Get(context.Background(), runID)
	if err != nil {
		panic(err)
	}
	fmt.Println("status:", run.Status)

	// Output:
	// debited 100
	// refunded debit-42
	// status: failed
}
