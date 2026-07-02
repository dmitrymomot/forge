package backoff_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/backoff"
)

func TestConstant(t *testing.T) {
	b := backoff.Constant(2 * time.Second)
	assert.Equal(t, 2*time.Second, b.Next(1))
	assert.Equal(t, 2*time.Second, b.Next(9))
}

func TestExponential(t *testing.T) {
	b := backoff.Exponential(100*time.Millisecond, 10*time.Second)
	assert.Equal(t, 100*time.Millisecond, b.Next(1))
	assert.Equal(t, 200*time.Millisecond, b.Next(2))
	assert.Equal(t, 400*time.Millisecond, b.Next(3))
	assert.Equal(t, 10*time.Second, b.Next(30)) // capped at max
}

func TestExponentialMultiplier(t *testing.T) {
	b := backoff.Exponential(10*time.Millisecond, time.Minute, backoff.WithMultiplier(3))
	assert.Equal(t, 10*time.Millisecond, b.Next(1))
	assert.Equal(t, 30*time.Millisecond, b.Next(2))
	assert.Equal(t, 90*time.Millisecond, b.Next(3))
}

func TestExponentialJitterWithinBounds(t *testing.T) {
	b := backoff.Exponential(100*time.Millisecond, 10*time.Second, backoff.WithJitter(0.5))
	for range 200 {
		d := b.Next(1) // base 100ms ±50% => [50ms, 150ms]
		assert.GreaterOrEqual(t, d, 50*time.Millisecond)
		assert.LessOrEqual(t, d, 150*time.Millisecond)
	}
}
