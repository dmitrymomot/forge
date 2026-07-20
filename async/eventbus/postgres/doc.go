// Package pgeventbus is the Postgres eventbus.Inbox: an idempotency inbox
// keyed by (consumer, event id) that rides the handler's own pgx.Tx, so
// marking an event processed commits or rolls back atomically with the
// handler's writes. Apply Migrations with data/migration before use.
//
//	inbox, _ := pgeventbus.NewInbox("user.created.send_welcome")
//	eventbus.Subscribe(bus, UserCreated, "send_welcome", func(ctx context.Context, d eventbus.Delivery[UserCreatedPayload]) error {
//		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
//			seen, err := inbox.Seen(ctx, tx, d.ID)
//			if err != nil || seen {
//				return err
//			}
//			// ... handler writes in the same tx ...
//			return nil
//		})
//	})
//
// The inbox only dedups deliveries that can still arrive, so rows need to
// outlive the worker's retry horizon, not live forever: schedule
// PurgeSeenBefore (e.g. daily with a cutoff a few multiples of the retry
// horizon back) to keep the table bounded.
package pgeventbus
