// Package idempotency provides Idempotency-Key middleware for mutating,
// partner-facing API calls: it replays the first response to a retry, rejects a
// concurrent in-flight duplicate with 409, and rejects a key reused with a
// different payload with 422.
//
// The first request with a given key atomically claims it (cache.Store SetNX)
// and, if its status is < 500, its status/headers/body are stored under a TTL
// and replayed to later retries. A 5xx releases the claim so a genuine retry
// re-executes. Set-Cookie and hop-by-hop response headers are never replayed.
// The middleware buffers the response, so it is not for streaming endpoints.
//
// The in-memory cache.Store is LRU-evicting and unsuitable here; back it with
// cache/redis or another durable Store in production.
//
//	store := redis.NewStore(rdb) // resilience/cache/redis
//	mux.Handle("/v1/charges", idempotency.New(store,
//		idempotency.WithTTL(24*time.Hour),
//	)(chargeHandler))
package idempotency
