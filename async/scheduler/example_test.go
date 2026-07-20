package scheduler_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/scheduler"
)

type digestPayload struct {
	Source string `json:"source"`
}

var kindDigest = queue.NewKind[digestPayload]("example.digest")

func Example() {
	broker := queue.NewMemoryBroker()
	client := queue.NewClient(broker)

	sched, err := scheduler.New(client)
	if err != nil {
		panic(err)
	}
	scheduler.Add(sched, "example.digest.tick", scheduler.Every(20*time.Millisecond), kindDigest, digestPayload{Source: "scheduler"})

	cfg := queue.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	svc, err := queue.NewService(broker, queue.WithConfig(cfg))
	if err != nil {
		panic(err)
	}
	done := make(chan struct{})
	var once sync.Once
	queue.Register(svc, kindDigest, func(_ context.Context, p digestPayload) error {
		once.Do(func() { fmt.Println("processed job from", p.Source); close(done) })
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { _ = sched.Run(ctx) })
	wg.Go(func() { _ = svc.Run(ctx) })

	<-done
	cancel()
	wg.Wait()
	// Output: processed job from scheduler
}
