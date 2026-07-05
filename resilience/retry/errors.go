package retry

import "time"

// RetryAfterError is implemented by errors that carry a minimum delay before
// the next attempt — e.g. an HTTP 429/503 with a Retry-After header, or a
// circuitbreaker open error. Retrier.Do treats the reported duration as a
// floor: it waits at least this long before the next attempt, even beyond the
// backoff ceiling. The context deadline still bounds the total.
type RetryAfterError interface {
	error
	RetryAfter() time.Duration
}
