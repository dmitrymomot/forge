package supervisor

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// forceQuitCode is the process exit code used when WithForceQuit triggers on the
// second signal (128 + SIGINT, the conventional forced-interrupt code).
const forceQuitCode = 130

// NewContext returns a context cancelled on the first SIGINT or SIGTERM. Call the
// returned CancelFunc (typically deferred in main) to release the signal handler.
// It is single-shot by default: after the first signal further signals are not
// handled. With WithForceQuit, the first signal still cancels the context for a
// graceful drain and a second signal forces os.Exit(130).
func NewContext(opts ...ContextOption) (context.Context, context.CancelFunc) {
	var cfg contextConfig
	for _, o := range opts {
		o(&cfg)
	}
	parent := cfg.parent
	if parent == nil {
		parent = context.Background()
	}
	if !cfg.forceQuit {
		return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	}

	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ch:
			cancel()
		case <-parent.Done(): // parent cancelled: nothing to force, just stop watching
			return
		case <-stopped:
			return
		}
		select {
		case <-ch:
			os.Exit(forceQuitCode)
		case <-stopped:
		}
	}()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			signal.Stop(ch)
			close(stopped)
		})
		cancel()
	}
	return ctx, stop
}
