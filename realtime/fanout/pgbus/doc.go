// Package pgbus is the Postgres LISTEN/NOTIFY backplane for realtime/fanout:
// multi-instance push with zero new infrastructure. Every instance publishes
// with pg_notify on one Postgres channel and receives on a dedicated
// listening connection, so a fanout.Hub built with fanout.WithBus(bus) spans
// all instances sharing the database.
//
//	bus, _ := pgbus.New(pool)
//	hub, _ := fanout.New(fanout.WithBus(bus))
//	// run the receive loop under ops/supervisor:
//	sup := supervisor.New(supervisor.WithService(bus))
//
// Messages ride a JSON envelope (topic + base64 payload) inside the NOTIFY
// payload, which Postgres caps at 8000 bytes: Publish rejects anything whose
// envelope exceeds that with ErrPayloadTooLarge. Fanout payloads are UI
// pushes — keep them small and let clients fetch the rest.
//
// Delivery is at-most-once. NOTIFY drops nothing while the listener is
// connected, but messages published while the listener reconnects are lost —
// exactly the fanout contract. The Run loop reconnects with exponential
// backoff and never returns until its context is cancelled.
//
// While Run is live it holds one connection from the pool exclusively for
// LISTEN; size pgxpool.Pool.MaxConns to account for it when the pool is
// shared with application traffic.
package pgbus
