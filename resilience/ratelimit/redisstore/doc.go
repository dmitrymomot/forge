// Package redisstore implements ratelimit.Store over a go-redis client, so a
// sliding-window Limiter shares one counter across instances. Incr is a Lua
// script that INCRBYs and sets the TTL only on the window's first hit; the
// client's lifecycle stays with the caller.
//
//	store := redisstore.New(client, redisstore.WithPrefix("rl:"))
//	limiter := ratelimit.New(store, ratelimit.WithLimit(100, time.Minute))
package redisstore
