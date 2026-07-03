package circuitbreaker_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

var errBoom = errors.New("boom")

func fail(context.Context) error { return errBoom }
func ok(context.Context) error   { return nil }

func TestOpensAfterThresholdAndFastFails(t *testing.T) {
	b := circuitbreaker.New(circuitbreaker.WithFailureThreshold(3))
	for range 3 {
		_ = b.Do(t.Context(), fail)
	}
	assert.Equal(t, circuitbreaker.StateOpen, b.State())

	err := b.Do(t.Context(), func(context.Context) error {
		t.Fatal("fn must not run while open")
		return nil
	})
	assert.ErrorIs(t, err, circuitbreaker.ErrOpen)
}

func TestHalfOpenProbeRecovers(t *testing.T) {
	clk := clock.NewMock(time.Now())
	var transitions []string
	b := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(2),
		circuitbreaker.WithOpenTimeout(10*time.Second),
		circuitbreaker.WithClock(clk),
		circuitbreaker.WithOnStateChange(func(from, to circuitbreaker.State) {
			transitions = append(transitions, from.String()+"->"+to.String())
		}),
	)
	_ = b.Do(t.Context(), fail)
	_ = b.Do(t.Context(), fail)
	assert.Equal(t, circuitbreaker.StateOpen, b.State())

	clk.Advance(10 * time.Second)
	assert.NoError(t, b.Do(t.Context(), ok))
	assert.Equal(t, circuitbreaker.StateClosed, b.State())
	assert.Equal(t, []string{"closed->open", "open->half-open", "half-open->closed"}, transitions)
}

func TestHalfOpenProbeFailureReopens(t *testing.T) {
	clk := clock.NewMock(time.Now())
	b := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(1),
		circuitbreaker.WithOpenTimeout(5*time.Second),
		circuitbreaker.WithClock(clk),
	)
	_ = b.Do(t.Context(), fail)
	assert.Equal(t, circuitbreaker.StateOpen, b.State())
	clk.Advance(5 * time.Second)
	_ = b.Do(t.Context(), fail) // half-open probe fails
	assert.Equal(t, circuitbreaker.StateOpen, b.State())
}

func TestHalfOpenMaxAdmitsConcurrentProbesThenRejects(t *testing.T) {
	clk := clock.NewMock(time.Now())
	b := circuitbreaker.New(
		circuitbreaker.WithFailureThreshold(1),
		circuitbreaker.WithOpenTimeout(time.Second),
		circuitbreaker.WithHalfOpenMax(2),
		circuitbreaker.WithClock(clk),
	)
	_ = b.Do(t.Context(), fail) // trip open
	assert.Equal(t, circuitbreaker.StateOpen, b.State())
	clk.Advance(time.Second) // allow half-open probing

	inProbe := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_ = b.Do(t.Context(), func(context.Context) error {
				inProbe <- struct{}{}
				<-release
				return nil
			})
		})
	}
	<-inProbe // both probes admitted and running in half-open
	<-inProbe
	// A third probe must be rejected: halfOpenIn(2) >= halfOpenMax(2).
	assert.ErrorIs(t, b.Do(t.Context(), ok), circuitbreaker.ErrOpen)
	close(release)
	wg.Wait()
	// Both probes succeeded → breaker closed.
	assert.Equal(t, circuitbreaker.StateClosed, b.State())
}
