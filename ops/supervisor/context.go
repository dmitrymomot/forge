package supervisor

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// NewContext returns a context that is cancelled on the first SIGINT or
// SIGTERM, implemented with signal.NotifyContext. It is single-shot: after the
// first signal the context is cancelled and further signals are not handled by
// this helper. Call stop (typically deferred in main) to release the handler.
func NewContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
