package mongo

import (
	"context"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// WithTransaction runs fn inside a single multi-document transaction: it starts a
// session, calls the driver's Session.WithTransaction (which commits when fn
// returns nil and aborts when fn returns an error or panics), and ends the session.
// fn must perform its operations using the context passed to it — that context
// carries the session, so any collection call made with another context runs
// outside the transaction.
//
// This requires a replica set or a sharded (mongos) deployment; on a standalone
// server the driver returns its own error verbatim (transactions are unsupported
// there). The driver may run fn more than once on transient transaction errors, so
// fn must be idempotent in its own bookkeeping.
func WithTransaction(ctx context.Context, c *mongodriver.Client, fn func(ctx context.Context) error) error {
	sess, err := c.StartSession()
	if err != nil {
		return err
	}
	defer sess.EndSession(ctx)

	// The driver's WithTransaction callback returns (any, error); forge's fn returns
	// only an error, so the result value is unused (nil).
	_, err = sess.WithTransaction(ctx, func(sessCtx context.Context) (any, error) {
		return nil, fn(sessCtx)
	})
	return err
}
