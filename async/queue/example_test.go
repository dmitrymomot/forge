package queue_test

import (
	"context"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

type sendWelcome struct {
	Email string `json:"email"`
}

var kindSendWelcome = queue.NewKind[sendWelcome]("example.send_welcome")

func Example() {
	broker := queue.NewMemoryBroker()

	cfg := queue.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	svc, err := queue.NewService(broker, queue.WithConfig(cfg))
	if err != nil {
		panic(err)
	}

	done := make(chan struct{})
	queue.Register(svc, kindSendWelcome, func(_ context.Context, p sendWelcome) error {
		fmt.Println("welcome sent to", p.Email)
		close(done)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = svc.Run(ctx); close(stopped) }()

	client := queue.NewClient(broker)
	_ = queue.Push(context.Background(), client, kindSendWelcome, sendWelcome{Email: "new@user.dev"})

	<-done
	cancel()
	<-stopped
	// Output: welcome sent to new@user.dev
}

func ExamplePushMany() {
	broker := queue.NewMemoryBroker()
	client := queue.NewClient(broker)

	batch := []sendWelcome{{Email: "a@user.dev"}, {Email: "b@user.dev"}}
	if err := queue.PushMany(context.Background(), client, kindSendWelcome, batch); err != nil {
		panic(err)
	}

	st, _ := broker.Stats(context.Background())
	fmt.Println("pending:", st["default"].Pending)
	// Output: pending: 2
}
