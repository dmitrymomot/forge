package circuitbreaker_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

func TestGroupLazyCreateAndIndependence(t *testing.T) {
	g := circuitbreaker.NewGroup(circuitbreaker.WithBreakerOptions(
		circuitbreaker.WithFailureThreshold(1),
	))
	assert.Equal(t, 0, g.Len())

	_ = g.Do(t.Context(), "a", fail) // trips "a"
	_ = g.Do(t.Context(), "b", ok)   // healthy "b"

	assert.Equal(t, 2, g.Len())
	assert.Equal(t, circuitbreaker.StateOpen, g.State("a"))
	assert.Equal(t, circuitbreaker.StateClosed, g.State("b"))
	assert.Equal(t, circuitbreaker.StateClosed, g.State("never-seen"))
}

func TestGroupEvictsIdleBreakers(t *testing.T) {
	clk := clock.NewMock(time.Now())
	g := circuitbreaker.NewGroup(
		circuitbreaker.WithBreakerOptions(circuitbreaker.WithClock(clk)),
		circuitbreaker.WithIdleTTL(time.Minute),
		circuitbreaker.WithSweepInterval(time.Second),
	)
	_ = g.Do(t.Context(), "a", ok) // create "a" at t0
	clk.Advance(2 * time.Minute)
	_ = g.Do(t.Context(), "b", ok) // sweep interval elapsed -> "a" idle > 1m -> evicted

	assert.Equal(t, 1, g.Len())
	assert.Equal(t, circuitbreaker.StateClosed, g.State("a")) // evicted -> reads Closed
}

func TestGroupActiveKeySurvivesSweep(t *testing.T) {
	clk := clock.NewMock(time.Now())
	g := circuitbreaker.NewGroup(
		circuitbreaker.WithBreakerOptions(
			circuitbreaker.WithClock(clk),
			circuitbreaker.WithFailureThreshold(2),
		),
		circuitbreaker.WithIdleTTL(time.Minute),
		circuitbreaker.WithSweepInterval(time.Second),
	)
	_ = g.Do(t.Context(), "a", fail) // a: 1 failure of 2 -> still Closed
	clk.Advance(90 * time.Second)
	// breaker() refreshes a's last-access BEFORE the sweep, so the ORIGINAL a
	// survives and its SECOND failure trips it. A recreated breaker would be
	// back to one failure and stay Closed — so StateOpen proves preservation.
	_ = g.Do(t.Context(), "a", fail)
	assert.Equal(t, circuitbreaker.StateOpen, g.State("a"),
		"active key's breaker must be preserved across the sweep")
}
