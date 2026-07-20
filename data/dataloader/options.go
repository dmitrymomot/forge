package dataloader

import "time"

type config struct {
	wait     time.Duration
	maxBatch int
}

// Option configures a Loader at construction time.
type Option func(*config)

func newConfig(opts ...Option) config {
	c := config{wait: time.Millisecond, maxBatch: 100}
	for _, o := range opts {
		o(&c)
	}
	c.wait = max(c.wait, 0)
	c.maxBatch = max(c.maxBatch, 0)
	return c
}

// WithWait sets the batching window: how long an open batch collects keys
// before fetching. Default 1ms. Zero (or negative) fires each batch as soon
// as its opener releases the loader — callers scheduling under one lock hold
// (LoadMany) still coalesce, but the cross-goroutine window is gone. The
// window is also the latency bound on an abandoned batch: a request that dies
// right after scheduling still fires its BatchFunc up to this much later.
func WithWait(d time.Duration) Option { return func(c *config) { c.wait = d } }

// WithMaxBatchSize caps keys per BatchFunc call; a full batch fetches
// immediately without waiting out the window. Default 100. Zero or negative
// removes the cap.
func WithMaxBatchSize(n int) Option { return func(c *config) { c.maxBatch = n } }
