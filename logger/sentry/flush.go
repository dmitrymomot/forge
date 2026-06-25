package sentry

import (
	"context"
	"time"

	sentry "github.com/getsentry/sentry-go"
)

// Flush flushes buffered Sentry events; call it before the program exits. The wait honors
// ctx's cancellation and deadline (fallback defaultFlushTimeout). Returns the context's
// error if ctx is done, or ErrSentryFlushTimeout if events remain unsent. A no-op when
// Sentry is not active. New always returns a non-nil Flush, so `defer flush(ctx)` is safe
// even when New returns an error.
type Flush func(ctx context.Context) error

const defaultFlushTimeout = 2 * time.Second

// noopFlush is returned whenever Sentry is inactive (empty DSN, init failure, or a New that
// errored before activating Sentry). Keeping it non-nil makes deferring Flush always safe.
func noopFlush(context.Context) error { return nil }

// flush flushes the global Sentry client, honoring ctx's cancellation and deadline. When
// ctx carries no deadline the wait is bounded to defaultFlushTimeout so a stuck transport
// cannot block forever. Returns the context's error if ctx is (or becomes) done, or
// ErrSentryFlushTimeout if events remain unsent within the window.
func flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil { // already canceled or past deadline
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultFlushTimeout)
		defer cancel()
	}
	if !sentry.FlushWithContext(ctx) {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrSentryFlushTimeout
	}
	return nil
}
