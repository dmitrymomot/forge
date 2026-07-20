package eventrouter_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

// fastQueueConfig shrinks the worker poll interval so durable tests settle
// quickly.
func fastQueueConfig() queue.Config {
	cfg := queue.DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	return cfg
}

// startService runs svc in the background and returns a stop func that
// cancels it and waits for drain.
func startService(t *testing.T, svc *queue.Service) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = svc.Run(ctx); close(stopped) }()
	return func() {
		cancel()
		<-stopped
	}
}
