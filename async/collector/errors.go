package collector

import "errors"

var (
	// ErrInvalidConfig is returned by New when the configuration fails
	// validation.
	ErrInvalidConfig = errors.New("collector: invalid config")
	// ErrNilSink is returned by New when the sink is nil.
	ErrNilSink = errors.New("collector: nil sink")
	// ErrBufferFull is returned by Add when the buffer is at capacity: the
	// event is dropped (drop-newest), counted, and reported by the flusher.
	ErrBufferFull = errors.New("collector: buffer full, event dropped")
	// ErrScopeMissing is returned by Add when a scope hook is configured and
	// yields an error or empty scope.
	ErrScopeMissing = errors.New("collector: scope missing")
	// ErrClosed is returned by Add once the collector has begun shutting
	// down: the flusher is draining and new events would never be delivered.
	ErrClosed = errors.New("collector: closed")
)
