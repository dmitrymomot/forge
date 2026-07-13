package rng_test

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/gaming/rng"
)

var errBoom = errors.New("boom")

type failingStore struct{}

func (failingStore) Active(context.Context, string, string) (rng.Record, error) {
	return rng.Record{}, errBoom
}
func (failingStore) Create(context.Context, rng.Record) error { return errBoom }
func (failingStore) ConsumeNonce(context.Context, string, string) (rng.Record, error) {
	return rng.Record{}, errBoom
}
func (failingStore) Reveal(context.Context, string, string, time.Time) (rng.Record, error) {
	return rng.Record{}, errBoom
}
func (failingStore) Get(context.Context, string, string) (rng.Record, error) {
	return rng.Record{}, errBoom
}
