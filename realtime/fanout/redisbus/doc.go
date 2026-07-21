// Package redisbus is the Redis Pub/Sub backplane for realtime/fanout. Every
// instance publishes to one Redis channel through the caller's client and
// receives on a subscribed connection, so a fanout.Hub built with
// fanout.WithBus(bus) spans all instances sharing the Redis.
//
//	client, _ := redis.Open(ctx)                 // data/redis, caller-owned
//	bus, _ := redisbus.New(client)
//	hub, _ := fanout.New(fanout.WithBus(bus))
//	// run the receive loop under ops/supervisor:
//	sup := supervisor.New(supervisor.WithService(bus))
//
// Messages are framed as topic + NUL + payload — binary-safe with zero
// encoding overhead; topics containing NUL are rejected (the hub never emits
// them).
//
// Delivery is at-most-once: Redis Pub/Sub does not buffer for absent
// subscribers, and messages published while the receive connection
// reconnects are lost — exactly the fanout contract. The Run loop rides
// go-redis's built-in resubscribe and never returns until its context is
// cancelled.
package redisbus
