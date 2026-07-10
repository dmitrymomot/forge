// Package idempotency provides Idempotency-Key middleware for mutating,
// partner-facing API calls: it replays the first response to a retry, rejects a
// concurrent in-flight duplicate with 409, and rejects a key reused with a
// different payload with 422.
//
// The first request with a given key atomically claims it (cache.Store SetNX)
// and, if its status is < 500, its status/headers/body are stored under a TTL
// and replayed to later retries. A 5xx releases the claim so a genuine retry
// re-executes. Set-Cookie and hop-by-hop response headers are never replayed.
// The middleware buffers the response to decide whether to store it, so a
// handler that flushes the response stream opts out of caching: its response
// is streamed through to the client uncached rather than replayed to retries.
//
// The in-memory cache.Store is LRU-evicting and unsuitable here; back it with
// cache/redis or another durable Store in production.
//
// The Idempotency-Key header is used verbatim as the store key, and the request
// fingerprint (method, path, body) does not include the caller's identity. In a
// multi-tenant deployment that shares one Store, keys from different principals
// can collide: the same key with the same method+path+body from a second
// principal replays the first's stored response. Give each principal its own
// Store, or namespace the key by the authenticated caller, so idempotency keys
// never cross a trust boundary.
//
// # Usage
//
//	store := redis.NewStore(rdb) // resilience/cache/redis
//	mux.Handle("/v1/charges", idempotency.New(store,
//		idempotency.WithTTL(24*time.Hour),
//	)(chargeHandler))
package idempotency
