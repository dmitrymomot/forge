package loadshed_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/loadshed"
)

func TestAcquire_AdmitsWhenIdle(t *testing.T) {
	s := loadshed.New(loadshed.WithCriteria(loadshed.Concurrency(4)))
	tk, ok := s.Acquire(context.Background())
	assert.True(t, ok)
	tk.Release()
}

func TestAcquire_ShedsWhenSaturated(t *testing.T) {
	// force pressure high and rand low so the ramp always rejects
	s := loadshed.New(
		loadshed.WithCriteria(loadshed.Concurrency(1)),
		loadshed.WithThreshold(0.0),
		loadshed.WithFloor(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	tk1, ok := s.Acquire(context.Background()) // fills the single slot
	assert.True(t, ok)
	_, ok2 := s.Acquire(context.Background()) // pressure 1.0, reject prob 1.0
	assert.False(t, ok2)
	tk1.Release()
}

func TestPressure_FailsOpenOnPanic(t *testing.T) {
	s := loadshed.New(
		loadshed.WithCriteria(panicCriteria{}),
		loadshed.WithThreshold(0.0),
		loadshed.WithRand(func() float64 { return 0.0 }),
	)
	_, ok := s.Acquire(context.Background())
	assert.True(t, ok) // panic → pressure 0 → admit
}

type panicCriteria struct{}

func (panicCriteria) Pressure() float64 { panic("boom") }

// fixedCriteria reports a constant pressure, for deterministic ramp tests.
type fixedCriteria struct{ p float64 }

func (f fixedCriteria) Pressure() float64 { return f.p }

// TestAdmit_RejectionRamp drives the probabilistic rejection ramp
// deterministically via a fixed pressure and injected rand source, asserting
// the exact boundaries traced from admit(): below threshold always admits;
// at/above threshold, rejectProb = min((p-threshold)/(1-threshold), 1) *
// (1-floor), and admit iff rnd() >= rejectProb.
func TestAdmit_RejectionRamp(t *testing.T) {
	newShedder := func(pressure float64, rnd func() float64) *loadshed.Shedder {
		return loadshed.New(
			loadshed.WithCriteria(fixedCriteria{p: pressure}),
			loadshed.WithThreshold(0.5),
			loadshed.WithFloor(0.1),
			loadshed.WithRand(rnd),
		)
	}

	t.Run("below threshold admits regardless of rand", func(t *testing.T) {
		s := newShedder(0.4, func() float64 { return 0.0 }) // worst-case rand
		tk, ok := s.Acquire(context.Background())
		assert.True(t, ok)
		tk.Release()
	})

	t.Run("at saturation rejectProb is 0.9", func(t *testing.T) {
		// pressure=1.0: frac=1.0, rejectProb = 1.0*(1-0.1) = 0.9
		s := newShedder(1.0, func() float64 { return 0.0 })
		_, ok := s.Acquire(context.Background())
		assert.False(t, ok) // 0.0 >= 0.9 is false -> shed

		s = newShedder(1.0, func() float64 { return 0.95 })
		tk, ok := s.Acquire(context.Background())
		assert.True(t, ok) // 0.95 >= 0.9 is true -> admit
		tk.Release()
	})

	t.Run("midpoint pressure rejectProb is 0.45", func(t *testing.T) {
		// pressure=0.75: frac=(0.75-0.5)/0.5=0.5, rejectProb = 0.5*(1-0.1) = 0.45
		s := newShedder(0.75, func() float64 { return 0.5 })
		tk, ok := s.Acquire(context.Background())
		assert.True(t, ok) // 0.5 >= 0.45 is true -> admit
		tk.Release()

		s = newShedder(0.75, func() float64 { return 0.4 })
		_, ok = s.Acquire(context.Background())
		assert.False(t, ok) // 0.4 >= 0.45 is false -> shed
	})
}
